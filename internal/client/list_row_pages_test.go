package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// listRowPages is the walker ListInventory was refactored onto and that the four
// Proxmox routes now share. These cover the branches that only the shared walker
// has, plus the behaviours the listRowPages extraction had to preserve.

// The collector block comes from the FIRST PAGE THAT CARRIES ONE — the
// behaviour the pre-refactor loop had, preserved across the listRowPages
// extraction. listRowPages invokes onPage on EVERY page and ListInventory's
// closure keeps the first block it sees (`!ok || status != nil` → skip), so the
// keep-the-first decision belongs to the caller rather than to the walker.
//
// Both halves are pinned because the tempting simplification — read only page 1
// — is a silent narrowing: a response that omits the block on page 1 and sends
// it on page 2 would yield a nil status, which the data source turns into a hard
// "Missing collector block" error for a walk that in fact carried one.
func TestClient_ListInventory_CollectorBlockComesFromTheFirstPageThatCarriesOne(t *testing.T) {
	t.Run("page 1 wins over a later page", func(t *testing.T) {
		_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			page := r.URL.Query().Get("page")
			name := "systemd"
			if page != "1" {
				name = "not-the-first-page"
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"systemd_units": []map[string]interface{}{{"name": "a.service"}},
				"collector":     map[string]interface{}{"name": name, "enabled": true},
				"meta": map[string]int{
					"current_page": mustAtoi(t, page), "total_pages": 2,
					"total_count": 2, "per_page": 100,
				},
			})
		})

		_, status, err := c.ListInventory(context.Background(), "host-1", "systemd_units", nil)
		if err != nil {
			t.Fatalf("ListInventory: %v", err)
		}
		if status == nil || status.Name != "systemd" {
			t.Errorf("expected the first page's collector block to win, got %+v", status)
		}
	})

	// Tolerating a late block is the behaviour the pre-refactor loop had, and
	// it is strictly safer than the alternative: without a status the data
	// source hard-errors, so a page 1 that happened to omit the block would
	// fail a walk whose later pages carried it.
	t.Run("a block on a later page is still picked up", func(t *testing.T) {
		_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			page := r.URL.Query().Get("page")
			body := map[string]interface{}{
				"systemd_units": []map[string]interface{}{{"name": "a.service"}},
				"meta": map[string]int{
					"current_page": mustAtoi(t, page), "total_pages": 2,
					"total_count": 2, "per_page": 100,
				},
			}
			if page != "1" {
				body["collector"] = map[string]interface{}{"name": "systemd", "enabled": true}
			}
			json.NewEncoder(w).Encode(body)
		})

		_, status, err := c.ListInventory(context.Background(), "host-1", "systemd_units", nil)
		if err != nil {
			t.Fatalf("ListInventory: %v", err)
		}
		if status == nil || status.Name != "systemd" {
			t.Errorf("expected the later page's collector block to be picked up, got %+v", status)
		}
	})
}

// The refactor also started skipping empty filter values. It used to send them,
// and an empty query parameter is a 400 on these endpoints rather than a no-op,
// so a caller that passed "" got a failed read where it now gets an unfiltered
// one. Pinned here because ListInventory is the caller the change was not
// written for.
func TestClient_ListInventory_OmitsEmptyFilterValues(t *testing.T) {
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
		map[string]string{"status": "running", "q": "", "vanished": ""})
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	if got := query.Get("status"); got != "running" {
		t.Errorf("expected status=running to survive, got %q", got)
	}
	for _, key := range []string{"q", "vanished"} {
		if _, ok := query[key]; ok {
			t.Errorf("expected the empty filter %q to be omitted, got %q", key, query.Get(key))
		}
	}
	// The walk's own parameters are never dropped.
	if query.Get("page") != "1" || query.Get("per_page") != "100" {
		t.Errorf("unexpected pagination parameters: %v", query)
	}
}

// A `collector` block the response mangles must fail loudly rather than read as
// absent: absent means "API contract violation" to the data source, and a
// malformed one would be reported as the wrong problem.
func TestClient_ListInventory_MalformedCollectorBlockIsAnError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"systemd_units": [], "collector": "switched off", "meta": {"current_page": 1, "total_pages": 1}}`))
	})

	_, _, err := c.ListInventory(context.Background(), "host-1", "systemd_units", nil)
	if err == nil {
		t.Fatal("expected an error for a collector block that is not an object")
	}
	if !strings.Contains(err.Error(), "decoding collector block") {
		t.Errorf("expected the collector block to be named in the error, got %v", err)
	}
}

// The three decode failures the shared walker can hit. Driven through a Proxmox
// route because that one passes no onPage callback, so these are the walker's
// own branches rather than ListInventory's.
func TestClient_ListRowPages_MalformedBodyIsAnError(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "rows are not an array",
			body: `{"proxmox_nodes": {"id": "node-1"}, "meta": {"current_page": 1, "total_pages": 1}}`,
			want: "decoding proxmox_nodes rows",
		},
		{
			name: "meta is not an object",
			body: `{"proxmox_nodes": [], "meta": "none"}`,
			want: "decoding pagination meta",
		},
		{
			name: "the body is not an object at all",
			body: `[{"id": "node-1"}]`,
			want: "decoding response",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tt.body))
			})

			_, err := c.ListProxmoxClusterRows(context.Background(), "cluster-uuid", "nodes", "proxmox_nodes", nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected an error mentioning %q, got %v", tt.want, err)
			}
		})
	}
}

// The cluster child routes must walk every page, not stop at the first. This is
// the failure class TODOS.md records, and these routes reach the walker through a
// different entry point than the one ListInventory's pagination test covers.
func TestClient_ListProxmoxClusterRows_Paginates(t *testing.T) {
	var pages []string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_guests": []map[string]interface{}{{"id": "guest-" + page}},
			"meta": map[string]int{
				"current_page": mustAtoi(t, page), "total_pages": 3,
				"total_count": 3, "per_page": 100,
			},
		})
	})

	rows, err := c.ListProxmoxClusterRows(context.Background(), "cluster-uuid", "guests", "proxmox_guests", nil)
	if err != nil {
		t.Fatalf("ListProxmoxClusterRows: %v", err)
	}
	if len(pages) != 3 {
		t.Errorf("expected the walk to follow the envelope across 3 pages, got %v", pages)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[2]["id"] != "guest-3" {
		t.Errorf("expected the last page's rows to be kept, got %v", rows[2])
	}
}

// A `"collector": null` must read as ABSENT, not as a zeroed block.
//
// encoding/json unmarshals null into a struct as a silent no-op: no error, and
// every field left at its zero value. Accepting it would publish
// `enabled: false` -- documented as "switching it off deletes the rows, so
// false fully explains an empty list" -- which is a confident all-clear for a
// host the API declined to answer about. This is the single most dangerous
// shape the collector contract can take, because it looks like a successful
// read all the way through.
func TestClient_ListInventory_NullCollectorBlockCountsAsAbsent(t *testing.T) {
	// First: pin the encoding/json behaviour the bug rests on, so this test
	// still explains itself if the guard is ever removed.
	var cs CollectorStatus
	if err := json.Unmarshal([]byte("null"), &cs); err != nil {
		t.Fatalf("expected encoding/json to accept a null into a struct, got %v", err)
	}
	if cs.Enabled || cs.Name != "" {
		t.Fatalf("expected a null to leave the struct zeroed, got %+v", cs)
	}

	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"systemd_units": [], "collector": null, "meta": {"current_page": 1, "total_pages": 1}}`))
	})

	_, status, err := c.ListInventory(context.Background(), "host-1", "systemd_units", nil)
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	// nil, so inventoryDataSource.Read raises its contract error rather than
	// reporting a switched-off collector.
	if status != nil {
		t.Errorf("expected a null collector block to read as absent, got %+v", status)
	}
}
