package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// An index that failed must leave state null and say so. The failure mode these
// guard against is the one the whole #25 surface is built to avoid: a read that
// swallowed an API error and planned as "there is nothing here" would let a
// for_each over the clusters quietly destroy everything downstream.
func TestClusterDataSources_Read_ErrorsAreDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name        string
		build       func(c *client.Client) datasource.DataSource
		config      map[string]tftypes.Value
		wantSummary string
	}{
		{
			name:        "the ceph cluster index",
			build:       func(c *client.Client) datasource.DataSource { return &cephClustersDataSource{client: c} },
			wantSummary: "Error listing Ceph clusters",
		},
		{
			name:        "the proxmox cluster index",
			build:       func(c *client.Client) datasource.DataSource { return &proxmoxClustersDataSource{client: c} },
			wantSummary: "Error listing Proxmox clusters",
		},
		{
			name:        "a single proxmox cluster",
			build:       func(c *client.Client) datasource.DataSource { return &proxmoxClusterDataSource{client: c} },
			config:      map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "missing")},
			wantSummary: "Error reading Proxmox cluster",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{"error": "Internal Server Error"})
			})

			state, resp := readDataSource(t, tt.build(c), tt.config)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic, not an empty result")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != tt.wantSummary {
				t.Errorf("expected summary %q, got %q", tt.wantSummary, got)
			}
			if !state.Raw.IsNull() {
				t.Errorf("expected state to be left null, got %v", state.Raw)
			}
		})
	}
}

// A 403 gets the permission-specific diagnostic; everything else must NOT, or an
// outage would be reported to the operator as a token-scope problem.
func TestStatusPageSubscribersDataSource_Read_NonForbiddenError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Internal Server Error"})
	})

	state, resp := readDataSource(t, &statusPageSubscribersDataSource{client: c}, map[string]tftypes.Value{
		"status_page_id": tftypes.NewValue(tftypes.Number, 7),
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); got != "Error listing status page subscribers" {
		t.Errorf("expected the generic diagnostic for a non-403, got %q", got)
	}
	if !state.Raw.IsNull() {
		t.Errorf("expected state to be left null, got %v", state.Raw)
	}
}

// The Proxmox index has the same empty-list contract the Ceph one is tested for:
// zero matches is [], never null.
func TestProxmoxClustersDataSource_Read_EmptyIsAnEmptyList(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_clusters": []interface{}{},
			"meta":             map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &proxmoxClustersDataSource{client: c}, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}

	var clusters types.List
	if diags := state.GetAttribute(context.Background(), path.Root("proxmox_clusters"), &clusters); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if clusters.IsNull() {
		t.Error("expected an empty list, got null")
	}
	if len(clusters.Elements()) != 0 {
		t.Errorf("expected no clusters, got %d", len(clusters.Elements()))
	}
}

// The show routes OMIT `reporters` rather than sending an empty array when a
// cluster has none, and a null nested list is the shape that breaks a for_each
// over the reporting hosts.
func TestClusterDataSources_Read_MissingReportersAreAnEmptyList(t *testing.T) {
	for _, tt := range []struct {
		name   string
		key    string
		body   map[string]interface{}
		build  func(c *client.Client) datasource.DataSource
		config map[string]tftypes.Value
	}{
		{
			name: "ceph",
			key:  "ceph_cluster",
			body: map[string]interface{}{
				"fsid": "8e4a-prod", "name": "prod-ceph", "promoted": false, "health": nil,
				"stale": true, "reporter_count": 0, "fresh_reporter_count": 0,
				"unreachable_reporter_count": 0,
			},
			build:  func(c *client.Client) datasource.DataSource { return &cephClusterDataSource{client: c} },
			config: map[string]tftypes.Value{"fsid": tftypes.NewValue(tftypes.String, "8e4a-prod")},
		},
		{
			name: "proxmox",
			key:  "proxmox_cluster",
			body: map[string]interface{}{
				"id": "cluster-uuid", "cluster_key": "pve-prod", "name": "pve-prod",
				"standalone": true, "quorate": nil, "stale": true,
				"reporter_count": 0, "fresh_reporter_count": 0, "unreachable_reporter_count": 0,
			},
			build:  func(c *client.Client) datasource.DataSource { return &proxmoxClusterDataSource{client: c} },
			config: map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "cluster-uuid")},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{tt.key: tt.body})
			})

			state, resp := readDataSource(t, tt.build(c), tt.config)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
			}

			var reporters types.List
			if diags := state.GetAttribute(context.Background(), path.Root("reporters"), &reporters); diags.HasError() {
				t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
			}
			if reporters.IsNull() {
				t.Fatal("expected an empty reporters list, got null")
			}
			if len(reporters.Elements()) != 0 {
				t.Errorf("expected no reporters, got %d", len(reporters.Elements()))
			}

			// A cluster nobody reports still publishes its "we are not
			// watching" verdict rather than a healthy-looking blank.
			var stale types.Bool
			if diags := state.GetAttribute(context.Background(), path.Root("stale"), &stale); diags.HasError() {
				t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
			}
			if !stale.ValueBool() {
				t.Error("expected stale=true")
			}
		})
	}
}
