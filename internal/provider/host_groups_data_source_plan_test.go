package provider_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// A data source is only reachable through a real plan+apply: the unit tests
// drive Read directly, which skips schema validation of the config entirely. An
// unparseable filter value or a nested attribute Terraform cannot decode is
// invisible to them and fatal to every practitioner.
func hostGroupsDataSourceHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"host_groups": []map[string]interface{}{
			{
				"id": 7, "name": "Production", "position": 1,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		},
		"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
	})
}

// The point of the data source: a group id reaches a resource argument without
// anyone hardcoding an integer.
func TestHostGroupsDataSourcePlan_LooksUpIDByName(t *testing.T) {
	planTest(t, hostGroupsDataSourceHandler)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_host_groups" "test" {
  query = "prod"
}

output "group_id" {
  value = one([for g in data.fivenines_host_groups.test.host_groups : g.id])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_host_groups.test", "host_groups.#", "1"),
				resource.TestCheckResourceAttr("data.fivenines_host_groups.test", "host_groups.0.id", "7"),
				resource.TestCheckResourceAttr("data.fivenines_host_groups.test", "host_groups.0.name", "Production"),
				resource.TestCheckResourceAttr("data.fivenines_host_groups.test", "host_groups.0.position", "1"),
				// The filter round-trips instead of being dropped or re-planned.
				resource.TestCheckResourceAttr("data.fivenines_host_groups.test", "query", "prod"),
				resource.TestCheckOutput("group_id", "7"),
			),
		}},
	})
}

// With no filters the data source still has to produce a complete plan: every
// unset Optional attribute stays null rather than becoming an unknown that
// Terraform refuses to converge.
func TestHostGroupsDataSourcePlan_NoFilters(t *testing.T) {
	planTest(t, hostGroupsDataSourceHandler)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_host_groups" "all" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_host_groups.all", "host_groups.#", "1"),
				resource.TestCheckNoResourceAttr("data.fivenines_host_groups.all", "query"),
			),
		}},
	})
}

// order and direction are enumerated server-side, so a typo has to fail at plan
// time with the accepted values rather than reaching the API and coming back as
// an opaque 400 mid-apply.
func TestHostGroupsDataSourcePlan_RejectsInvalidOrderAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_host_groups" "test" {
  order = "postion"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute order value must be one of.*"position"`),
		}},
	})
}
