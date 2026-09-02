package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
)

// --- Host Groups ---

func TestClient_CreateHostGroup(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/host_groups" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_group": map[string]interface{}{
				"id": 7, "name": "Production", "position": 2,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	position := int64(2)
	group, err := c.CreateHostGroup(context.Background(), CreateHostGroupInput{
		Name: "Production", Position: &position,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if group.ID != 7 {
		t.Errorf("expected ID 7, got %d", group.ID)
	}
	if group.Position != 2 {
		t.Errorf("expected position 2, got %d", group.Position)
	}

	payload, ok := gotBody["host_group"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected host_group envelope, got %v", gotBody)
	}
	if payload["name"] != "Production" {
		t.Errorf("expected name Production, got %v", payload["name"])
	}
	if payload["position"] != float64(2) {
		t.Errorf("expected position 2, got %v", payload["position"])
	}
}

func TestClient_CreateHostGroup_OmitsNilPosition(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_group": map[string]interface{}{
				"id": 1, "name": "Staging", "position": 1,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	if _, err := c.CreateHostGroup(context.Background(), CreateHostGroupInput{Name: "Staging"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := gotBody["host_group"].(map[string]interface{})
	if _, present := payload["position"]; present {
		t.Errorf("expected position to be omitted, got %v", payload["position"])
	}
}

func TestClient_GetHostGroup(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/host_groups/7" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("ETag", `"hg-etag-gzip"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_group": map[string]interface{}{
				"id": 7, "name": "Production", "position": 3,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	group, etag, err := c.GetHostGroup(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"hg-etag"` {
		t.Errorf("expected sanitized etag %q, got %q", `"hg-etag"`, etag)
	}
	if group.Name != "Production" {
		t.Errorf("expected name Production, got %s", group.Name)
	}
	if group.Position != 3 {
		t.Errorf("expected position 3, got %d", group.Position)
	}
}

func TestClient_GetHostGroup_404(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	})

	_, _, err := c.GetHostGroup(context.Background(), 7)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("expected 404, got %d", apiErr.StatusCode)
	}
}

func TestClient_UpdateHostGroup_ETag(t *testing.T) {
	var gotIfMatch string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/host_groups/7" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotIfMatch = r.Header.Get("If-Match")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_group": map[string]interface{}{
				"id": 7, "name": "Prod EU", "position": 1,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-03T00:00:00Z",
			},
		})
	})

	name := "Prod EU"
	group, err := c.UpdateHostGroup(context.Background(), 7, `"hg-etag"`, UpdateHostGroupInput{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIfMatch != `"hg-etag"` {
		t.Errorf("expected If-Match %q, got %q", `"hg-etag"`, gotIfMatch)
	}
	if group.Name != "Prod EU" {
		t.Errorf("expected name Prod EU, got %s", group.Name)
	}
}

func TestClient_UpdateHostGroup_412(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Precondition Failed"})
	})

	name := "Prod EU"
	_, err := c.UpdateHostGroup(context.Background(), 7, `"stale"`, UpdateHostGroupInput{Name: &name})
	if !IsPreconditionFailed(err) {
		t.Errorf("expected precondition failed, got %v", err)
	}
}

func TestClient_CreateHostGroup_422DuplicateName(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []string{"Name has already been taken"},
		})
	})

	_, err := c.CreateHostGroup(context.Background(), CreateHostGroupInput{Name: "Production"})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 422 {
		t.Errorf("expected 422, got %d", apiErr.StatusCode)
	}
	if len(apiErr.Errors) != 1 || apiErr.Errors[0] != "Name has already been taken" {
		t.Errorf("expected duplicate name error, got %v", apiErr.Errors)
	}
}

func TestClient_DeleteHostGroup(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/host_groups/7" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteHostGroup(context.Background(), 7); err != nil {
		t.Fatalf("expected no error for 204, got: %v", err)
	}
}

// The filters are server-side, so each one has to reach the wire under its
// documented name — the endpoint 400s on anything outside that set, and the
// "query" -> "q" rename is the one that would silently drop the whole filter.
func TestClient_ListHostGroups_Filters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_groups": []map[string]interface{}{},
			"meta":        map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	_, err := c.ListHostGroups(context.Background(), &ListHostGroupsOptions{
		Query:        "prod 100%",
		UpdatedSince: "2026-08-30T12:00:00Z",
		Order:        "name",
		Direction:    "desc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for key, want := range map[string]string{
		// A literal percent survives URL encoding rather than arriving as a
		// truncated term or a decode error.
		"q":             "prod 100%",
		"updated_since": "2026-08-30T12:00:00Z",
		"order":         "name",
		"direction":     "desc",
		"page":          "1",
		"per_page":      "100",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q on the wire, got %q", key, want, got)
		}
	}
	for key := range gotQuery {
		switch key {
		case "q", "updated_since", "order", "direction", "page", "per_page":
		default:
			t.Errorf("undocumented query parameter %q would 400 the request", key)
		}
	}
}

// Unset filters must be absent, not empty-valued: "q=" is a filter matching
// everything on some endpoints and an error on others, and neither is "no filter".
func TestClient_ListHostGroups_OmitsUnsetFilters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_groups": []map[string]interface{}{},
			"meta":        map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	// Both an empty options struct and a nil one mean "no filters".
	for _, opts := range []*ListHostGroupsOptions{{}, nil} {
		if _, err := c.ListHostGroups(context.Background(), opts); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, key := range []string{"q", "updated_since", "order", "direction"} {
			if _, ok := gotQuery[key]; ok {
				t.Errorf("opts %v: expected %s to be omitted when unset, got %q", opts, key, gotQuery.Get(key))
			}
		}
	}
}

func TestClient_ListHostGroups_Pagination(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := int(atomic.AddInt32(&requestCount, 1))
		if got := r.URL.Query().Get("page"); got != strconv.Itoa(page) {
			t.Errorf("request %d asked for page %q, want %d", page, got, page)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_groups": []map[string]interface{}{
				{"id": page, "name": fmt.Sprintf("group-%d", page), "position": page},
			},
			"meta": map[string]int{"current_page": page, "total_pages": 3, "total_count": 3, "per_page": 1},
		})
	})

	groups, err := c.ListHostGroups(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("expected 3 host groups across 3 pages, got %d", len(groups))
	}
	for i, want := range []int64{1, 2, 3} {
		if groups[i].ID != want {
			t.Errorf("group %d: expected id %d, got %d", i, want, groups[i].ID)
		}
	}
}

// An unrecognised meta envelope (every field zero) must still terminate: morePages
// falls back to walking until an empty page, and a list loop that trusted the old
// count/total/offset fields would spin here instead.
func TestClient_ListHostGroups_UnrecognisedMetaStopsOnEmptyPage(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := int(atomic.AddInt32(&requestCount, 1))
		groups := []map[string]interface{}{}
		if page == 1 {
			groups = append(groups, map[string]interface{}{"id": 1, "name": "only", "position": 1})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_groups": groups,
			"meta":        map[string]int{"count": len(groups), "total": 1, "offset": 0},
		})
	})

	groups, err := c.ListHostGroups(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 host group, got %d", len(groups))
	}
	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Errorf("expected 2 requests (one page of data, one empty), got %d", got)
	}
}
