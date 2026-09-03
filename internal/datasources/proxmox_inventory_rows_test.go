package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// fieldValue and nullValue were written for the twenty per-instance collectors
// and are now the row mapper for the four Proxmox data sources too. Their type
// guards are the only thing between a server that changes a column's type and a
// silently wrong value in state, and every one of them was unreachable from the
// existing suite.
func TestFieldValue_TypeErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		kind fieldKind
		raw  interface{}
		want string
	}{
		{"an integer column carrying a string", fieldInt, "12", "expected a number"},
		{"an integer column carrying a bool", fieldInt, true, "expected a number"},
		{"an integer column carrying a non-numeric number", fieldInt, json.Number("not-a-number"), "expected an integer"},
		{"a float column carrying a string", fieldFloat, "1.5", "expected a number"},
		{"a float column carrying a malformed number", fieldFloat, json.Number("1.2.3"), "expected a number"},
		{"a bool column carrying a string", fieldBool, "true", "expected a boolean"},
		{"a list column carrying a string", fieldStringList, "a,b", "expected an array"},
		{"a list column carrying non-strings", fieldStringList, []interface{}{"a", json.Number("2")}, "expected an array of strings"},
		{"a string column carrying a number", fieldString, json.Number("7"), "expected a string"},
		{"a string column carrying an object", fieldString, map[string]interface{}{"a": 1}, "expected a string"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v, err := fieldValue(inventoryField{name: "col", kind: tt.kind}, tt.raw)
			if err == nil {
				t.Fatalf("expected an error, got %v", v)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected an error mentioning %q, got %v", tt.want, err)
			}
		})
	}
}

// A whole number the API renders as a float still belongs in an int64 column --
// otherwise a serializer that emits 3.0 instead of 3 turns a node count into a
// plan error.
func TestFieldValue_WholeNumberRenderedAsAFloat(t *testing.T) {
	v, err := fieldValue(inventoryField{name: "nodes_total", kind: fieldInt}, json.Number("3.0"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	i, ok := v.(types.Int64)
	if !ok {
		t.Fatalf("expected an Int64, got %T", v)
	}
	if i.ValueInt64() != 3 {
		t.Errorf("expected 3, got %d", i.ValueInt64())
	}
}

// The Proxmox data sources reuse fieldValue, so its errors have to surface as a
// diagnostic on THEIR name rather than being dropped or panicking. Nothing in the
// existing suite reached this branch from the new data sources.
func TestProxmoxInventory_Read_WrongTypedFieldIsADiagnostic(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_nodes": []interface{}{
				map[string]interface{}{
					"id": "node-1", "proxmox_cluster_id": "cluster-uuid", "name": "pve1",
					"status": "online",
					// `stale` is a bool column; a server sending the string is
					// the contract break this guard exists for.
					"stale": "yes",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	d := &proxmoxInventoryDataSource{inventory: proxmoxInventoryByName(t, "proxmox_cluster_nodes"), client: c}
	state, resp := readDataSource(t, d, map[string]tftypes.Value{
		"cluster_id": tftypes.NewValue(tftypes.String, "cluster-uuid"),
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic for a wrongly typed column")
	}
	got := resp.Diagnostics.Errors()[0]
	if got.Summary() != "Unexpected value in proxmox_cluster_nodes response" {
		t.Errorf("unexpected summary: %q", got.Summary())
	}
	// The row index and column name are what make the report actionable.
	if !strings.Contains(got.Detail(), `Row 0, field "stale"`) {
		t.Errorf("expected the row and field to be named, got %q", got.Detail())
	}
	if !state.Raw.IsNull() {
		t.Errorf("expected state to be left null, got %v", state.Raw)
	}
}

// proxmox_cluster_storages is the one cluster-scoped inventory nothing exercised:
// its own field table, its `active` boolean filter, and the `pool` / `zpool_root`
// nulls that separate an agent too old to report the backing dataset from one
// reporting an empty one.
func TestProxmoxClusterStorages_Read(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_storages": []interface{}{
				map[string]interface{}{
					"id": "storage-1", "proxmox_cluster_id": "cluster-uuid",
					"proxmox_node_id": "node-1", "node_name": "pve1", "name": "local-zfs",
					"storage_type": "zfspool", "active": true, "pool": "rpool/data",
					"zpool_root": "rpool", "zfs_backed": true,
					"last_synced_at": "2026-02-01T00:00:00Z", "stale": false,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-02-01T00:00:00Z",
				},
				// An agent older than 1.11.7 reports no backing pool. Null, never
				// "": zpool_root is what correlates the row to a ZFS pool, and an
				// empty string would silently correlate to nothing.
				map[string]interface{}{
					"id": "storage-2", "proxmox_cluster_id": "cluster-uuid",
					"proxmox_node_id": "node-2", "node_name": "pve2", "name": "nfs-backups",
					"storage_type": nil, "active": false, "pool": nil,
					"zpool_root": nil, "zfs_backed": false,
					"last_synced_at": nil, "stale": true,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-02-01T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	d := &proxmoxInventoryDataSource{inventory: proxmoxInventoryByName(t, "proxmox_cluster_storages"), client: c}
	state, resp := readDataSource(t, d, map[string]tftypes.Value{
		"cluster_id":   tftypes.NewValue(tftypes.String, "cluster-uuid"),
		"active":       tftypes.NewValue(tftypes.Bool, true),
		"storage_type": tftypes.NewValue(tftypes.String, "zfspool"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// The route segment is `storages` while the envelope key is
	// `proxmox_storages`; getting either wrong yields an empty list, not an error.
	if gotPath != "/api/v1/proxmox_clusters/cluster-uuid/storages" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if got := gotQuery.Get("active"); got != "true" {
		t.Errorf("expected active=true, got %q", got)
	}
	if got := gotQuery.Get("storage_type"); got != "zfspool" {
		t.Errorf("expected storage_type=zfspool, got %q", got)
	}
	for _, key := range []string{"stale", "q", "updated_since"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected the unset filter %q to be omitted", key)
		}
	}

	var rows types.List
	if diags := state.GetAttribute(context.Background(), path.Root("proxmox_cluster_storages"), &rows); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(rows.Elements()) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows.Elements()))
	}

	older, ok := rows.Elements()[1].(types.Object)
	if !ok {
		t.Fatalf("expected an object row, got %T", rows.Elements()[1])
	}
	for _, name := range []string{"pool", "zpool_root", "storage_type", "last_synced_at"} {
		if v := older.Attributes()[name]; !v.IsNull() {
			t.Errorf("expected %s to be null on a row that did not report it, got %v", name, v)
		}
	}
	// `active` false is a real reading, not an absence, and must not read null.
	active, ok := older.Attributes()["active"].(types.Bool)
	if !ok || active.IsNull() || active.ValueBool() {
		t.Errorf("expected active=false, got %v", older.Attributes()["active"])
	}
}

// Zero rows must serialise as an empty list rather than null on both route
// shapes: length()/for_each/toset over a null fail, and "this cluster has no
// guests" is a completely ordinary answer here.
func TestProxmoxInventory_Read_EmptyIsAnEmptyListNotNull(t *testing.T) {
	for _, tt := range []struct {
		inventory string
		key       string
		config    map[string]tftypes.Value
	}{
		{
			inventory: "proxmox_cluster_guests",
			key:       "proxmox_guests",
			config:    map[string]tftypes.Value{"cluster_id": tftypes.NewValue(tftypes.String, "cluster-uuid")},
		},
		{inventory: "organization_proxmox_guests", key: "proxmox_guests"},
	} {
		t.Run(tt.inventory, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{
					tt.key: []interface{}{},
					"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
				})
			})

			d := &proxmoxInventoryDataSource{inventory: proxmoxInventoryByName(t, tt.inventory), client: c}
			state, resp := readDataSource(t, d, tt.config)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
			}

			var rows types.List
			if diags := state.GetAttribute(context.Background(), path.Root(tt.inventory), &rows); diags.HasError() {
				t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
			}
			if rows.IsNull() {
				t.Fatal("expected an empty list, got null")
			}
			if len(rows.Elements()) != 0 {
				t.Errorf("expected no rows, got %d", len(rows.Elements()))
			}
		})
	}
}
