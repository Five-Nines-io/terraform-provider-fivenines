package provider_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// These drive REAL Terraform against a fake API, so they need no organisation and
// no key — unlike the TF_ACC-gated suites, they run wherever the terraform binary
// is (CI has it, because the docs check shells out to tfplugindocs).
//
// They exist because the unit tests cannot see plan validation at all. Driving
// Create and Update directly skips the step where Terraform compares the plan to
// the configuration, and an earlier revision of this resource planned position as
// unknown whenever it changed: every unit test passed while `position = 5` failed
// at plan time for every practitioner, before the API was ever called.
//
//	Error: Provider produced invalid plan
//	planned value cty.UnknownVal(cty.Number) does not match config value
//	cty.NumberIntVal(5)
func hostGroupPlanTest(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform CLI not on PATH — skipping plan-validation test")
	}
	srv := httptest.NewServer(http.HandlerFunc(respond))
	t.Cleanup(srv.Close)
	t.Setenv("FIVENINES_BASE_URL", srv.URL)
	t.Setenv("FIVENINES_API_KEY", "fn_test")
	t.Setenv("TF_ACC", "1") // hermetic: the fake server above is the whole API
	return srv
}

func hostGroupHandler(position int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("ETag", `"hg"`)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"host_group": map[string]interface{}{
			"id": 7, "name": "Production", "position": position,
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}})
	}
}

// A configured position must survive plan validation and produce a completed
// apply even when the API clamps it to a different number.
//
// ExpectNonEmptyPlan is the documented non-convergence, not a defect being
// papered over: the practitioner asked for slot 5, the API answered 1, and the
// standing diff is the provider saying so out loud. The assertions that matter
// are that the plan is VALID and the apply COMPLETES — the alternative designs
// both abort, one at plan time ("planned value cty.UnknownVal does not match
// config value") and one at apply time ("inconsistent result after apply").
func TestHostGroupPlan_ExplicitPositionSurvivesClamping(t *testing.T) {
	hostGroupPlanTest(t, hostGroupHandler(1)) // config asks 5, API answers 1

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_host_group" "test" {
  name     = "Production"
  position = 5
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("fivenines_host_group.test", "position", "5"),
				resource.TestCheckResourceAttr("fivenines_host_group.test", "name", "Production"),
			),
			ExpectNonEmptyPlan: true,
		}},
	})
}

// With no configured position the API's assignment is the only answer, and it
// must land in state without leaving an unknown behind.
func TestHostGroupPlan_UnconfiguredPositionTakesTheAPIValue(t *testing.T) {
	hostGroupPlanTest(t, hostGroupHandler(3))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_host_group" "test" {
  name = "Production"
}`,
			Check: resource.TestCheckResourceAttr("fivenines_host_group.test", "position", "3"),
		}},
	})
}

// A second apply of an identical configuration must be an empty plan.
func TestHostGroupPlan_ConfiguredPositionIsStable(t *testing.T) {
	hostGroupPlanTest(t, hostGroupHandler(2))

	cfg := providerConfig + `
resource "fivenines_host_group" "test" {
  name     = "Production"
  position = 2
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{Config: cfg, PlanOnly: true},
		},
	})
}
