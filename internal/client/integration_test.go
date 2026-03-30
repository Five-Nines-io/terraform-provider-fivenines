package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
)

// Integration test against the real FiveNines API.
// Run with: go test ./internal/client/ -run TestIntegration -v
var (
	testAPIKey  = os.Getenv("FIVENINES_API_KEY")
	testBaseURL = envOr("FIVENINES_BASE_URL", "https://fivenines.io")
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func skipIfNoAPIKey(t *testing.T) {
	t.Helper()
	if testAPIKey == "" {
		t.Skip("FIVENINES_API_KEY not set — skipping integration test")
	}
}

func TestIntegration_StatusPage_FullLifecycle(t *testing.T) {
	skipIfNoAPIKey(t)
	c := NewClient(testBaseURL, testAPIKey)
	ctx := context.Background()

	// ── Step 1: Create a status page ──────────────────────────────
	t.Log("=== Step 1: Create status page ===")
	isPublic := false
	showUptime := true
	input := CreateStatusPageInput{
		Name:   "TF Integration Test Page",
		Public: &isPublic,
		Uptime: &showUptime,
	}
	page, err := c.CreateStatusPage(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	t.Logf("Created page: id=%d, name=%q, slug=%q, theme=%q", page.ID, page.Name, page.Slug, page.ThemeVariant)
	pageID := page.ID

	// Cleanup: always delete the page at the end
	defer func() {
		t.Log("=== Cleanup: Delete status page ===")
		if err := c.DeleteStatusPage(ctx, pageID); err != nil {
			t.Logf("WARNING: cleanup delete failed: %v", err)
		} else {
			t.Log("Deleted successfully")
		}
	}()

	// ── Step 2: Read it back ──────────────────────────────────────
	t.Log("=== Step 2: Read back ===")
	readPage, etag, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	t.Logf("Read page: name=%q, theme=%q, etag=%q, items=%d", readPage.Name, readPage.ThemeVariant, etag, len(readPage.Items))

	if readPage.Name != "TF Integration Test Page" {
		t.Errorf("name mismatch: expected 'TF Integration Test Page', got %q", readPage.Name)
	}

	// ── Step 3: Update scalar field (theme_variant) ───────────────
	t.Log("=== Step 3: Update theme to 'dark' ===")
	newTheme := "dark"
	newName := "TF Integration Test Page"
	updateInput := UpdateStatusPageInput{
		Name:         &newName,
		ThemeVariant: &newTheme,
	}

	updatedPage, err := c.UpdateStatusPage(ctx, pageID, etag, updateInput)
	if err != nil {
		t.Fatalf("Update (theme) failed: %v", err)
	}
	t.Logf("Updated page: theme=%q, updated_at=%q", updatedPage.ThemeVariant, updatedPage.UpdatedAt)

	if updatedPage.ThemeVariant != "dark" {
		t.Errorf("theme not updated: expected 'dark', got %q", updatedPage.ThemeVariant)
	}

	// ── Step 4: Verify update persisted via fresh read ────────────
	t.Log("=== Step 4: Read after theme update ===")
	readPage2, etag2, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Read after update failed: %v", err)
	}
	t.Logf("Read page: theme=%q, etag=%q", readPage2.ThemeVariant, etag2)

	if readPage2.ThemeVariant != "dark" {
		t.Errorf("theme change didn't persist: expected 'dark', got %q", readPage2.ThemeVariant)
	}

	// ── Step 5: Update with all fields (simulate terraform sending everything) ──
	t.Log("=== Step 5: Update all fields (terraform-style) ===")
	desc := "Integration test description"
	pub := true
	upt := true
	cde := false
	cfe := false
	ihe := true
	theme2 := "light"
	fullUpdate := UpdateStatusPageInput{
		Name:                    &newName,
		Description:             &desc,
		Public:                  &pub,
		Uptime:                  &upt,
		CustomDomainEnabled:     &cde,
		CustomFooterEnabled:     &cfe,
		IncidentsHistoryEnabled: &ihe,
		ThemeVariant:            &theme2,
	}
	updatedPage2, err := c.UpdateStatusPage(ctx, pageID, etag2, fullUpdate)
	if err != nil {
		t.Fatalf("Update (full) failed: %v", err)
	}
	t.Logf("Updated: theme=%q, desc=%q, public=%v, incidents_history=%v",
		updatedPage2.ThemeVariant, updatedPage2.Description,
		updatedPage2.Public, updatedPage2.IncidentsHistoryEnabled)

	if updatedPage2.ThemeVariant != "light" {
		t.Errorf("theme not 'light': got %q", updatedPage2.ThemeVariant)
	}
	if updatedPage2.Description != "Integration test description" {
		t.Errorf("description not updated: got %q", updatedPage2.Description)
	}
	if !updatedPage2.IncidentsHistoryEnabled {
		t.Error("incidents_history_enabled should be true")
	}

	// ── Step 6: Second update (change theme again — the customer's exact scenario) ──
	t.Log("=== Step 6: Second update (change theme back to 'system') ===")
	_, etag3, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Read before second update failed: %v", err)
	}

	theme3 := "system"
	secondUpdate := UpdateStatusPageInput{
		Name:                    &newName,
		Description:             &desc,
		Public:                  &pub,
		Uptime:                  &upt,
		CustomDomainEnabled:     &cde,
		CustomFooterEnabled:     &cfe,
		IncidentsHistoryEnabled: &ihe,
		ThemeVariant:            &theme3,
	}
	updatedPage3, err := c.UpdateStatusPage(ctx, pageID, etag3, secondUpdate)
	if err != nil {
		t.Fatalf("Second update failed: %v", err)
	}
	t.Logf("Second update: theme=%q", updatedPage3.ThemeVariant)

	if updatedPage3.ThemeVariant != "system" {
		t.Errorf("theme not 'system' after second update: got %q", updatedPage3.ThemeVariant)
	}

	// ── Step 7: Verify second update persisted ────────────────────
	t.Log("=== Step 7: Final read ===")
	finalPage, _, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Final read failed: %v", err)
	}
	t.Logf("Final state: theme=%q, desc=%q, public=%v, incidents_history=%v",
		finalPage.ThemeVariant, finalPage.Description,
		finalPage.Public, finalPage.IncidentsHistoryEnabled)

	if finalPage.ThemeVariant != "system" {
		t.Errorf("theme didn't persist: expected 'system', got %q", finalPage.ThemeVariant)
	}

	// ── Bonus: dump raw JSON from API to inspect shape ────────────
	t.Log("=== Bonus: Raw JSON dump ===")
	rawJSON, _ := json.MarshalIndent(finalPage, "", "  ")
	t.Logf("Final page JSON:\n%s", string(rawJSON))
}

func TestIntegration_StatusPage_UpdateWithItems(t *testing.T) {
	skipIfNoAPIKey(t)
	c := NewClient(testBaseURL, testAPIKey)
	ctx := context.Background()

	// First, find an existing uptime monitor to reference
	t.Log("=== Finding existing monitors ===")
	monitors, err := c.ListUptimeMonitors(ctx)
	if err != nil {
		t.Fatalf("ListUptimeMonitors failed: %v", err)
	}
	if len(monitors) == 0 {
		t.Skip("No uptime monitors found — cannot test items. Create at least one monitor first.")
	}
	t.Logf("Found %d monitors. Using: id=%q name=%q", len(monitors), monitors[0].ID, monitors[0].Name)

	// Create a status page
	t.Log("=== Create status page ===")
	isPublic := false
	showUptime := true
	page, err := c.CreateStatusPage(ctx, CreateStatusPageInput{
		Name:   "TF Items Test Page",
		Public: &isPublic,
		Uptime: &showUptime,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	pageID := page.ID
	t.Logf("Created page id=%d, items=%d", pageID, len(page.Items))

	defer func() {
		c.DeleteStatusPage(ctx, pageID)
	}()

	// Update: add a monitor as an item
	t.Log("=== Update: add monitor item ===")
	_, etag, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	name := "TF Items Test Page"
	theme := "system"
	updatedPage, err := c.UpdateStatusPage(ctx, pageID, etag, UpdateStatusPageInput{
		Name:         &name,
		ThemeVariant: &theme,
		Items: []StatusPageItem{
			{ItemType: "UptimeMonitor", ItemID: monitors[0].ID, Position: 0},
		},
	})
	if err != nil {
		t.Fatalf("Update (add items) failed: %v", err)
	}
	t.Logf("After adding item: items=%d", len(updatedPage.Items))
	if len(updatedPage.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(updatedPage.Items))
	}

	// Read back to confirm items persisted
	t.Log("=== Read after adding items ===")
	readPage, etag2, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Read after items update failed: %v", err)
	}
	t.Logf("Read: items=%d", len(readPage.Items))
	if len(readPage.Items) != 1 {
		t.Errorf("items didn't persist: expected 1, got %d", len(readPage.Items))
	}
	if len(readPage.Items) > 0 {
		t.Logf("Item: type=%q id=%q pos=%d", readPage.Items[0].ItemType, readPage.Items[0].ItemID, readPage.Items[0].Position)
	}

	// Update: change theme WITHOUT touching items (terraform sends no items key)
	t.Log("=== Update: change theme only (no items in payload) ===")
	darkTheme := "dark"
	updatedPage2, err := c.UpdateStatusPage(ctx, pageID, etag2, UpdateStatusPageInput{
		Name:         &name,
		ThemeVariant: &darkTheme,
		// Items intentionally omitted — simulates terraform when only theme changed
	})
	if err != nil {
		t.Fatalf("Update (theme only) failed: %v", err)
	}
	t.Logf("After theme-only update: theme=%q, items=%d", updatedPage2.ThemeVariant, len(updatedPage2.Items))

	if updatedPage2.ThemeVariant != "dark" {
		t.Errorf("theme not updated: got %q", updatedPage2.ThemeVariant)
	}
	if len(updatedPage2.Items) != 1 {
		t.Errorf("items lost after theme-only update! expected 1, got %d", len(updatedPage2.Items))
	}

	// Update: add a second monitor if available
	if len(monitors) >= 2 {
		t.Log("=== Update: add second monitor ===")
		_, etag3, _ := c.GetStatusPage(ctx, pageID)
		updatedPage3, err := c.UpdateStatusPage(ctx, pageID, etag3, UpdateStatusPageInput{
			Name:         &name,
			ThemeVariant: &darkTheme,
			Items: []StatusPageItem{
				{ItemType: "UptimeMonitor", ItemID: monitors[0].ID, Position: 0},
				{ItemType: "UptimeMonitor", ItemID: monitors[1].ID, Position: 1},
			},
		})
		if err != nil {
			t.Fatalf("Update (2 items) failed: %v", err)
		}
		t.Logf("After adding 2nd item: items=%d", len(updatedPage3.Items))
		if len(updatedPage3.Items) != 2 {
			t.Errorf("expected 2 items, got %d", len(updatedPage3.Items))
		}
		for i, item := range updatedPage3.Items {
			t.Logf("  Item[%d]: type=%q id=%q pos=%d", i, item.ItemType, item.ItemID, item.Position)
		}
	}

	// Final dump
	rawJSON, _ := json.MarshalIndent(updatedPage2, "", "  ")
	t.Logf("Final page JSON:\n%s", string(rawJSON))
}

func TestIntegration_StatusPage_CreateWithItems(t *testing.T) {
	skipIfNoAPIKey(t)
	c := NewClient(testBaseURL, testAPIKey)
	ctx := context.Background()

	// Find monitors
	monitors, err := c.ListUptimeMonitors(ctx)
	if err != nil {
		t.Fatalf("ListUptimeMonitors failed: %v", err)
	}
	if len(monitors) == 0 {
		t.Skip("No monitors available")
	}

	// Create WITH items from the start
	t.Log("=== Create with items ===")
	isPublic := false
	showUptime := true
	page, err := c.CreateStatusPage(ctx, CreateStatusPageInput{
		Name:   "TF Create-With-Items Test",
		Public: &isPublic,
		Uptime: &showUptime,
		Items: []StatusPageItem{
			{ItemType: "UptimeMonitor", ItemID: monitors[0].ID, Position: 0},
		},
	})
	if err != nil {
		t.Fatalf("Create with items failed: %v", err)
	}
	pageID := page.ID
	t.Logf("Created page id=%d, items=%d", pageID, len(page.Items))

	defer func() {
		c.DeleteStatusPage(ctx, pageID)
	}()

	if len(page.Items) != 1 {
		t.Errorf("expected 1 item after create, got %d", len(page.Items))
	}

	// Now update theme only (THE CUSTOMER'S EXACT BUG: "changing the theme")
	t.Log("=== Update theme on page created with items ===")
	_, etag, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Simulate terraform: sends all fields from plan, including items
	name := "TF Create-With-Items Test"
	desc := ""
	pub := false
	upt := true
	cde := false
	cfe := false
	ihe := false
	theme := "dark"
	updatedPage, err := c.UpdateStatusPage(ctx, pageID, etag, UpdateStatusPageInput{
		Name:                    &name,
		Description:             &desc,
		Public:                  &pub,
		Uptime:                  &upt,
		CustomDomainEnabled:     &cde,
		CustomFooterEnabled:     &cfe,
		IncidentsHistoryEnabled: &ihe,
		ThemeVariant:            &theme,
		Items: []StatusPageItem{
			{ItemType: "UptimeMonitor", ItemID: monitors[0].ID, Position: 0},
		},
	})
	if err != nil {
		t.Fatalf("Update (theme + items) failed: %v", err)
	}
	t.Logf("After update: theme=%q, items=%d", updatedPage.ThemeVariant, len(updatedPage.Items))

	if updatedPage.ThemeVariant != "dark" {
		t.Errorf("theme not 'dark': got %q", updatedPage.ThemeVariant)
	}
	if len(updatedPage.Items) != 1 {
		t.Errorf("items lost! expected 1, got %d", len(updatedPage.Items))
	}

	// Verify with read
	t.Log("=== Final read ===")
	finalPage, _, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Final read failed: %v", err)
	}
	t.Logf("Final: theme=%q, items=%d, desc=%q", finalPage.ThemeVariant, len(finalPage.Items), finalPage.Description)
	rawJSON, _ := json.MarshalIndent(finalPage, "", "  ")
	t.Logf("JSON:\n%s", string(rawJSON))

	if finalPage.ThemeVariant != "dark" {
		t.Errorf("theme didn't persist after final read")
	}
	if len(finalPage.Items) != 1 {
		t.Errorf("items didn't persist after final read: got %d", len(finalPage.Items))
	}
}

// Test updating an EXISTING page that's been sitting in the DB.
// This reproduces the customer's exact scenario: page created earlier, now trying to update.
func TestIntegration_StatusPage_UpdateExistingPage(t *testing.T) {
	skipIfNoAPIKey(t)
	c := NewClient(testBaseURL, testAPIKey)
	ctx := context.Background()

	// List existing pages to find one with items
	pages, err := c.ListStatusPages(ctx)
	if err != nil {
		t.Fatalf("ListStatusPages failed: %v", err)
	}

	var targetPage *StatusPage
	for i := range pages {
		if len(pages[i].Items) > 0 {
			targetPage = &pages[i]
			break
		}
	}
	if targetPage == nil {
		t.Skip("No existing page with items found")
	}

	t.Logf("Target page: id=%d name=%q theme=%q items=%d", targetPage.ID, targetPage.Name, targetPage.ThemeVariant, len(targetPage.Items))

	// Step 1: GET to get ETag (exactly what the terraform Update() does)
	t.Log("=== Step 1: GET for ETag ===")
	readPage, etag, err := c.GetStatusPage(ctx, targetPage.ID)
	if err != nil {
		t.Fatalf("GetStatusPage failed: %v", err)
	}
	t.Logf("ETag from GET: %q", etag)
	t.Logf("updated_at: %q", readPage.UpdatedAt)

	// Step 2: PATCH with same values (no actual change, just testing ETag mechanism)
	t.Log("=== Step 2: PATCH (theme change only, keep items) ===")
	newTheme := "dark"
	if readPage.ThemeVariant == "dark" {
		newTheme = "light"
	}
	name := readPage.Name
	updateInput := UpdateStatusPageInput{
		Name:         &name,
		ThemeVariant: &newTheme,
		// Send items back exactly as they are (like terraform does)
		Items: readPage.Items,
	}
	reqJSON, _ := json.MarshalIndent(map[string]interface{}{"status_page": updateInput}, "", "  ")
	t.Logf("PATCH body:\n%s", string(reqJSON))

	updatedPage, err := c.UpdateStatusPage(ctx, targetPage.ID, etag, updateInput)
	if err != nil {
		t.Fatalf("UpdateStatusPage FAILED: %v", err)
	}
	t.Logf("Update succeeded: theme=%q", updatedPage.ThemeVariant)

	// Step 3: Revert the theme back
	t.Log("=== Step 3: Revert theme ===")
	_, etag2, _ := c.GetStatusPage(ctx, targetPage.ID)
	revertTheme := readPage.ThemeVariant
	revertInput := UpdateStatusPageInput{
		Name:         &name,
		ThemeVariant: &revertTheme,
		Items:        readPage.Items,
	}
	_, err = c.UpdateStatusPage(ctx, targetPage.ID, etag2, revertInput)
	if err != nil {
		t.Logf("WARNING: revert failed: %v", err)
	} else {
		t.Log("Reverted successfully")
	}
}

// Test to check what ListUptimeMonitors returns (for debugging item IDs)
func TestIntegration_ListMonitors(t *testing.T) {
	skipIfNoAPIKey(t)
	c := NewClient(testBaseURL, testAPIKey)
	ctx := context.Background()

	monitors, err := c.ListUptimeMonitors(ctx)
	if err != nil {
		t.Fatalf("ListUptimeMonitors failed: %v", err)
	}
	t.Logf("Found %d monitors:", len(monitors))
	for i, m := range monitors {
		t.Logf("  [%d] id=%q name=%q url=%q", i, m.ID, m.Name, m.URL)
		if i >= 4 {
			t.Logf("  ... and %d more", len(monitors)-5)
			break
		}
	}

	// Also check instances (Hosts)
	instances, err := c.ListInstances(ctx)
	if err != nil {
		t.Logf("ListInstances failed: %v", err)
	} else {
		t.Logf("Found %d instances:", len(instances))
		for i, inst := range instances {
			t.Logf("  [%d] id=%q name=%q", i, inst.ID, inst.DisplayName)
			if i >= 4 {
				break
			}
		}
	}

	// List existing status pages
	pages, err := c.ListStatusPages(ctx)
	if err != nil {
		t.Logf("ListStatusPages failed: %v", err)
	} else {
		t.Logf("Found %d status pages:", len(pages))
		for i, p := range pages {
			itemsJSON, _ := json.Marshal(p.Items)
			t.Logf("  [%d] id=%d name=%q theme=%q items=%s", i, p.ID, p.Name, p.ThemeVariant, string(itemsJSON))
		}
	}
}

// Check what the raw API actually returns for an update
func TestIntegration_StatusPage_RawUpdateResponse(t *testing.T) {
	skipIfNoAPIKey(t)
	c := NewClient(testBaseURL, testAPIKey)
	ctx := context.Background()

	// Create
	isPublic := false
	page, err := c.CreateStatusPage(ctx, CreateStatusPageInput{
		Name:   "TF Raw Response Test",
		Public: &isPublic,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer c.DeleteStatusPage(ctx, page.ID)

	// Do a raw PATCH and inspect the response body
	_, etag, _ := c.GetStatusPage(ctx, page.ID)

	theme := "dark"
	name := "TF Raw Response Test"
	updateBody := map[string]interface{}{
		"status_page": UpdateStatusPageInput{
			Name:         &name,
			ThemeVariant: &theme,
		},
	}
	bodyJSON, _ := json.Marshal(updateBody)
	t.Logf("Request body: %s", string(bodyJSON))

	req, _ := http.NewRequestWithContext(ctx, "PATCH",
		fmt.Sprintf("%s/api/v1/status_pages/%d", testBaseURL, page.ID), nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	req.Header.Set("Content-Type", "application/json")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	req.Body = io.NopCloser(bytes.NewReader(bodyJSON))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		t.Fatalf("Raw PATCH failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("Response status: %d", resp.StatusCode)
	t.Logf("Response ETag: %q", resp.Header.Get("ETag"))
	t.Logf("Response body: %s", string(respBody))

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// Simulate exactly what Terraform sends during a "change theme" update
// after the page was created with items. This is the customer's exact scenario.
func TestIntegration_StatusPage_TerraformUpdateSimulation(t *testing.T) {
	skipIfNoAPIKey(t)
	c := NewClient(testBaseURL, testAPIKey)
	ctx := context.Background()

	// Find a monitor
	monitors, err := c.ListUptimeMonitors(ctx)
	if err != nil || len(monitors) == 0 {
		t.Skip("No monitors available")
	}

	// Step 1: Create with items (simulating terraform create)
	t.Log("=== Step 1: Terraform Create ===")
	isPublic := false
	showUptime := true
	incHist := false
	cde := false
	cfe := false
	createInput := CreateStatusPageInput{
		Name:                    "TF Simulation Test",
		Description:             "", // user didn't specify, empty string from Optional+Computed
		Public:                  &isPublic,
		Uptime:                  &showUptime,
		CustomDomainEnabled:     &cde,
		CustomFooterEnabled:     &cfe,
		IncidentsHistoryEnabled: &incHist,
		ThemeVariant:            "system",
		Items: []StatusPageItem{
			{ItemType: "UptimeMonitor", ItemID: monitors[0].ID, Position: 0},
		},
	}
	createJSON, _ := json.MarshalIndent(map[string]interface{}{"status_page": createInput}, "", "  ")
	t.Logf("Create request body:\n%s", string(createJSON))

	page, err := c.CreateStatusPage(ctx, createInput)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	pageID := page.ID
	defer c.DeleteStatusPage(ctx, pageID)
	t.Logf("Created: id=%d, items=%d, theme=%q", pageID, len(page.Items), page.ThemeVariant)

	// Step 2: Terraform Read (refresh)
	t.Log("=== Step 2: Terraform Read (refresh) ===")
	readPage, _, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	readJSON, _ := json.MarshalIndent(readPage, "", "  ")
	t.Logf("State after Read:\n%s", string(readJSON))

	// Step 3: Simulate terraform update (user changes theme_variant to "dark")
	// Terraform sends ALL plan values, not just the changed field.
	// The plan has: all defaults + user's values + state values for Optional+Computed
	t.Log("=== Step 3: Terraform Update (change theme to dark) ===")
	_, etag, _ := c.GetStatusPage(ctx, pageID)

	// This is what the Terraform resource's Update() would build:
	name := readPage.Name        // from plan (user's config, unchanged)
	desc := readPage.Description // from plan (state value, user didn't specify)
	pub := readPage.Public       // from plan (default false)
	upt := readPage.Uptime       // from plan (default true)
	cdEnabled := readPage.CustomDomainEnabled   // from plan (default false)
	cfEnabled := readPage.CustomFooterEnabled   // from plan (default false)
	ihEnabled := readPage.IncidentsHistoryEnabled // from plan (default false)
	theme := "dark" // THE CHANGE the user made

	updateInput := UpdateStatusPageInput{
		Name:                    &name,
		Description:             &desc,
		Public:                  &pub,
		Uptime:                  &upt,
		CustomDomainEnabled:     &cdEnabled,
		CustomFooterEnabled:     &cfEnabled,
		IncidentsHistoryEnabled: &ihEnabled,
		ThemeVariant:            &theme,
		// Items from plan — user's config still has the same items
		Items: []StatusPageItem{
			{ItemType: "UptimeMonitor", ItemID: monitors[0].ID, Position: 0},
		},
	}
	updateJSON, _ := json.MarshalIndent(map[string]interface{}{"status_page": updateInput}, "", "  ")
	t.Logf("Update request body:\n%s", string(updateJSON))

	updatedPage, err := c.UpdateStatusPage(ctx, pageID, etag, updateInput)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	updateRespJSON, _ := json.MarshalIndent(updatedPage, "", "  ")
	t.Logf("Update response:\n%s", string(updateRespJSON))

	// Verify
	if updatedPage.ThemeVariant != "dark" {
		t.Errorf("THEME NOT UPDATED: expected 'dark', got %q", updatedPage.ThemeVariant)
	}
	if len(updatedPage.Items) != 1 {
		t.Errorf("ITEMS LOST: expected 1, got %d", len(updatedPage.Items))
	}

	// Step 4: Simulate terraform read after update
	t.Log("=== Step 4: Terraform Read after update ===")
	finalPage, _, err := c.GetStatusPage(ctx, pageID)
	if err != nil {
		t.Fatalf("Final read failed: %v", err)
	}
	finalJSON, _ := json.MarshalIndent(finalPage, "", "  ")
	t.Logf("Final state:\n%s", string(finalJSON))

	// Check for perpetual diff: does the state match what terraform would expect?
	if finalPage.ThemeVariant != "dark" {
		t.Errorf("PERPETUAL DIFF: theme reverted to %q", finalPage.ThemeVariant)
	}
	if finalPage.Description != desc {
		t.Errorf("PERPETUAL DIFF: description changed from %q to %q", desc, finalPage.Description)
	}
	if len(finalPage.Items) != 1 {
		t.Errorf("PERPETUAL DIFF: items count changed from 1 to %d", len(finalPage.Items))
	}
}
