package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// --- Status Page Maintenance Windows ---

func maintenanceWindowFixture() map[string]interface{} {
	return map[string]interface{}{
		"id":             77,
		"status_page_id": 12,
		"title":          "Database upgrade",
		"body":           "Read-only mode for two hours.",
		"starts_at":      "2026-09-02T00:00:00+02:00",
		"ends_at":        "2026-09-02T02:00:00+02:00",
		"time_zone":      "Europe/Paris",
		"status":         "scheduled",
		"state":          "scheduled",
		"affected_items": []interface{}{
			map[string]interface{}{"item_type": "Host", "item_id": "host-uuid"},
		},
		"created_at": "2026-09-01T00:00:00Z",
		"updated_at": "2026-09-01T00:00:00Z",
	}
}

func TestClient_GetStatusPageMaintenanceWindow(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/status_pages/12/maintenance_windows/77" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("ETag", `"mw-etag-gzip"`)
		json.NewEncoder(w).Encode(map[string]interface{}{"maintenance_window": maintenanceWindowFixture()})
	})

	window, etag, err := c.GetStatusPageMaintenanceWindow(context.Background(), 12, 77)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"mw-etag"` {
		t.Errorf("expected sanitized etag %q, got %q", `"mw-etag"`, etag)
	}
	if window.ID != 77 || window.StatusPageID != 12 {
		t.Errorf("expected id 77 on page 12, got %d on %d", window.ID, window.StatusPageID)
	}
	if window.Body == nil || *window.Body != "Read-only mode for two hours." {
		t.Errorf("unexpected body: %v", window.Body)
	}
	if window.TimeZone != "Europe/Paris" {
		t.Errorf("expected time_zone Europe/Paris, got %s", window.TimeZone)
	}
	if len(window.AffectedItems) != 1 || window.AffectedItems[0].ItemID != "host-uuid" {
		t.Errorf("unexpected affected_items: %#v", window.AffectedItems)
	}
}

func TestClient_GetStatusPageMaintenanceWindow_NullBody(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fixture := maintenanceWindowFixture()
		fixture["body"] = nil
		json.NewEncoder(w).Encode(map[string]interface{}{"maintenance_window": fixture})
	})

	window, _, err := c.GetStatusPageMaintenanceWindow(context.Background(), 12, 77)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if window.Body != nil {
		t.Errorf("expected nil body, got %q", *window.Body)
	}
}

func TestClient_CreateStatusPageMaintenanceWindow(t *testing.T) {
	var gotBody map[string]interface{}
	var gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"maintenance_window": maintenanceWindowFixture()})
	})

	body := "Read-only mode for two hours."
	items := []MaintenanceWindowAffectedItem{{ItemType: "Host", ItemID: "host-uuid"}}
	window, err := c.CreateStatusPageMaintenanceWindow(context.Background(), 12, CreateStatusPageMaintenanceWindowInput{
		Title:         "Database upgrade",
		Body:          &body,
		StartsAt:      "2026-09-01T22:00:00Z",
		EndsAt:        "2026-09-02T00:00:00Z",
		AffectedItems: &items,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/status_pages/12/maintenance_windows" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	sent, ok := gotBody["maintenance_window"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected maintenance_window envelope, got %#v", gotBody)
	}
	if sent["title"] != "Database upgrade" {
		t.Errorf("unexpected title: %v", sent["title"])
	}
	if sent["starts_at"] != "2026-09-01T22:00:00Z" {
		t.Errorf("unexpected starts_at: %v", sent["starts_at"])
	}
	if got := sent["affected_items"].([]interface{}); len(got) != 1 {
		t.Errorf("expected 1 affected item, got %d", len(got))
	}
	if window.ID != 77 {
		t.Errorf("expected id 77, got %d", window.ID)
	}
}

func TestClient_CreateStatusPageMaintenanceWindow_OmitsUnsetOptionals(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"maintenance_window": maintenanceWindowFixture()})
	})

	_, err := c.CreateStatusPageMaintenanceWindow(context.Background(), 12, CreateStatusPageMaintenanceWindowInput{
		Title:    "Database upgrade",
		StartsAt: "2026-09-01T22:00:00Z",
		EndsAt:   "2026-09-02T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sent := gotBody["maintenance_window"].(map[string]interface{})
	if _, present := sent["body"]; present {
		t.Error("expected body to be omitted when unset")
	}
	if _, present := sent["affected_items"]; present {
		t.Error("expected affected_items to be omitted when unset")
	}
}

func TestClient_UpdateStatusPageMaintenanceWindow(t *testing.T) {
	var gotIfMatch, gotPath, gotMethod string
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotIfMatch = r.Header.Get("If-Match")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{"maintenance_window": maintenanceWindowFixture()})
	})

	title := "Database upgrade"
	items := []MaintenanceWindowAffectedItem{}
	_, err := c.UpdateStatusPageMaintenanceWindow(context.Background(), 12, 77, `"mw-etag"`, UpdateStatusPageMaintenanceWindowInput{
		Title:         &title,
		AffectedItems: &items,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "PATCH" || gotPath != "/api/v1/status_pages/12/maintenance_windows/77" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
	if gotIfMatch != `"mw-etag"` {
		t.Errorf("expected If-Match %q, got %q", `"mw-etag"`, gotIfMatch)
	}
	sent := gotBody["maintenance_window"].(map[string]interface{})
	// A nil Body pointer must serialize as an explicit null so the API clears it.
	raw, present := sent["body"]
	if !present || raw != nil {
		t.Errorf("expected body to be sent as null, got %v (present=%v)", raw, present)
	}
	// An empty (non-nil) slice must serialize as [] so the API clears the list.
	if got, ok := sent["affected_items"].([]interface{}); !ok || len(got) != 0 {
		t.Errorf("expected affected_items [], got %#v", sent["affected_items"])
	}
	// Unset scalars stay out of the payload so the partial PATCH leaves them alone.
	if _, present := sent["starts_at"]; present {
		t.Error("expected starts_at to be omitted when unset")
	}
}

func TestClient_UpdateStatusPageMaintenanceWindow_PreconditionFailed(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "stale"})
	})

	_, err := c.UpdateStatusPageMaintenanceWindow(context.Background(), 12, 77, `"old"`, UpdateStatusPageMaintenanceWindowInput{})
	if !IsPreconditionFailed(err) {
		t.Errorf("expected precondition failed error, got %v", err)
	}
}

func TestClient_DeleteStatusPageMaintenanceWindow(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteStatusPageMaintenanceWindow(context.Background(), 12, 77); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "DELETE" || gotPath != "/api/v1/status_pages/12/maintenance_windows/77" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
}

func TestClient_CancelStatusPageMaintenanceWindow(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	if err := c.CancelStatusPageMaintenanceWindow(context.Background(), 12, 77); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "PATCH" || gotPath != "/api/v1/status_pages/12/maintenance_windows/77/cancel" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
}

func TestClient_CancelStatusPageMaintenanceWindow_NotFound(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})

	err := c.CancelStatusPageMaintenanceWindow(context.Background(), 12, 77)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != 404 {
		t.Errorf("expected 404 APIError, got %v", err)
	}
}
