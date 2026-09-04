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

// A negative cap is a caller bug, and the zero-means-unbounded sentinel makes the
// dangerous reading (walk everything) the natural one. Refuse it instead.
func TestListAllPages_RejectsNegativeRowCap(t *testing.T) {
	var requests int
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []map[string]interface{}{{"id": "task-uuid"}},
			"meta":  map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	_, err := listAllPages[Task](context.Background(), c, "/api/v1/tasks", "tasks", nil, 100, -5)
	if err == nil {
		t.Fatal("expected a negative row cap to be refused, not read as unbounded")
	}
	if requests != 0 {
		t.Errorf("expected the walk to be refused before any request, got %d", requests)
	}
}

// The offset-pagination insert race: a task created between two page requests
// pushes the last row of page 1 onto page 2, so it arrives twice. A duplicate id
// fails a Terraform plan outright on `{ for t in ... : t.id => t }`, and a data
// source re-reads on every plan, so it recurs nondeterministically.
func TestListAllPages_DropsRowsRepeatedAcrossPages(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var ids []string
		switch page {
		case "1":
			ids = []string{"a", "b", "c"}
		default:
			// "c" slid onto page 2 when a new task was inserted mid-walk.
			ids = []string{"c", "d"}
		}
		rows := make([]map[string]interface{}, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, map[string]interface{}{"id": id, "name": "backup"})
		}
		current, _ := strconv.Atoi(page)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": rows,
			"meta":  map[string]int{"current_page": current, "total_pages": 2, "total_count": 5, "per_page": 3},
		})
	})

	tasks, err := listAllPages[Task](context.Background(), c, "/api/v1/tasks", "tasks", nil, 3, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got []string
	seen := map[string]int{}
	for _, task := range tasks {
		got = append(got, task.ID)
		seen[task.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("task %q returned %d times; a repeated id fails a Terraform plan", id, n)
		}
	}
	if len(got) != 4 {
		t.Errorf("expected 4 distinct tasks, got %d (%v)", len(got), got)
	}
}

// De-duplication happens BEFORE the cap, so a repeated row does not eat a slot
// the caller asked for: limit 3 over a duplicate-bearing walk still yields 3.
func TestListAllPages_DuplicatesDoNotConsumeTheRowCap(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var ids []string
		switch page {
		case "1":
			ids = []string{"a", "b"}
		default:
			ids = []string{"b", "c"}
		}
		rows := make([]map[string]interface{}, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, map[string]interface{}{"id": id, "name": "backup"})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": rows,
			"meta":  map[string]int{"current_page": 1, "total_pages": 3, "total_count": 6, "per_page": 2},
		})
	})

	tasks, err := listAllPages[Task](context.Background(), c, "/api/v1/tasks", "tasks", nil, 2, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected the cap filled with 3 DISTINCT tasks, got %d", len(tasks))
	}
	want := []string{"a", "b", "c"}
	for i, id := range want {
		if tasks[i].ID != id {
			t.Errorf("row %d: expected %q, got %q", i, id, tasks[i].ID)
		}
	}
}

// A row type with no identity still walks: de-duplication is opt-in via
// rowIdentifier, and nothing is dropped for want of an id.
func TestListAllPages_RowsWithoutIdentityAreNotDropped(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"rows": []map[string]interface{}{{"v": 1}, {"v": 1}, {"v": 2}},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 3, "per_page": 100},
		})
	})

	type plainRow struct {
		V int `json:"v"`
	}
	rows, err := listAllPages[plainRow](context.Background(), c, "/api/v1/anything", "rows", nil, 100, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected all 3 rows kept for a type with no rowID, got %d", len(rows))
	}
}
