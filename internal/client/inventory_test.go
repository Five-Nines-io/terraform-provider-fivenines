package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func TestClient_ListInventory_Pagination(t *testing.T) {
	var pages []string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/instances/host-1/systemd_units" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("expected per_page=100, got %q", got)
		}
		name := "a.service"
		if page == "2" {
			name = "b.service"
		}
		// current_page has to echo the request: morePages compares it against
		// total_pages, so a stub that always says page 1 would walk forever.
		current, _ := strconv.Atoi(page)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"systemd_units": []map[string]interface{}{{"name": name}},
			"collector":     map[string]interface{}{"name": "systemd", "enabled": true, "supported": true},
			"meta":          map[string]int{"current_page": current, "total_pages": 2, "total_count": 2, "per_page": 100},
		})
	})

	rows, status, err := c.ListInventory(context.Background(), "host-1", "systemd_units", nil)
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	if len(pages) != 2 || pages[0] != "1" || pages[1] != "2" {
		t.Errorf("expected pages 1 and 2, got %v", pages)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "a.service" || rows[1]["name"] != "b.service" {
		t.Errorf("unexpected rows: %v", rows)
	}
	if status == nil || status.Name != "systemd" || !status.Enabled {
		t.Errorf("unexpected collector status: %+v", status)
	}
}

// An unrecognised meta envelope must walk to an empty page rather than stop
// after one. This is morePages' deliberate choice, and the reason the whole
// provider stopped truncating at 100 rows: the last envelope rename decoded to
// all zeros, and a walk that trusts a zeroed counter drops data silently.
// Over-fetching one page is the cheap failure; a short list with no error is not.
func TestClient_ListInventory_UnreadableMetaWalksToEmptyPage(t *testing.T) {
	var calls int
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		rows := []map[string]interface{}{{"name": "tank"}}
		if r.URL.Query().Get("page") != "1" {
			rows = nil
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"zfs_pools": rows,
			"collector": map[string]interface{}{"name": "zfs"},
		})
	})

	rows, _, err := c.ListInventory(context.Background(), "host-1", "zfs_pools", nil)
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected the walk to continue past an unreadable meta and stop on the empty page (2 requests), got %d", calls)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
}

// A server that ignores `page` while serving an unrecognised meta would
// otherwise append the same rows forever. maxListPages turns that into an
// error instead of an unbounded request stream inside a terraform plan.
func TestClient_ListInventory_BoundsAServerThatIgnoresPage(t *testing.T) {
	var calls int
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"zfs_pools": []map[string]interface{}{{"name": "tank"}},
			"collector": map[string]interface{}{"name": "zfs"},
		})
	})

	_, _, err := c.ListInventory(context.Background(), "host-1", "zfs_pools", nil)
	if err == nil {
		t.Fatal("expected an error once the page cap is hit, got nil")
	}
	if calls < 2 {
		t.Errorf("expected the walk to actually iterate, got %d requests", calls)
	}
}

func TestClient_ListInventory_SendsFilters(t *testing.T) {
	var query url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"qemu_vms":  []map[string]interface{}{},
			"collector": map[string]interface{}{"name": "qemu"},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1},
		})
	})

	_, _, err := c.ListInventory(context.Background(), "host-1", "qemu_vms",
		map[string]string{"vanished": "false", "status": "running"})
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	if got := query.Get("vanished"); got != "false" {
		t.Errorf("expected vanished=false, got %q", got)
	}
	if got := query.Get("status"); got != "running" {
		t.Errorf("expected status=running, got %q", got)
	}
}

// Numbers must survive as json.Number so an int64 column keeps its precision,
// and a JSON null must stay nil rather than becoming a zero value.
func TestClient_ListInventory_PreservesNullsAndPrecision(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
			"systemd_units": [{"memory_current": 9007199254740993, "oom_kill_count": null}],
			"collector": {"name": "systemd"},
			"meta": {"current_page": 1, "total_pages": 1}
		}`)
	})

	rows, _, err := c.ListInventory(context.Background(), "host-1", "systemd_units", nil)
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	n, ok := rows[0]["memory_current"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number, got %T", rows[0]["memory_current"])
	}
	if n.String() != "9007199254740993" {
		t.Errorf("lost precision: got %s", n.String())
	}
	v, present := rows[0]["oom_kill_count"]
	if !present || v != nil {
		t.Errorf("expected an explicit nil for a JSON null, got %#v (present=%v)", v, present)
	}
}

func TestClient_ListInventory_Error(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not Found"})
	})

	_, _, err := c.ListInventory(context.Background(), "nope", "docker_containers", nil)
	if err == nil {
		t.Fatal("expected an error for a 404")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != 404 {
		t.Errorf("expected a 404 APIError, got %v", err)
	}
}
