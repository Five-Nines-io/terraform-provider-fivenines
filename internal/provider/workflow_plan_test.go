package provider_test

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// workflowDescriptionHandler models the server's own handling of `description`:
// the column is nullable, so a write that omits the key leaves it alone and
// every read answers whatever is stored, `null` included. A write that sends the
// key stores what it was sent, empty string included.
//
// That is the whole contract these tests turn on. The provider decides which of
// the two the practitioner gets, and it decides it in a json struct tag.
//
// The mutex is not decoration: httptest may serve handlers concurrently, and the
// update test drives several requests through this one closure.
func workflowDescriptionHandler() func(http.ResponseWriter, *http.Request) {
	var (
		mu sync.Mutex
		// nil until a write sends the key — exactly the column's own state.
		stored *string
	)

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		mu.Lock()
		defer mu.Unlock()

		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var payload struct {
				Workflow map[string]json.RawMessage `json:"workflow"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Key absent means "leave it alone"; key present means "store this",
			// which is the distinction omitempty either preserves or destroys.
			if raw, sent := payload.Workflow["description"]; sent {
				var s *string
				if err := json.Unmarshal(raw, &s); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				stored = s
			}
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
			}
		}

		w.Header().Set("ETag", `"wf"`)
		json.NewEncoder(w).Encode(map[string]interface{}{"workflow": map[string]interface{}{
			"id": 42, "name": "CPU Alert", "description": stored, "status": "draft",
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}})
	}
}

func workflowConfig(description string) string {
	if description == "" {
		return providerConfig + `
resource "fivenines_workflow" "test" {
  name        = "CPU Alert"
  description = ""
}`
	}
	return providerConfig + `
resource "fivenines_workflow" "test" {
  name        = "CPU Alert"
  description = "` + description + `"
}`
}

// An empty description is a legal configuration for an Optional attribute, and
// it has to survive the round trip: Terraform compares what the provider returns
// against the configuration and aborts the apply when they differ.
//
//	Error: Provider produced inconsistent result after apply
//	.description: was cty.StringVal(""), but now null
//
// It aborted because `json:"description,omitempty"` on a plain string cannot
// express an empty string — omitempty dropped "" and null came back. The
// PlanOnly step then proves the value is stable rather than merely accepted once.
func TestWorkflowPlan_EmptyDescriptionRoundTrips(t *testing.T) {
	planTest(t, workflowDescriptionHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: workflowConfig(""),
			Check:  resource.TestCheckResourceAttr("fivenines_workflow.test", "description", ""),
		}, {
			Config:   workflowConfig(""),
			PlanOnly: true,
		}},
	})
}

// Emptying an existing description is the same contract on the update path,
// which shares the tag policy but not the call site. Create and update are
// classified independently in the guard table, so they are covered independently
// here.
func TestWorkflowPlan_DescriptionCanBeEmptied(t *testing.T) {
	planTest(t, workflowDescriptionHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: workflowConfig("Alerts on high CPU"),
			Check:  resource.TestCheckResourceAttr("fivenines_workflow.test", "description", "Alerts on high CPU"),
		}, {
			Config: workflowConfig(""),
			Check:  resource.TestCheckResourceAttr("fivenines_workflow.test", "description", ""),
		}, {
			Config:   workflowConfig(""),
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
