package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// All of them must be wired for Configure, otherwise d.client stays nil and
// Read panics on the first apply.
var (
	_ datasource.DataSourceWithConfigure = &cephClustersDataSource{}
	_ datasource.DataSourceWithConfigure = &cephClusterDataSource{}
	_ datasource.DataSourceWithConfigure = &proxmoxClustersDataSource{}
	_ datasource.DataSourceWithConfigure = &proxmoxClusterDataSource{}
	_ datasource.DataSourceWithConfigure = &proxmoxInventoryDataSource{}
	_ datasource.DataSourceWithConfigure = &instanceCapabilityStatusDataSource{}
	_ datasource.DataSourceWithConfigure = &statusPageSubscribersDataSource{}
)

func TestClusterDataSources_MetadataAndConfigure(t *testing.T) {
	for _, tt := range []struct {
		ds   datasource.DataSource
		want string
	}{
		{NewCephClustersDataSource(), "fivenines_ceph_clusters"},
		{NewCephClusterDataSource(), "fivenines_ceph_cluster"},
		{NewProxmoxClustersDataSource(), "fivenines_proxmox_clusters"},
		{NewProxmoxClusterDataSource(), "fivenines_proxmox_cluster"},
		{NewInstanceCapabilityStatusDataSource(), "fivenines_instance_capability_status"},
		{NewStatusPageSubscribersDataSource(), "fivenines_status_page_subscribers"},
	} {
		resp := &datasource.MetadataResponse{}
		tt.ds.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "fivenines"}, resp)
		if resp.TypeName != tt.want {
			t.Errorf("expected type name %q, got %q", tt.want, resp.TypeName)
		}
	}

	for _, tt := range []struct {
		name         string
		providerData interface{}
		wantError    bool
	}{
		// Terraform calls Configure with nil data before the provider is
		// configured; that must be a quiet no-op, not an error.
		{name: "nil provider data", providerData: nil},
		{name: "correct client", providerData: client.NewClient("https://example.com", "key")},
		{name: "wrong type", providerData: "not a client", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, ds := range []datasource.DataSourceWithConfigure{
				&cephClustersDataSource{},
				&cephClusterDataSource{},
				&proxmoxClustersDataSource{},
				&proxmoxClusterDataSource{},
				&proxmoxInventoryDataSource{},
				&instanceCapabilityStatusDataSource{},
				&statusPageSubscribersDataSource{},
			} {
				resp := &datasource.ConfigureResponse{}
				ds.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: tt.providerData}, resp)
				if resp.Diagnostics.HasError() != tt.wantError {
					t.Errorf("%T: expected error=%v, got %v", ds, tt.wantError, resp.Diagnostics.Errors())
				}
			}
		})
	}
}

// --- ceph_clusters ---

func TestCephClustersDataSource_Read(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ceph_clusters": []interface{}{
				map[string]interface{}{
					"fsid": "8e4a-prod", "name": "prod-ceph", "configured_name": "prod-ceph",
					"promoted": true, "promoted_at": "2026-01-01T00:00:00Z", "health": "HEALTH_WARN",
					"stale": false, "last_seen_at": "2026-02-01T00:00:00Z",
					"reporter_count": 3, "fresh_reporter_count": 3, "unreachable_reporter_count": 1,
					"authoritative_host_id": "host-a",
					"created_at":            "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				},
				// An unpromoted cluster: health is null and MUST NOT read as
				// healthy. This is the whole reason the field is a pointer.
				map[string]interface{}{
					"fsid": "phantom", "name": "phantom", "configured_name": nil,
					"promoted": false, "promoted_at": nil, "health": nil,
					"stale": true, "last_seen_at": nil,
					"reporter_count": 1, "fresh_reporter_count": 0, "unreachable_reporter_count": 0,
					"authoritative_host_id": nil,
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &cephClustersDataSource{client: c}, map[string]tftypes.Value{
		"query":         tftypes.NewValue(tftypes.String, "prod"),
		"updated_since": tftypes.NewValue(tftypes.String, "2026-01-01T00:00:00Z"),
		"promoted":      tftypes.NewValue(tftypes.Bool, true),
		"stale":         tftypes.NewValue(tftypes.Bool, false),
		"order":         tftypes.NewValue(tftypes.String, "fsid"),
		"direction":     tftypes.NewValue(tftypes.String, "desc"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if gotPath != "/api/v1/ceph_clusters" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	// Every schema filter has to reach the API, including the query -> q rename
	// and the two booleans, whose `false` must be sent rather than dropped.
	for key, want := range map[string]string{
		"q": "prod", "updated_since": "2026-01-01T00:00:00Z",
		"promoted": "true", "stale": "false", "order": "fsid", "direction": "desc",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q, got %q", key, want, got)
		}
	}

	var out cephClustersModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.CephClusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(out.CephClusters))
	}

	first := out.CephClusters[0]
	if first.FSID.ValueString() != "8e4a-prod" || first.Health.ValueString() != "HEALTH_WARN" {
		t.Errorf("unexpected first cluster: %+v", first)
	}
	if first.UnreachableReporterCount.ValueInt64() != 1 {
		t.Errorf("expected unreachable_reporter_count 1, got %d", first.UnreachableReporterCount.ValueInt64())
	}

	// An unpromoted cluster publishes no verdict. A null here must stay null:
	// collapsing it to "" would hand automation a green light for a cluster the
	// product itself refuses to vouch for.
	second := out.CephClusters[1]
	if !second.Health.IsNull() {
		t.Errorf("expected a null health for an unpromoted cluster, got %v", second.Health)
	}
	if second.Promoted.ValueBool() {
		t.Error("expected promoted=false")
	}
	for name, v := range map[string]types.String{
		"configured_name":       second.ConfiguredName,
		"promoted_at":           second.PromotedAt,
		"last_seen_at":          second.LastSeenAt,
		"authoritative_host_id": second.AuthoritativeHostID,
		"created_at":            second.CreatedAt,
	} {
		if !v.IsNull() {
			t.Errorf("expected %s to be null, got %v", name, v)
		}
	}
}

func TestCephClustersDataSource_Read_NoFiltersAndNoResults(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ceph_clusters": []interface{}{},
			"meta":          map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &cephClustersDataSource{client: c}, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// An unset filter must stay out of the query entirely: the API 400s on an
	// unknown or empty parameter rather than ignoring it.
	for _, key := range []string{"q", "updated_since", "promoted", "stale", "order", "direction"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %s to be omitted when unset", key)
		}
	}

	// Zero matches must serialise as [] and not null: length()/for_each/toset
	// over a null list fail.
	var clusters types.List
	if diags := state.GetAttribute(context.Background(), path.Root("ceph_clusters"), &clusters); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if clusters.IsNull() {
		t.Error("expected an empty list, got null")
	}
}

func TestCephClusterDataSource_Read(t *testing.T) {
	var gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ceph_cluster": map[string]interface{}{
				"fsid": "8e4a-prod", "name": "prod-ceph", "promoted": true, "health": "HEALTH_OK",
				"stale": false, "reporter_count": 2, "fresh_reporter_count": 2,
				"unreachable_reporter_count": 0, "authoritative_host_id": "host-a",
				"reporters": []interface{}{
					map[string]interface{}{
						"host_id": "host-a", "host_name": "mon-a", "fresh": true, "authoritative": true,
						"reachable": true, "status_ok": true, "df_ok": true, "tree_ok": true,
						"osd_df_ok": true, "perf_ok": true,
						"completeness_score": 63, "max_completeness_score": 63,
						"last_health": "HEALTH_OK", "last_error": nil,
						"last_synced_at": "2026-02-01T00:00:00Z", "received_at": "2026-02-01T00:00:01Z",
					},
					// A silent host keeps its last reading forever. `fresh`
					// false is the only thing that says so.
					map[string]interface{}{
						"host_id": "host-b", "host_name": nil, "fresh": false, "authoritative": false,
						"reachable": false, "status_ok": false, "df_ok": false, "tree_ok": false,
						"osd_df_ok": false, "perf_ok": false,
						"completeness_score": 0, "max_completeness_score": 63,
						"last_health": "HEALTH_OK", "last_error": "connection refused",
						"last_synced_at": nil, "received_at": nil,
					},
				},
			},
		})
	})

	state, resp := readDataSource(t, &cephClusterDataSource{client: c}, map[string]tftypes.Value{
		"fsid": tftypes.NewValue(tftypes.String, "8e4a-prod"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// The path segment is the fsid, not a row id.
	if gotPath != "/api/v1/ceph_clusters/8e4a-prod" {
		t.Errorf("unexpected path: %s", gotPath)
	}

	var out cephClusterDetailModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.Reporters) != 2 {
		t.Fatalf("expected 2 reporters, got %d", len(out.Reporters))
	}
	if !out.Reporters[0].Fresh.ValueBool() || !out.Reporters[0].Authoritative.ValueBool() {
		t.Errorf("expected the first reporter to be fresh and authoritative: %+v", out.Reporters[0])
	}
	stale := out.Reporters[1]
	if stale.Fresh.ValueBool() {
		t.Error("expected the second reporter to be stale")
	}
	// The stale host's borrowed verdict is still published — it is provenance,
	// and `fresh` is what qualifies it.
	if stale.LastHealth.ValueString() != "HEALTH_OK" {
		t.Errorf("expected the stale reporter to keep its last_health, got %v", stale.LastHealth)
	}
	if !stale.HostName.IsNull() || !stale.LastSyncedAt.IsNull() {
		t.Errorf("expected null host_name and last_synced_at, got %+v", stale)
	}
}

func TestCephClusterDataSource_Read_Error(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not Found"})
	})

	state, resp := readDataSource(t, &cephClusterDataSource{client: c}, map[string]tftypes.Value{
		"fsid": tftypes.NewValue(tftypes.String, "missing"),
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic for a 404")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); got != "Error reading Ceph cluster" {
		t.Errorf("unexpected summary: %q", got)
	}
	if !state.Raw.IsNull() {
		t.Errorf("expected state to be left null, got %v", state.Raw)
	}
}

// --- proxmox_clusters ---

func TestProxmoxClustersDataSource_Read(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_clusters": []interface{}{
				map[string]interface{}{
					"id": "cluster-uuid", "cluster_key": "pve-prod", "name": "pve-prod", "version": "8.1",
					"standalone": false, "quorate": true, "stale": false,
					"last_seen_at": "2026-02-01T00:00:00Z", "reporter_count": 3,
					"fresh_reporter_count": 3, "unreachable_reporter_count": 0,
					"authoritative_host_id": "host-a",
					"nodes_total":           3, "nodes_online": 2, "guests_total": 42, "guests_running": 40,
					"storage_total": 6, "storage_active": 6,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				},
				// A stale cluster: quorate is null, meaning UNKNOWN and never
				// "lost". A consumer that read it as false would page on a
				// monitoring outage.
				map[string]interface{}{
					"id": "standalone-uuid", "cluster_key": "standalone:host-z", "name": "lab",
					"version": nil, "standalone": true, "quorate": nil, "stale": true,
					"last_seen_at": nil, "reporter_count": 1, "fresh_reporter_count": 0,
					"unreachable_reporter_count": 0, "authoritative_host_id": nil,
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &proxmoxClustersDataSource{client: c}, map[string]tftypes.Value{
		"query":      tftypes.NewValue(tftypes.String, "pve"),
		"standalone": tftypes.NewValue(tftypes.Bool, false),
		"stale":      tftypes.NewValue(tftypes.Bool, false),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	for key, want := range map[string]string{"q": "pve", "standalone": "false", "stale": "false"} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q, got %q", key, want, got)
		}
	}

	var out proxmoxClustersModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.ProxmoxClusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(out.ProxmoxClusters))
	}

	first := out.ProxmoxClusters[0]
	if !first.Quorate.ValueBool() {
		t.Error("expected quorate=true")
	}
	if first.NodesTotal.ValueInt64() != 3 || first.NodesOnline.ValueInt64() != 2 {
		t.Errorf("unexpected node rollups: %+v", first)
	}

	// THE NULL IS LOAD-BEARING: unknown, not "lost".
	second := out.ProxmoxClusters[1]
	if !second.Quorate.IsNull() {
		t.Errorf("expected a null quorate for a stale standalone cluster, got %v", second.Quorate)
	}
	if !second.Standalone.ValueBool() {
		t.Error("expected standalone=true")
	}
	// A rollup the response omitted must read null, never 0: 0 would say "this
	// cluster has no nodes".
	for name, v := range map[string]types.Int64{
		"nodes_total": second.NodesTotal, "nodes_online": second.NodesOnline,
		"guests_total": second.GuestsTotal, "guests_running": second.GuestsRunning,
		"storage_total": second.StorageTotal, "storage_active": second.StorageActive,
	} {
		if !v.IsNull() {
			t.Errorf("expected %s to be null when the rollup is omitted, got %v", name, v)
		}
	}
}

func TestProxmoxClusterDataSource_Read(t *testing.T) {
	var gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_cluster": map[string]interface{}{
				"id": "cluster-uuid", "cluster_key": "pve-prod", "name": "pve-prod",
				"standalone": false, "quorate": true, "stale": false,
				"reporter_count": 2, "fresh_reporter_count": 2, "unreachable_reporter_count": 1,
				"nodes_total": 3, "nodes_online": 3,
				"reporters": []interface{}{
					map[string]interface{}{
						"host_id": "host-a", "host_name": "pve1", "fresh": true, "authoritative": true,
						"reachable": true, "cluster_ok": true, "nodes_ok": true, "guests_ok": true,
						"storage_ok": true, "completeness_score": 31, "max_completeness_score": 31,
						"quorate_seen": true, "nodes_online_seen": 3, "nodes_total_seen": 3,
						"last_error": nil, "last_synced_at": "2026-02-01T00:00:00Z",
						"received_at": "2026-02-01T00:00:01Z",
					},
					// The minority partition during a split brain: this host
					// really does see no quorum while the cluster is quorate.
					map[string]interface{}{
						"host_id": "host-b", "host_name": "pve2", "fresh": true, "authoritative": false,
						"reachable": true, "cluster_ok": true, "nodes_ok": false, "guests_ok": false,
						"storage_ok": false, "completeness_score": 7, "max_completeness_score": 31,
						"quorate_seen": false, "nodes_online_seen": nil, "nodes_total_seen": nil,
						"last_error": "cluster not ready", "last_synced_at": nil, "received_at": nil,
					},
				},
			},
		})
	})

	state, resp := readDataSource(t, &proxmoxClusterDataSource{client: c}, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "cluster-uuid"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if gotPath != "/api/v1/proxmox_clusters/cluster-uuid" {
		t.Errorf("unexpected path: %s", gotPath)
	}

	var out proxmoxClusterDetailModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if !out.Quorate.ValueBool() {
		t.Error("expected the cluster verdict to stay quorate")
	}
	if len(out.Reporters) != 2 {
		t.Fatalf("expected 2 reporters, got %d", len(out.Reporters))
	}
	// The per-host reading is provenance, not a second verdict: a fresh
	// reporter reporting false does not flip the cluster.
	minority := out.Reporters[1]
	if minority.QuorateSeen.IsNull() || minority.QuorateSeen.ValueBool() {
		t.Errorf("expected quorate_seen=false on the minority reporter, got %v", minority.QuorateSeen)
	}
	if !minority.NodesOnlineSeen.IsNull() || !minority.NodesTotalSeen.IsNull() {
		t.Errorf("expected null seen-counts when the host could not read them: %+v", minority)
	}
	if minority.LastError.ValueString() != "cluster not ready" {
		t.Errorf("unexpected last_error: %v", minority.LastError)
	}
}

// --- the Proxmox child inventories ---

// proxmoxInventoryByName finds a declared inventory so a test can drive it.
func proxmoxInventoryByName(t *testing.T, name string) proxmoxInventory {
	t.Helper()
	for _, p := range proxmoxInventories {
		if p.name == name {
			return p
		}
	}
	t.Fatalf("no proxmox inventory named %q", name)
	return proxmoxInventory{}
}

func TestProxmoxInventoryDataSources_Registered(t *testing.T) {
	constructors := ProxmoxInventoryDataSources()
	if len(constructors) != len(proxmoxInventories) {
		t.Fatalf("expected %d constructors, got %d", len(proxmoxInventories), len(constructors))
	}
	// Each constructor must close over its OWN table entry. A loop-variable
	// capture bug here would register four copies of the last one.
	seen := map[string]bool{}
	for _, ctor := range constructors {
		resp := &datasource.MetadataResponse{}
		ctor().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "fivenines"}, resp)
		if seen[resp.TypeName] {
			t.Errorf("duplicate data source %q", resp.TypeName)
		}
		seen[resp.TypeName] = true
	}
	for _, want := range []string{
		"fivenines_proxmox_cluster_nodes",
		"fivenines_proxmox_cluster_guests",
		"fivenines_proxmox_cluster_storages",
		"fivenines_organization_proxmox_guests",
	} {
		if !seen[want] {
			t.Errorf("%s is not registered", want)
		}
	}
}

func TestProxmoxClusterInventory_Read(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_nodes": []interface{}{
				map[string]interface{}{
					"id": "node-1", "proxmox_cluster_id": "cluster-uuid", "name": "pve1",
					"status": "online", "last_synced_at": "2026-02-01T00:00:00Z", "stale": false,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-02-01T00:00:00Z",
				},
				map[string]interface{}{
					"id": "node-2", "proxmox_cluster_id": "cluster-uuid", "name": "pve2",
					"status": "offline", "last_synced_at": nil, "stale": true,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-02-01T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	d := &proxmoxInventoryDataSource{inventory: proxmoxInventoryByName(t, "proxmox_cluster_nodes"), client: c}
	state, resp := readDataSource(t, d, map[string]tftypes.Value{
		"cluster_id": tftypes.NewValue(tftypes.String, "cluster-uuid"),
		"status":     tftypes.NewValue(tftypes.String, "offline"),
		"stale":      tftypes.NewValue(tftypes.Bool, false),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// The route segment is `nodes` while the envelope key is `proxmox_nodes`.
	if gotPath != "/api/v1/proxmox_clusters/cluster-uuid/nodes" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if got := gotQuery.Get("status"); got != "offline" {
		t.Errorf("expected status=offline, got %q", got)
	}
	// A false boolean filter must be SENT, not dropped: it is the half of the
	// partition that returns the currently-reporting rows.
	if got := gotQuery.Get("stale"); got != "false" {
		t.Errorf("expected stale=false to be sent, got %q", got)
	}
	if _, ok := gotQuery["q"]; ok {
		t.Error("expected an unset filter to be omitted")
	}

	var rows types.List
	if diags := state.GetAttribute(context.Background(), path.Root("proxmox_cluster_nodes"), &rows); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(rows.Elements()) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows.Elements()))
	}

	// The configured cluster_id and filters round-trip into state.
	var clusterID types.String
	if diags := state.GetAttribute(context.Background(), path.Root("cluster_id"), &clusterID); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if clusterID.ValueString() != "cluster-uuid" {
		t.Errorf("expected cluster_id to be echoed, got %q", clusterID.ValueString())
	}
}

func TestOrganizationProxmoxGuests_Read(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_guests": []interface{}{
				map[string]interface{}{
					"id": "guest-1", "proxmox_cluster_id": "cluster-a", "proxmox_node_id": "node-1",
					"node_name": "pve1", "vmid": "1042", "name": "web-01", "guest_type": "vm",
					"status": "running", "state_changed_at": "2026-02-01T00:00:00Z",
					"last_synced_at": "2026-02-01T00:00:00Z", "stale": false,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-02-01T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	d := &proxmoxInventoryDataSource{inventory: proxmoxInventoryByName(t, "organization_proxmox_guests"), client: c}
	state, resp := readDataSource(t, d, map[string]tftypes.Value{
		"proxmox_cluster_id": tftypes.NewValue(tftypes.String, "cluster-a"),
		"guest_type":         tftypes.NewValue(tftypes.String, "vm"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// The fleet-wide route is unscoped — no instance and no cluster in the path.
	if gotPath != "/api/v1/proxmox_guests" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	for key, want := range map[string]string{"proxmox_cluster_id": "cluster-a", "guest_type": "vm"} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q, got %q", key, want, got)
		}
	}

	// The unscoped data source must not grow a cluster_id ARGUMENT; the cluster
	// is a filter here, not the thing addressed.
	s := dataSourceSchema(t, d)
	if _, ok := s.Attributes["cluster_id"]; ok {
		t.Error("the organization-wide guest list must not take a cluster_id argument")
	}

	var rows types.List
	if diags := state.GetAttribute(context.Background(), path.Root("organization_proxmox_guests"), &rows); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(rows.Elements()) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows.Elements()))
	}
}

func TestProxmoxClusterInventory_Read_Error(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not Found"})
	})

	d := &proxmoxInventoryDataSource{inventory: proxmoxInventoryByName(t, "proxmox_cluster_guests"), client: c}
	state, resp := readDataSource(t, d, map[string]tftypes.Value{
		"cluster_id": tftypes.NewValue(tftypes.String, "missing"),
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic for a 404")
	}
	if !state.Raw.IsNull() {
		t.Errorf("expected state to be left null, got %v", state.Raw)
	}
}

// proxmoxRowFields is the single source of the row shapes, so a typo in the
// table is a panic at init rather than a data source documenting the wrong
// column. Both guards are exercised here because neither can fire in normal use.
func TestProxmoxRowFields_Guards(t *testing.T) {
	for _, tt := range []struct {
		name      string
		collector string
		overrides map[string]string
	}{
		{name: "unknown collector", collector: "not_a_collector"},
		{name: "override for a field that is not there", collector: "proxmox_nodes",
			overrides: map[string]string{"no_such_field": "x"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic")
				}
			}()
			proxmoxRowFields(tt.collector, tt.overrides)
		})
	}

	// The override must actually replace the description, and must not leak
	// into the per-instance collector table it was read from.
	fields := proxmoxRowFields("proxmox_nodes", map[string]string{"proxmox_cluster_id": "overridden"})
	var got string
	for _, f := range fields {
		if f.name == "proxmox_cluster_id" {
			got = f.desc
		}
	}
	if got != "overridden" {
		t.Errorf("expected the override to apply, got %q", got)
	}
	for _, c := range inventoryCollectors {
		if c.name != "proxmox_nodes" {
			continue
		}
		for _, f := range c.fields {
			if f.name == "proxmox_cluster_id" && f.desc == "overridden" {
				t.Error("the override leaked into the shared collector table")
			}
		}
	}
}

// Configure must STORE the client, not merely accept it. Dropping the
// assignment (`configureClient(req, resp)` as a bare statement) still compiles,
// still satisfies the DataSourceWithConfigure assertions above, and still
// produces no diagnostic -- and every Read test injects `client: c` directly, so
// nothing else would catch the nil until an apply panicked.
func TestClusterDataSources_ConfigureStoresTheClient(t *testing.T) {
	c := client.NewClient("https://example.com", "key")

	cephIndex := &cephClustersDataSource{}
	cephOne := &cephClusterDataSource{}
	proxIndex := &proxmoxClustersDataSource{}
	proxOne := &proxmoxClusterDataSource{}
	inv := &proxmoxInventoryDataSource{}
	caps := &instanceCapabilityStatusDataSource{}
	subs := &statusPageSubscribersDataSource{}

	for _, ds := range []datasource.DataSourceWithConfigure{
		cephIndex, cephOne, proxIndex, proxOne, inv, caps, subs,
	} {
		resp := &datasource.ConfigureResponse{}
		ds.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: c}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%T: configure failed: %v", ds, resp.Diagnostics.Errors())
		}
	}

	for name, got := range map[string]*client.Client{
		"ceph_clusters": cephIndex.client, "ceph_cluster": cephOne.client,
		"proxmox_clusters": proxIndex.client, "proxmox_cluster": proxOne.client,
		"proxmox inventory": inv.client, "capability_status": caps.client,
		"status_page_subscribers": subs.client,
	} {
		if got != c {
			t.Errorf("%s: expected the configured client to be stored, got %v", name, got)
		}
	}
}

// The Proxmox inventory table gets the guard the twenty-collector table has.
// Schema writes the filters into the attribute map LAST, after the rows
// attribute and cluster_id, so a filter sharing either name silently replaces
// it -- and `cluster_id` is a plausible future filter name, given the org-wide
// list already carries `proxmox_cluster_id`. Read would then resolve
// path.Root("cluster_id") to null and request /proxmox_clusters//nodes.
func TestProxmoxInventories_TableIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range proxmoxInventories {
		if seen[p.name] {
			t.Errorf("duplicate proxmox inventory %q", p.name)
		}
		seen[p.name] = true
		if p.desc == "" || p.key == "" || len(p.fields) == 0 {
			t.Errorf("%s: incomplete declaration", p.name)
		}

		reserved := map[string]bool{p.name: true}
		if p.clusterScoped() {
			reserved["cluster_id"] = true
		}
		filters := map[string]bool{}
		for _, f := range p.filters {
			if reserved[f.name] || filters[f.name] {
				t.Errorf("%s: filter %q collides with another top-level attribute", p.name, f.name)
			}
			filters[f.name] = true
			if f.kind != fieldString && f.kind != fieldBool {
				t.Errorf("%s: filter %q must be a string or a bool", p.name, f.name)
			}
			if f.desc == "" {
				t.Errorf("%s.%s: no description", p.name, f.name)
			}
		}

		// The table alone cannot show a collision — count what the schema
		// actually ends up with.
		want := len(p.filters) + 1
		if p.clusterScoped() {
			want++
		}
		s := dataSourceSchema(t, &proxmoxInventoryDataSource{inventory: p})
		if len(s.Attributes) != want {
			t.Errorf("%s: expected %d top-level attributes, got %d", p.name, want, len(s.Attributes))
		}
	}
}

// The show routes must echo the CONFIGURED identifier rather than whatever the
// response carries. `fsid` and `id` are Required arguments, and Terraform fails
// an apply with "Provider produced inconsistent result" when a configured value
// comes back changed. The API looks both up by exact match today, so this can
// only bite after a server-side change -- which is exactly when a test is the
// thing that catches it.
func TestClusterDetailDataSources_EchoTheConfiguredIdentifier(t *testing.T) {
	for _, tt := range []struct {
		name  string
		key   string
		body  map[string]interface{}
		build func(c *client.Client) datasource.DataSource
		arg   string
		want  string
	}{
		{
			name: "ceph keeps the configured fsid when the response canonicalises it",
			key:  "ceph_cluster",
			body: map[string]interface{}{
				"fsid": "8e4a-prod", "name": "prod-ceph", "promoted": true, "stale": false,
				"reporter_count": 1, "fresh_reporter_count": 1, "unreachable_reporter_count": 0,
			},
			build: func(c *client.Client) datasource.DataSource { return &cephClusterDataSource{client: c} },
			arg:   "fsid",
			want:  "8E4A-PROD",
		},
		{
			name: "proxmox keeps the configured id when the response omits it",
			key:  "proxmox_cluster",
			body: map[string]interface{}{
				"cluster_key": "pve-prod", "name": "pve-prod", "standalone": false, "stale": false,
				"reporter_count": 1, "fresh_reporter_count": 1, "unreachable_reporter_count": 0,
			},
			build: func(c *client.Client) datasource.DataSource { return &proxmoxClusterDataSource{client: c} },
			arg:   "id",
			want:  "cluster-uuid",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{tt.key: tt.body})
			})

			state, resp := readDataSource(t, tt.build(c), map[string]tftypes.Value{
				tt.arg: tftypes.NewValue(tftypes.String, tt.want),
			})
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
			}
			var got types.String
			if diags := state.GetAttribute(context.Background(), path.Root(tt.arg), &got); diags.HasError() {
				t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
			}
			if got.ValueString() != tt.want {
				t.Errorf("expected the configured %s %q to survive, got %q", tt.arg, tt.want, got.ValueString())
			}
		})
	}
}
