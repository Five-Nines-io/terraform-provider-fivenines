package provider_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCheckInstanceDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_instance", func(ctx context.Context, id string) error {
		_, _, err := testAccClient().GetInstance(ctx, id)
		return err
	})
}

func testAccInstanceConfig(displayName string, maintenance bool) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_instance" "test" {
  display_name     = %[1]q
  maintenance_mode = %[2]t
}
`, displayName, maintenance)
}

func TestAccInstance_lifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-instance")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// The instance DELETE is a 202: this fails if the provider stops
		// waiting for the asynchronous teardown.
		CheckDestroy: testAccCheckInstanceDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig(name, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_instance.test", "display_name", name),
					resource.TestCheckResourceAttr("fivenines_instance.test", "enabled", "true"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "maintenance_mode", "false"),
					resource.TestCheckResourceAttrSet("fivenines_instance.test", "id"),
					resource.TestCheckResourceAttrSet("fivenines_instance.test", "created_at"),
					// A host that has never reported stays null here. Mapping
					// the API's null to "" made every plan drift.
					resource.TestCheckNoResourceAttr("fivenines_instance.test", "last_sync_at"),
				),
			},
			{
				ResourceName:      "fivenines_instance.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig(name+"-renamed", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_instance.test", "display_name", name+"-renamed"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "maintenance_mode", "true"),
				),
			},
		},
	})
}
