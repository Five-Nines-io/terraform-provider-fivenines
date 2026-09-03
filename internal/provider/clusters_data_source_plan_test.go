package provider_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The cluster and status data sources added for #25 lean on plan-time behaviour
// the unit tests structurally cannot see: the OneOf validators on the enumerated
// filters, and the maps and lists that must plan as empty rather than null. The
// unit tests drive Read directly, which skips config validation entirely, so a
// validator can be declared and never wired and the whole datasources suite
// stays green.

func clusterPlanHandler(t *testing.T) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/ceph_clusters"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ceph_clusters": []map[string]interface{}{{
					"fsid": "8e4a-prod", "name": "prod-ceph", "configured_name": "prod-ceph",
					"promoted": true, "health": "HEALTH_OK", "stale": false,
					"reporter_count": 3, "fresh_reporter_count": 3, "unreachable_reporter_count": 0,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				}},
				"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/proxmox_clusters"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"proxmox_clusters": []map[string]interface{}{{
					"id": "3cac0e44-0000-4000-8000-000000000001", "cluster_key": "pve-prod",
					"name": "pve-prod", "standalone": false, "quorate": true, "stale": false,
					"reporter_count": 2, "fresh_reporter_count": 2, "unreachable_reporter_count": 0,
					"nodes_total": 3, "nodes_online": 3, "guests_total": 42, "guests_running": 40,
					"storage_total": 6, "storage_active": 6,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				}},
				"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
			})
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	}
}

func TestClusterDataSourcesPlan_FiltersRoundTripAndFieldsMap(t *testing.T) {
	planTest(t, clusterPlanHandler(t))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_ceph_clusters" "test" {
  query     = "prod"
  promoted  = true
  stale     = false
  order     = "fsid"
  direction = "desc"
}

data "fivenines_proxmox_clusters" "test" {
  query      = "pve"
  standalone = false
  order      = "cluster_key"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				// The filters have to survive the round trip into state: an
				// argument silently dropped re-plans forever.
				resource.TestCheckResourceAttr("data.fivenines_ceph_clusters.test", "query", "prod"),
				resource.TestCheckResourceAttr("data.fivenines_ceph_clusters.test", "promoted", "true"),
				resource.TestCheckResourceAttr("data.fivenines_ceph_clusters.test", "stale", "false"),
				resource.TestCheckResourceAttr("data.fivenines_ceph_clusters.test", "order", "fsid"),
				resource.TestCheckResourceAttr("data.fivenines_ceph_clusters.test", "ceph_clusters.0.fsid", "8e4a-prod"),
				resource.TestCheckResourceAttr("data.fivenines_ceph_clusters.test", "ceph_clusters.0.health", "HEALTH_OK"),
				resource.TestCheckResourceAttr("data.fivenines_proxmox_clusters.test", "standalone", "false"),
				resource.TestCheckResourceAttr("data.fivenines_proxmox_clusters.test", "proxmox_clusters.0.quorate", "true"),
				resource.TestCheckResourceAttr("data.fivenines_proxmox_clusters.test", "proxmox_clusters.0.nodes_online", "3"),
				// Two identifiers, not interchangeable -- both have to be there.
				resource.TestCheckResourceAttr("data.fivenines_proxmox_clusters.test", "proxmox_clusters.0.cluster_key", "pve-prod"),
				resource.TestCheckResourceAttr("data.fivenines_proxmox_clusters.test", "proxmox_clusters.0.id", "3cac0e44-0000-4000-8000-000000000001"),
			),
		}},
	})
}

func TestClusterDataSourcesPlan_RejectsInvalidOrderAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_ceph_clusters" "test" {
  order = "health"
}`,
			// `health` is a fold over the reporter set with no SQL twin, so it
			// is deliberately neither sortable nor filterable. A config that
			// assumes otherwise must fail at plan time, not 400 mid-apply.
			ExpectError: regexp.MustCompile(`(?s)Attribute order value must be one of.*"configured_name"`),
		}},
	})
}

func TestProxmoxClusters_RejectsInvalidOrderAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_proxmox_clusters" "test" {
  order = "quorate"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute order value must be one of.*"cluster_key"`),
		}},
	})
}

func TestProxmoxClusterInventoryPlan_RejectsInvalidStatusAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_proxmox_cluster_nodes" "test" {
  cluster_id = "3cac0e44-0000-4000-8000-000000000001"
  status     = "down"
}`,
			// The node vocabulary is unknown/offline/online; "down" is the
			// word an operator reaches for and is not one of them.
			ExpectError: regexp.MustCompile(`(?s)Attribute status value must be one of.*"offline"`),
		}},
	})
}

func TestStatusPageSubscribersPlan_RejectsInvalidStatusAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_status_page_subscribers" "test" {
  status_page_id = 7
  status         = "unconfirmed"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute status value must be one of.*"pending"`),
		}},
	})
}

// The capability status maps and list must plan as EMPTY, not null. A host that
// has never checked in returns nulls for all three, and a null map fails
// lookup() and for_each at plan time -- which is precisely the config an
// operator writes to branch on a capability.
func TestInstanceCapabilityStatusPlan_EmptyCollectionsStillPlan(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"capability_status": map[string]interface{}{
				"capabilities": nil, "pending": nil, "reasons": nil, "updated_at": nil,
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_instance_capability_status" "test" {
  instance_id = "3cac0e44-0000-4000-8000-000000000001"
}

output "docker_ready" {
  value = lookup(data.fivenines_instance_capability_status.test.capabilities, "docker", false)
}

output "pending_count" {
  value = length(data.fivenines_instance_capability_status.test.pending)
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_instance_capability_status.test", "capabilities.%", "0"),
				resource.TestCheckResourceAttr("data.fivenines_instance_capability_status.test", "pending.#", "0"),
				// `reported` is what separates "not reported" from "nothing is
				// supported"; an empty map alone cannot say which.
				resource.TestCheckResourceAttr("data.fivenines_instance_capability_status.test", "reported", "false"),
				resource.TestCheckOutput("docker_ready", "false"),
				resource.TestCheckOutput("pending_count", "0"),
			),
		}},
	})
}

// A filtered read that matches nothing has to plan as an empty list rather than
// null, for the same reason: the `for` expression below is the shape
// practitioners write.
func TestClusterDataSourcesPlan_EmptyResultStillPlans(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ceph_clusters": []map[string]interface{}{},
			"meta":          map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_ceph_clusters" "none" {
  query = "nothing-matches-this"
}

output "unhealthy" {
  value = join(",", [
    for c in data.fivenines_ceph_clusters.none.ceph_clusters :
    c.fsid if c.health != "HEALTH_OK"
  ])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_ceph_clusters.none", "ceph_clusters.#", "0"),
				resource.TestCheckOutput("unhealthy", ""),
			),
		}},
	})
}

// The two SINGULAR data sources drive real Terraform through the embedded-model
// path: `cephClusterDetailModel` / `proxmoxClusterDetailModel` embed their list
// model rather than restating its fields, and Read overwrites the whole embedded
// value before restoring the Required identifier.
//
// This is the tier that proves the embedding works. An outside review flagged
// anonymous embedding as unsupported by the framework and predicted a
// model/schema conversion error making both data sources unusable; the unit
// tests could not settle it because they never leave the process. These do: a
// real `terraform plan` performs the full Config.Get / State.Set round trip over
// gRPC, so a broken promotion surfaces as a plan error rather than a silent
// wrong value. They also pin the identifier-restore, which is a plan-vs-config
// consistency rule the unit tier cannot see.
func TestClusterDetailDataSourcesPlan_EmbeddedModelRoundTrips(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/ceph_clusters/"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ceph_cluster": map[string]interface{}{
					"fsid": "8e4a-prod", "name": "prod-ceph", "configured_name": nil,
					"promoted": true, "health": "HEALTH_OK", "stale": false,
					"reporter_count": 2, "fresh_reporter_count": 2, "unreachable_reporter_count": 0,
					"authoritative_host_id": "host-a",
					"created_at":            "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
					"reporters": []map[string]interface{}{{
						"host_id": "host-a", "host_name": "mon-a", "fresh": true, "authoritative": true,
						"reachable": true, "status_ok": true, "df_ok": true, "tree_ok": true,
						"osd_df_ok": true, "perf_ok": true,
						"completeness_score": 63, "max_completeness_score": 63,
						"last_health": "HEALTH_OK", "last_error": nil,
						"last_synced_at": "2026-02-01T00:00:00Z", "received_at": "2026-02-01T00:00:01Z",
					}},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/proxmox_clusters/"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"proxmox_cluster": map[string]interface{}{
					"id": "3cac0e44-0000-4000-8000-000000000001", "cluster_key": "pve-prod",
					"name": "pve-prod", "version": nil, "standalone": false, "quorate": true,
					"stale": false, "reporter_count": 1, "fresh_reporter_count": 1,
					"unreachable_reporter_count": 0, "nodes_total": 3, "nodes_online": 3,
					"guests_total": 42, "guests_running": 40, "storage_total": 6, "storage_active": 6,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
					"reporters": []map[string]interface{}{{
						"host_id": "host-a", "host_name": "pve1", "fresh": true, "authoritative": true,
						"reachable": true, "cluster_ok": true, "nodes_ok": true, "guests_ok": true,
						"storage_ok": true, "completeness_score": 31, "max_completeness_score": 31,
						"quorate_seen": true, "nodes_online_seen": 3, "nodes_total_seen": 3,
						"last_error": nil, "last_synced_at": "2026-02-01T00:00:00Z",
						"received_at": "2026-02-01T00:00:01Z",
					}},
				},
			})
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_ceph_cluster" "one" {
  fsid = "8e4a-prod"
}

data "fivenines_proxmox_cluster" "one" {
  id = "3cac0e44-0000-4000-8000-000000000001"
}

output "ceph_reporter" {
  value = one(data.fivenines_ceph_cluster.one.reporters[*].host_name)
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				// Promoted fields from the embedded model must be readable.
				resource.TestCheckResourceAttr("data.fivenines_ceph_cluster.one", "health", "HEALTH_OK"),
				resource.TestCheckResourceAttr("data.fivenines_ceph_cluster.one", "name", "prod-ceph"),
				resource.TestCheckResourceAttr("data.fivenines_ceph_cluster.one", "reporter_count", "2"),
				// A promoted nullable field stays null rather than becoming "".
				resource.TestCheckNoResourceAttr("data.fivenines_ceph_cluster.one", "configured_name"),
				// The sibling (non-embedded) nested attribute survives too.
				resource.TestCheckResourceAttr("data.fivenines_ceph_cluster.one", "reporters.#", "1"),
				resource.TestCheckResourceAttr("data.fivenines_ceph_cluster.one", "reporters.0.authoritative", "true"),
				resource.TestCheckOutput("ceph_reporter", "mon-a"),
				// The Required identifier is echoed unchanged -- a changed value
				// is "Provider produced inconsistent result after apply".
				resource.TestCheckResourceAttr("data.fivenines_ceph_cluster.one", "fsid", "8e4a-prod"),

				resource.TestCheckResourceAttr("data.fivenines_proxmox_cluster.one", "cluster_key", "pve-prod"),
				resource.TestCheckResourceAttr("data.fivenines_proxmox_cluster.one", "quorate", "true"),
				resource.TestCheckResourceAttr("data.fivenines_proxmox_cluster.one", "nodes_online", "3"),
				resource.TestCheckNoResourceAttr("data.fivenines_proxmox_cluster.one", "version"),
				resource.TestCheckResourceAttr("data.fivenines_proxmox_cluster.one", "reporters.0.quorate_seen", "true"),
				resource.TestCheckResourceAttr("data.fivenines_proxmox_cluster.one", "id", "3cac0e44-0000-4000-8000-000000000001"),
			),
		}},
	})
}
