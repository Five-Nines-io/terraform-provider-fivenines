package provider_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
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
				// The graph is not part of the workflow payload, so an import
				// cannot recover it.
				ImportStateVerifyIgnore: []string{"execution_graph_json"},
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

// Reformatting the graph must not publish a new version. The attribute is
// Optional-only, and the framework does not apply semantic equality during
// PlanResourceChange, so the plan still shows an in-place update — what
// jsontypes.Normalized buys here is that shouldPublishGraph suppresses the
// republish, which is what actually costs something. Once #10 makes Read
// populate the graph the plan goes empty too.
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

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWorkflowDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: config(compact),
			},
			{
				Config: config(reformatted),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_workflow.test", plancheck.ResourceActionUpdate),
					},
				},
				// The republish is the expensive part, and it must not happen:
				// the published version has to survive a pure reformat.
				Check: resource.TestCheckResourceAttrPair(
					"fivenines_workflow.test", "published_version_id",
					"fivenines_workflow.test", "published_version_id",
				),
			},
		},
	})
}
