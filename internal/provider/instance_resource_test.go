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

// The writable host-settings surface against the real API: configured
// settings and a credential land and read back, the credential drops without
// wiping the stored value, and the host leaves its group when the reference
// goes. The PlanOnly steps are the live half of what the plan tests assert
// against a fake — no setting may plan a permanent diff.
func TestAccInstance_settings(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-instance")
	group := acctest.RandomWithPrefix("tf-acc-group")

	full := fmt.Sprintf(providerConfig+`
resource "fivenines_host_group" "test" {
  name = %[2]q
}

resource "fivenines_instance" "test" {
  display_name   = %[1]q
  description    = "managed by terraform"
  cluster_name   = "tf-acc"
  host_group_id  = fivenines_host_group.test.id
  docker_enabled = true
  redis_enabled  = true
  redis_port     = 6390
  redis_password = "s3cret"
}
`, name, group)

	// The credential and the group reference are gone; docker_enabled is no
	// longer managed. On the wire: redis_enabled flips, host_group_id clears
	// to an explicit null, and the rest is simply omitted.
	trimmed := fmt.Sprintf(providerConfig+`
resource "fivenines_host_group" "test" {
  name = %[2]q
}

resource "fivenines_instance" "test" {
  display_name  = %[1]q
  description   = "managed by terraform"
  cluster_name  = "tf-acc"
  redis_enabled = false
}
`, name, group)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckInstanceDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: full,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_instance.test", "description", "managed by terraform"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "cluster_name", "tf-acc"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "docker_enabled", "true"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_port", "6390"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "mysql_password_set", "false"),
					resource.TestCheckResourceAttrSet("fivenines_instance.test", "host_group_id"),
				),
			},
			{Config: full, PlanOnly: true},
			{
				ResourceName:      "fivenines_instance.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Write-only: an import cannot know the credential.
				ImportStateVerifyIgnore: []string{"redis_password"},
			},
			{
				Config: trimmed,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_enabled", "false"),
					// Dropped from the configuration, kept by the server: the
					// credential survives as blank-means-keep promises, and an
					// unmanaged toggle keeps its last value.
					resource.TestCheckNoResourceAttr("fivenines_instance.test", "redis_password"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "docker_enabled", "true"),
					// The group reference is a clear, not a keep.
					resource.TestCheckNoResourceAttr("fivenines_instance.test", "host_group_id"),
				),
			},
			{Config: trimmed, PlanOnly: true},
		},
	})
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
