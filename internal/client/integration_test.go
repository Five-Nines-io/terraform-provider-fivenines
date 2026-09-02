package client

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// Integration test against the real FiveNines API.
// Run with: TF_ACC=1 FIVENINES_API_KEY=... go test ./internal/client/ -run TestIntegration -v
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

func derefOr[T any](v *T, fallback T) T {
	if v == nil {
		return fallback
	}
	return *v
}

func skipIfNoAPIKey(t *testing.T) {
	t.Helper()
	// TF_ACC as well as the key: these tests mutate a real organisation, and
	// `make test` must never do that just because a key happens to be exported.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set — skipping integration test against a live API")
	}
	if testAPIKey == "" {
		t.Skip("FIVENINES_API_KEY not set — skipping integration test")
	}
}

// Reproduces the customer bug: updating an existing page with items fails
// with 412 because Nginx appends "-gzip" to the ETag header.
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

	t.Logf("Target page: id=%d name=%q theme=%q items=%d", targetPage.ID, targetPage.Name, derefOr(targetPage.ThemeVariant, ""), len(targetPage.Items))

	// Step 1: GET to get ETag (exactly what the terraform Update() does)
	t.Log("=== Step 1: GET for ETag ===")
	readPage, etag, err := c.GetStatusPage(ctx, targetPage.ID)
	if err != nil {
		t.Fatalf("GetStatusPage failed: %v", err)
	}
	t.Logf("ETag from GET: %q", etag)
	t.Logf("updated_at: %q", readPage.UpdatedAt)

	// Items is a pointer now, so sending an empty slice CLEARS the page. Guard
	// the exact value the PATCH below sends, not a separate read of it.
	if len(readPage.Items) == 0 {
		t.Skip("Re-read came back with no items — refusing to PATCH an empty items list")
	}

	// Step 2: PATCH with a theme change, keeping items
	t.Log("=== Step 2: PATCH (theme change, keep items) ===")
	newTheme := "dark"
	if derefOr(readPage.ThemeVariant, "") == "dark" {
		newTheme = "light"
	}
	name := readPage.Name
	items := itemInputs(readPage.Items)
	updateInput := UpdateStatusPageInput{
		Name:         &name,
		ThemeVariant: &newTheme,
		Items:        &items,
	}
	reqJSON, _ := json.MarshalIndent(map[string]interface{}{"status_page": updateInput}, "", "  ")
	t.Logf("PATCH body:\n%s", string(reqJSON))

	updatedPage, err := c.UpdateStatusPage(ctx, targetPage.ID, etag, updateInput)
	if err != nil {
		t.Fatalf("UpdateStatusPage FAILED: %v", err)
	}
	t.Logf("Update succeeded: theme=%q", derefOr(updatedPage.ThemeVariant, ""))

	// Step 3: Revert the theme back
	t.Log("=== Step 3: Revert theme ===")
	_, etag2, _ := c.GetStatusPage(ctx, targetPage.ID)
	revertTheme := derefOr(readPage.ThemeVariant, "")
	revertInput := UpdateStatusPageInput{
		Name:         &name,
		ThemeVariant: &revertTheme,
		Items:        &items,
	}
	_, err = c.UpdateStatusPage(ctx, targetPage.ID, etag2, revertInput)
	if err != nil {
		t.Logf("WARNING: revert failed: %v", err)
	} else {
		t.Log("Reverted successfully")
	}
}

// itemInputs echoes a page's items back as update inputs. The per-item label,
// description and section are left out so the server keeps whatever it has.
func itemInputs(items []StatusPageItem) []StatusPageItemInput {
	result := make([]StatusPageItemInput, len(items))
	for i, item := range items {
		result[i] = StatusPageItemInput{ItemType: item.ItemType, ItemID: item.ItemID}
	}
	return result
}
