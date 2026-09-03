package provider_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The organization-wide guest list is the only one of the four Proxmox child
// inventories with a different route shape -- no cluster_id ARGUMENT, the cluster
// demoted to a filter -- and it is assembled by hand from the schema type rather
// than reflected out of a struct. Nothing had driven it through real Terraform,
// where a state object missing one attribute is a plan error the unit tests
// structurally cannot see.
func TestOrganizationProxmoxGuestsPlan_FiltersRoundTripAndRowsMap(t *testing.T) {
	var gotQuery map[string][]string
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/proxmox_guests" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_guests": []map[string]interface{}{{
				"id": "guest-1", "proxmox_cluster_id": "3cac0e44-0000-4000-8000-000000000001",
				"proxmox_node_id": "node-1", "node_name": "pve1", "vmid": "1042",
				"name": "web-01", "guest_type": "vm", "status": "stopped",
				"state_changed_at": "2026-02-01T04:10:00Z",
				"last_synced_at":   "2026-02-01T05:00:00Z", "stale": false,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-02-01T05:00:00Z",
			}},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_organization_proxmox_guests" "test" {
  proxmox_cluster_id = "3cac0e44-0000-4000-8000-000000000001"
  guest_type         = "vm"
  stale              = false
}

output "stopped_since" {
  value = join(",", [
    for g in data.fivenines_organization_proxmox_guests.test.organization_proxmox_guests :
    g.state_changed_at if g.status == "stopped"
  ])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				// Every configured filter is echoed back into state; one silently
				// dropped re-plans forever.
				resource.TestCheckResourceAttr("data.fivenines_organization_proxmox_guests.test",
					"proxmox_cluster_id", "3cac0e44-0000-4000-8000-000000000001"),
				resource.TestCheckResourceAttr("data.fivenines_organization_proxmox_guests.test", "guest_type", "vm"),
				resource.TestCheckResourceAttr("data.fivenines_organization_proxmox_guests.test", "stale", "false"),
				resource.TestCheckResourceAttr("data.fivenines_organization_proxmox_guests.test",
					"organization_proxmox_guests.0.vmid", "1042"),
				resource.TestCheckResourceAttr("data.fivenines_organization_proxmox_guests.test",
					"organization_proxmox_guests.0.node_name", "pve1"),
				// state_changed_at is what turns "it is stopped" into "it has
				// been stopped since 04:10".
				resource.TestCheckOutput("stopped_since", "2026-02-01T04:10:00Z"),
			),
		}},
	})

	// `stale = false` is half of a partition, not an absence, so it must reach
	// the wire rather than being dropped as a zero value.
	if got := gotQuery["stale"]; len(got) != 1 || got[0] != "false" {
		t.Errorf("expected stale=false to be sent, got %v", gotQuery["stale"])
	}
	if got := gotQuery["proxmox_cluster_id"]; len(got) != 1 {
		t.Errorf("expected the cluster filter to be sent, got %v", got)
	}
}

// The guest vocabularies live in their own filter table, separate from the node
// one the existing plan test covers. A missing OneOf there is a 400 mid-apply
// instead of a plan error, and only a real plan can tell the difference.
func TestOrganizationProxmoxGuestsPlan_RejectsInvalidGuestTypeAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_organization_proxmox_guests" "test" {
  guest_type = "container"
}`,
			// The vocabulary is vm/lxc; "container" is the word an operator
			// reaches for and is not one of them.
			ExpectError: regexp.MustCompile(`(?s)Attribute guest_type value must be one of.*"lxc"`),
		}},
	})
}
