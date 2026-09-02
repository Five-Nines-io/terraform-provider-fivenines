package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccCheckWorkflowDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_workflow", func(ctx context.Context, id string) error {
		numericID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return err
		}
		_, _, err = testAccClient().GetWorkflow(ctx, numericID)
		return err
	})
}

func testAccWorkflowConfig(name, description string, interval int) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_workflow" "test" {
  name             = %[1]q
  description      = %[2]q
  interval_seconds = %[3]d
}
`, name, description, interval)
}

// A workflow with no published graph stays a draft, which is enough to cover
// the metadata lifecycle without depending on the org's node type catalogue.
func TestAccWorkflow_metadataLifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-workflow")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWorkflowDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccWorkflowConfig(name, "created by the acceptance suite", 60),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_workflow.test", "name", name),
					resource.TestCheckResourceAttr("fivenines_workflow.test", "interval_seconds", "60"),
					resource.TestCheckResourceAttr("fivenines_workflow.test", "active", "false"),
					resource.TestCheckResourceAttr("fivenines_workflow.test", "status", "draft"),
					// A draft has no published version and no derived trigger.
					resource.TestCheckNoResourceAttr("fivenines_workflow.test", "published_version_id"),
				),
			},
			{
				ResourceName:      "fivenines_workflow.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The graph is no longer ignored: Read follows published_version_id
				// to the version endpoint, and a draft has nothing published, so
				// both sides of the comparison are null.
			},
			{
				Config: testAccWorkflowConfig(name+"-renamed", "updated by the acceptance suite", 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_workflow.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("fivenines_workflow.test", "interval_seconds", "300"),
				),
			},
		},
	})
}

// Reformatting the graph must not publish a new version. The plan still shows an
// in-place update: the framework applies semantic equality in ReadResource,
// CreateResource and UpdateResource but not in PlanResourceChange, so a
// reformatted config always differs from the stored string at plan time. What
// jsontypes.Normalized buys is that shouldPublishGraph suppresses the republish,
// which is the part that actually costs something — asserted here by pinning the
// published version id across the reformat.
func TestAccWorkflow_executionGraphIgnoresFormatting(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-workflow")

	config := func(graph string) string {
		return fmt.Sprintf(providerConfig+`
resource "fivenines_workflow" "test" {
  name                 = %[1]q
  execution_graph_json = %[2]q
}
`, name, graph)
	}

	compact := `{"nodes":[],"edges":[]}`
	reformatted := "{\n  \"edges\": [],\n  \"nodes\": []\n}"
	var publishedBefore string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWorkflowDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: config(compact),
				Check:  captureAttr("fivenines_workflow.test", "published_version_id", &publishedBefore),
			},
			{
				Config: config(reformatted),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_workflow.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: expectAttrUnchanged("fivenines_workflow.test", "published_version_id", &publishedBefore),
			},
		},
	})
}

// captureAttr records an attribute value so a later step can assert it did not
// move. TestCheckResourceAttrPair cannot express this: pointing it at the same
// resource and the same attribute compares a value to itself and always passes.
func captureAttr(resourceName, attr string, out *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("%s not found in state", resourceName)
		}
		value, ok := rs.Primary.Attributes[attr]
		if !ok || value == "" {
			return fmt.Errorf("%s.%s is not set", resourceName, attr)
		}
		*out = value
		return nil
	}
}

func expectAttrUnchanged(resourceName, attr string, want *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("%s not found in state", resourceName)
		}
		if got := rs.Primary.Attributes[attr]; got != *want {
			return fmt.Errorf("%s.%s changed from %q to %q; the reformat republished",
				resourceName, attr, *want, got)
		}
		return nil
	}
}

// The graph is read back from the published version, so importing a workflow
// that has one recovers it — the point of #10.
//
// The comparison is semantic, not byte-for-byte: state holds the string the
// configuration wrote, while an import holds the API's re-serialisation of the
// same graph, and nothing promises those agree on key order. ImportStateVerify
// compares raw strings, so the graph is excluded there and checked properly here.
func TestAccWorkflow_importRecoversThePublishedGraph(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-workflow")
	graph := `{"nodes":[],"edges":[]}`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWorkflowDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(providerConfig+`
resource "fivenines_workflow" "test" {
  name                 = %[1]q
  execution_graph_json = %[2]q
}
`, name, graph),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("fivenines_workflow.test", "published_version_id"),
					resource.TestCheckResourceAttr("fivenines_workflow.test", "execution_graph_json", graph),
				),
			},
			{
				ResourceName:            "fivenines_workflow.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"execution_graph_json"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported instance, got %d", len(states))
					}
					imported := states[0].Attributes["execution_graph_json"]
					if imported == "" {
						return fmt.Errorf("import did not recover the published graph")
					}
					var want, got interface{}
					if err := json.Unmarshal([]byte(graph), &want); err != nil {
						return err
					}
					if err := json.Unmarshal([]byte(imported), &got); err != nil {
						return fmt.Errorf("imported graph is not valid JSON: %w", err)
					}
					if !reflect.DeepEqual(want, got) {
						return fmt.Errorf("imported graph %s is not the published graph %s", imported, graph)
					}
					return nil
				},
			},
		},
	})
}
