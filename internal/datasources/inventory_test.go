package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The twenty collectors are declared in a table, so the invariants that a
// hand-written data source would get from the compiler are asserted here.
func TestInventoryCollectors_TableIsWellFormed(t *testing.T) {
	if len(inventoryCollectors) != 20 {
		t.Errorf("expected 20 collector inventories, got %d", len(inventoryCollectors))
	}

	seen := map[string]bool{}
	for _, c := range inventoryCollectors {
		if seen[c.name] {
			t.Errorf("duplicate collector %q", c.name)
		}
		seen[c.name] = true

		if c.desc == "" {
			t.Errorf("%s: no description", c.name)
		}
		if len(c.fields) == 0 {
			t.Errorf("%s: no fields", c.name)
		}

		names := map[string]bool{}
		for _, f := range c.fields {
			if names[f.name] {
				t.Errorf("%s: duplicate field %q", c.name, f.name)
			}
			names[f.name] = true
			if f.desc == "" {
				t.Errorf("%s.%s: no description", c.name, f.name)
			}
		}

		// A filter shares the top level with instance_id, the rows attribute
		// and the collector block, so a collision would silently shadow one.
		reserved := map[string]bool{"instance_id": true, "collector": true, c.name: true}
		filters := map[string]bool{}
		for _, f := range c.filters {
			if reserved[f.name] || filters[f.name] {
				t.Errorf("%s: filter %q collides with another top-level attribute", c.name, f.name)
			}
			filters[f.name] = true
			if f.kind != fieldString && f.kind != fieldBool {
				t.Errorf("%s: filter %q must be a string or a bool", c.name, f.name)
			}
			if f.desc == "" {
				t.Errorf("%s.%s: no description", c.name, f.name)
			}
		}
	}
}

// Every collector must produce a schema the framework can build a type from,
// and every one of them must expose the collector block.
func TestInventoryDataSources_Schemas(t *testing.T) {
	ctx := context.Background()
	constructors := InventoryDataSources()
	if len(constructors) != len(inventoryCollectors) {
		t.Fatalf("expected %d constructors, got %d", len(inventoryCollectors), len(constructors))
	}

	names := map[string]bool{}
	for _, ctor := range constructors {
		ds := ctor()

		metaResp := &datasource.MetadataResponse{}
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "fivenines"}, metaResp)
		if names[metaResp.TypeName] {
			t.Errorf("duplicate data source name %q", metaResp.TypeName)
		}
		names[metaResp.TypeName] = true

		resp := &datasource.SchemaResponse{}
		ds.Schema(ctx, datasource.SchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s: schema diagnostics: %v", metaResp.TypeName, resp.Diagnostics)
		}

		if _, ok := resp.Schema.Attributes["collector"].(schema.SingleNestedAttribute); !ok {
			t.Errorf("%s: missing the collector block", metaResp.TypeName)
		}
		if resp.Schema.Attributes["instance_id"] == nil {
			t.Errorf("%s: missing instance_id", metaResp.TypeName)
		}
		// Panics if the schema is malformed.
		_ = resp.Schema.Type().TerraformType(ctx)
	}

	if !names["fivenines_systemd_units"] || !names["fivenines_qemu_vms"] {
		t.Errorf("expected the documented data source names, got %v", names)
	}
}

// readInventory drives a full Read against a stub API and returns the state.
func readInventory(t *testing.T, name string, config map[string]tftypes.Value, handler http.HandlerFunc) tfsdk.State {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	var collector inventoryCollector
	for _, c := range inventoryCollectors {
		if c.name == name {
			collector = c
		}
	}
	if collector.name == "" {
		t.Fatalf("no collector named %q", name)
	}

	ctx := context.Background()
	ds := &inventoryDataSource{collector: collector, client: client.NewClient(srv.URL, "test-api-key")}

	schemaResp := &datasource.SchemaResponse{}
	ds.Schema(ctx, datasource.SchemaRequest{}, schemaResp)
	s := schemaResp.Schema

	objType := s.Type().TerraformType(ctx).(tftypes.Object)
	raw := map[string]tftypes.Value{}
	for attrName, attrType := range objType.AttributeTypes {
		if v, ok := config[attrName]; ok {
			raw[attrName] = v
			continue
		}
		raw[attrName] = tftypes.NewValue(attrType, nil)
	}

	resp := &datasource.ReadResponse{
		State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)},
	}
	ds.Read(ctx, datasource.ReadRequest{
		Config: tfsdk.Config{Schema: s, Raw: tftypes.NewValue(objType, raw)},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("read diagnostics: %v", resp.Diagnostics)
	}
	return resp.State
}

// The null-vs-zero contract is the reason these data sources exist: a null
// cgroup counter must not arrive in state as 0, and a null reverse_deps must
// not arrive as an empty list.
func TestInventoryDataSource_Read_PreservesNulls(t *testing.T) {
	state := readInventory(t, "systemd_units",
		map[string]tftypes.Value{"instance_id": tftypes.NewValue(tftypes.String, "host-1")},
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"systemd_units": []map[string]interface{}{{
					"id":             1,
					"name":           "nginx.service",
					"active_state":   "failed",
					"failed":         true,
					"memory_current": 8388608,
					"oom_kill_count": nil,
					"journal_tail":   []string{"boom"},
					"reverse_deps":   nil,
					"stale":          false,
				}},
				"collector": map[string]interface{}{
					"name": "systemd", "enabled": true, "supported": true, "pending": false,
					"unavailable_reason": nil, "blocked_reason": nil,
					"last_reported_at": "2026-08-30T12:00:00Z",
				},
				"meta": map[string]int{"current_page": 1, "total_pages": 1},
			})
		})

	ctx := context.Background()
	var units types.List
	if diags := state.GetAttribute(ctx, path.Root("systemd_units"), &units); diags.HasError() {
		t.Fatalf("reading units: %v", diags)
	}
	if len(units.Elements()) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Elements()))
	}

	row := units.Elements()[0].(types.Object).Attributes()
	if got := row["oom_kill_count"]; !got.IsNull() {
		t.Errorf("oom_kill_count: expected null, got %v -- a 0 would read as \"nothing was OOM killed\"", got)
	}
	if got := row["reverse_deps"]; !got.IsNull() {
		t.Errorf("reverse_deps: expected null, got %v -- [] would mean \"nothing depends on this unit\"", got)
	}
	if got := row["memory_current"].(types.Int64).ValueInt64(); got != 8388608 {
		t.Errorf("memory_current: got %d", got)
	}
	if got := row["journal_tail"].(types.List); len(got.Elements()) != 1 {
		t.Errorf("journal_tail: expected 1 line, got %v", got)
	}
	// A column the response omitted altogether is null too, not a zero.
	if got := row["cpu_usec"]; !got.IsNull() {
		t.Errorf("cpu_usec: expected null for an absent column, got %v", got)
	}

	var collector types.Object
	if diags := state.GetAttribute(ctx, path.Root("collector"), &collector); diags.HasError() {
		t.Fatalf("reading collector: %v", diags)
	}
	attrs := collector.Attributes()
	if attrs["name"].(types.String).ValueString() != "systemd" {
		t.Errorf("collector.name: got %v", attrs["name"])
	}
	if !attrs["enabled"].(types.Bool).ValueBool() {
		t.Error("collector.enabled: expected true")
	}
	if !attrs["unavailable_reason"].IsNull() {
		t.Errorf("collector.unavailable_reason: expected null, got %v", attrs["unavailable_reason"])
	}
}

// An empty list with the collector switched off is the case the block exists
// for: the rows were deleted, and "no containers" would be a confident lie.
func TestInventoryDataSource_Read_EmptyListCarriesCollectorBlock(t *testing.T) {
	state := readInventory(t, "docker_containers",
		map[string]tftypes.Value{"instance_id": tftypes.NewValue(tftypes.String, "host-1")},
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"docker_containers": []interface{}{},
				"collector": map[string]interface{}{
					"name": "docker", "enabled": false, "supported": false, "pending": false,
					"unavailable_reason": "agent_outdated", "blocked_reason": "docker socket not readable",
				},
				"meta": map[string]int{"current_page": 1, "total_pages": 1},
			})
		})

	ctx := context.Background()
	var containers types.List
	state.GetAttribute(ctx, path.Root("docker_containers"), &containers)
	if len(containers.Elements()) != 0 {
		t.Errorf("expected no containers, got %d", len(containers.Elements()))
	}

	var collector types.Object
	state.GetAttribute(ctx, path.Root("collector"), &collector)
	attrs := collector.Attributes()
	if attrs["enabled"].(types.Bool).ValueBool() {
		t.Error("collector.enabled: expected false")
	}
	if got := attrs["unavailable_reason"].(types.String).ValueString(); got != "agent_outdated" {
		t.Errorf("collector.unavailable_reason: got %q", got)
	}
	if got := attrs["blocked_reason"].(types.String).ValueString(); got != "docker socket not readable" {
		t.Errorf("collector.blocked_reason: got %q", got)
	}
}

// Tombstoned VMs are the one deletion signal in this whole family, so the
// default read must not filter them out.
func TestInventoryDataSource_Read_QemuTombstonesAreNotFiltered(t *testing.T) {
	var gotVanishedFilter bool
	state := readInventory(t, "qemu_vms",
		map[string]tftypes.Value{"instance_id": tftypes.NewValue(tftypes.String, "host-1")},
		func(w http.ResponseWriter, r *http.Request) {
			_, gotVanishedFilter = r.URL.Query()["vanished"]
			json.NewEncoder(w).Encode(map[string]interface{}{
				"qemu_vms": []map[string]interface{}{
					{"id": "a", "vm_uuid": "u1", "status": "running", "vanished": false, "metrics_fresh": true, "cpu_percent": 42.5},
					{"id": "b", "vm_uuid": "u2", "status": "shutoff", "vanished": true, "vanished_at": "2026-08-30T12:00:00Z", "metrics_fresh": false, "cpu_percent": nil},
				},
				"collector": map[string]interface{}{"name": "qemu", "enabled": true, "supported": true},
				"meta":      map[string]int{"current_page": 1, "total_pages": 1},
			})
		})

	if gotVanishedFilter {
		t.Error("an unset vanished filter must not be sent: omitting it is what returns the tombstones")
	}

	ctx := context.Background()
	var vms types.List
	state.GetAttribute(ctx, path.Root("qemu_vms"), &vms)
	if len(vms.Elements()) != 2 {
		t.Fatalf("expected both VMs including the tombstone, got %d", len(vms.Elements()))
	}

	tombstone := vms.Elements()[1].(types.Object).Attributes()
	if !tombstone["vanished"].(types.Bool).ValueBool() {
		t.Error("expected the second row to be a tombstone")
	}
	// A stale sample comes back null, never as the last known number.
	if !tombstone["cpu_percent"].IsNull() {
		t.Errorf("cpu_percent: expected null on a row with metrics_fresh=false, got %v", tombstone["cpu_percent"])
	}
	live := vms.Elements()[0].(types.Object).Attributes()
	if got := live["cpu_percent"].(types.Float64).ValueFloat64(); got != 42.5 {
		t.Errorf("cpu_percent: got %v", got)
	}
}

// A never-scanned image must not read as "0 vulnerabilities".
func TestInventoryDataSource_Read_UnscannedImageCountsAreNull(t *testing.T) {
	state := readInventory(t, "docker_images",
		map[string]tftypes.Value{
			"instance_id": tftypes.NewValue(tftypes.String, "host-1"),
			"state":       tftypes.NewValue(tftypes.String, "pending"),
		},
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("state"); got != "pending" {
				t.Errorf("expected the state filter to reach the API, got %q", got)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"docker_images": []map[string]interface{}{{
					"id": "img-1", "image_id": "sha256:abc", "state": "pending",
					"countable": false, "vulnerability_count": nil,
					"critical_vulnerability_count": nil, "packages_truncated": false,
					"finding_count_is_floor": false, "tags": []string{"nginx:1.27"},
				}},
				"collector": map[string]interface{}{"name": "docker", "enabled": true, "supported": true},
				"meta":      map[string]int{"current_page": 1, "total_pages": 1},
			})
		})

	ctx := context.Background()
	var images types.List
	state.GetAttribute(ctx, path.Root("docker_images"), &images)
	row := images.Elements()[0].(types.Object).Attributes()

	if !row["vulnerability_count"].IsNull() {
		t.Errorf("vulnerability_count: expected null for an unscanned image, got %v", row["vulnerability_count"])
	}
	if row["countable"].(types.Bool).ValueBool() {
		t.Error("countable: expected false")
	}
	// The org-wide blast-radius fields are not on the per-instance endpoint.
	if _, ok := row["running_host_count"]; ok {
		t.Error("running_host_count must not be exposed: the per-instance endpoint does not return it")
	}
}

// A free-form column with no fixed shape is exposed as its JSON encoding.
func TestInventoryDataSource_Read_FreeFormColumnsAreJSON(t *testing.T) {
	state := readInventory(t, "zfs_pools",
		map[string]tftypes.Value{"instance_id": tftypes.NewValue(tftypes.String, "host-1")},
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"zfs_pools": []map[string]interface{}{{
					"id": 1, "name": "tank", "health": "DEGRADED", "problem": true,
					"scrub_errors": nil, "degraded_vdevs": nil,
					"vdev_tree": map[string]interface{}{"type": "mirror", "state": "DEGRADED"},
				}},
				"collector": map[string]interface{}{"name": "zfs", "enabled": true, "supported": true},
				"meta":      map[string]int{"current_page": 1, "total_pages": 1},
			})
		})

	ctx := context.Background()
	var pools types.List
	state.GetAttribute(ctx, path.Root("zfs_pools"), &pools)
	row := pools.Elements()[0].(types.Object).Attributes()

	var tree map[string]interface{}
	if err := json.Unmarshal([]byte(row["vdev_tree"].(types.String).ValueString()), &tree); err != nil {
		t.Fatalf("vdev_tree is not valid JSON: %v", err)
	}
	if tree["state"] != "DEGRADED" {
		t.Errorf("vdev_tree: got %v", tree)
	}
	// "DEGRADED with an unknown extent" is a real row, and 0 would deny it.
	if !row["degraded_vdevs"].IsNull() {
		t.Errorf("degraded_vdevs: expected null, got %v", row["degraded_vdevs"])
	}
	if !row["scrub_errors"].IsNull() {
		t.Errorf("scrub_errors: expected null -- 0 would mean a scrub completed clean")
	}
}

// Without the block an empty list is exactly the ambiguity these data sources
// exist to remove, so a response missing it must fail loudly.
func TestInventoryDataSource_Read_RequiresCollectorBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"listening_ports": []interface{}{},
			"meta":            map[string]int{"current_page": 1, "total_pages": 1},
		})
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	var collector inventoryCollector
	for _, c := range inventoryCollectors {
		if c.name == "listening_ports" {
			collector = c
		}
	}
	ds := &inventoryDataSource{collector: collector, client: client.NewClient(srv.URL, "k")}

	schemaResp := &datasource.SchemaResponse{}
	ds.Schema(ctx, datasource.SchemaRequest{}, schemaResp)
	objType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)

	raw := map[string]tftypes.Value{}
	for n, tp := range objType.AttributeTypes {
		raw[n] = tftypes.NewValue(tp, nil)
	}
	raw["instance_id"] = tftypes.NewValue(tftypes.String, "host-1")

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objType, nil)}}
	ds.Read(ctx, datasource.ReadRequest{
		Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objType, raw)},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when the response carries no collector block")
	}
}
