package provider_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// workflowDescriptionHandler models the server's own handling of `description`:
// the column is nullable, so a create that omits the key stores NULL and every
// later read answers `"description": null`. A create that sends the key stores
// what it was sent, empty string included.
//
// That is the whole contract this test turns on. The provider decides which of
// the two the practitioner gets, and it decides it in a json struct tag.
func workflowDescriptionHandler() func(http.ResponseWriter, *http.Request) {
	// nil until a create sends the key — exactly the column's own state.
	var stored *string

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Workflow map[string]json.RawMessage `json:"workflow"`
			}
			json.Unmarshal(body, &payload)
			if raw, sent := payload.Workflow["description"]; sent {
				var s *string
				json.Unmarshal(raw, &s)
				stored = s
			}
			w.WriteHeader(http.StatusCreated)
		}

		w.Header().Set("ETag", `"wf"`)
		json.NewEncoder(w).Encode(map[string]interface{}{"workflow": map[string]interface{}{
			"id": 42, "name": "CPU Alert", "description": stored, "status": "draft",
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}})
	}
}

// An empty description is a legal configuration for an Optional attribute, and
// it has to survive the round trip: Terraform compares what the provider returns
// against the configuration and aborts the apply when they differ.
//
//	Error: Provider produced inconsistent result after apply
//	.description: was cty.StringVal(""), but now null
//
// It aborts because `json:"description,omitempty"` cannot express an empty
// string — omitempty drops "" and null is what comes back. The PlanOnly step
// then proves the value is stable rather than merely accepted once.
func TestWorkflowPlan_EmptyDescriptionRoundTrips(t *testing.T) {
	planTest(t, workflowDescriptionHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_workflow" "test" {
  name        = "CPU Alert"
  description = ""
}`,
			Check: resource.TestCheckResourceAttr("fivenines_workflow.test", "description", ""),
		}, {
			Config: providerConfig + `
resource "fivenines_workflow" "test" {
  name        = "CPU Alert"
  description = ""
}`,
			PlanOnly: true,
		}},
	})
}

// The unconfigured case must keep answering null rather than "": the two are
// different answers, and collapsing them is what the nullable pointer types on
// the model exist to prevent.
func TestWorkflowPlan_UnsetDescriptionStaysNull(t *testing.T) {
	planTest(t, workflowDescriptionHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_workflow" "test" {
  name = "CPU Alert"
}`,
			Check: resource.TestCheckNoResourceAttr("fivenines_workflow.test", "description"),
		}},
	})
}
