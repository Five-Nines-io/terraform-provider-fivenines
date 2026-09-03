package provider_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCheckTaskDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_task", func(ctx context.Context, id string) error {
		_, _, err := testAccClient().GetTask(ctx, id)
		return err
	})
}

func testAccTaskCronConfig(name, schedule string) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_task" "test" {
  name          = %[1]q
  schedule_type = "cron"
  schedule      = %[2]q
}
`, name, schedule)
}

func testAccTaskIntervalConfig(name string, seconds int) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_task" "test" {
  name             = %[1]q
  schedule_type    = "interval"
  interval_seconds = %[2]d
}
`, name, seconds)
}

func TestAccTask_cronLifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-task")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckTaskDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccTaskCronConfig(name, "0 2 * * *"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "name", name),
					resource.TestCheckResourceAttr("fivenines_task.test", "schedule", "0 2 * * *"),
					resource.TestCheckResourceAttr("fivenines_task.test", "time_zone", "UTC"),
					resource.TestCheckResourceAttrSet("fivenines_task.test", "ping_key"),
					resource.TestCheckResourceAttrSet("fivenines_task.test", "ping_url"),
					// A cron task has no interval: the API returns null and it
					// has to stay null rather than land in state as 0.
					resource.TestCheckNoResourceAttr("fivenines_task.test", "interval_seconds"),
				),
			},
			{
				ResourceName:      "fivenines_task.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccTaskCronConfig(name+"-renamed", "30 3 * * *"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("fivenines_task.test", "schedule", "30 3 * * *"),
				),
			},
		},
	})
}

func TestAccTask_intervalLifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-task")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckTaskDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccTaskIntervalConfig(name, 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "interval_seconds", "300"),
					// Mirror image of the cron case: no schedule, so null.
					resource.TestCheckNoResourceAttr("fivenines_task.test", "schedule"),
				),
			},
			{
				Config: testAccTaskIntervalConfig(name, 900),
				Check:  resource.TestCheckResourceAttr("fivenines_task.test", "interval_seconds", "900"),
			},
		},
	})
}

// Removing an optional attribute from the configuration has to clear it
// server-side. host_id used to be dropped from the PATCH body when null, so the
// association survived and every subsequent plan diffed.
func TestAccTask_clearsHostAssociation(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-task")

	withHost := fmt.Sprintf(providerConfig+`
resource "fivenines_instance" "host" {
  display_name = %[1]q
}

resource "fivenines_task" "test" {
  name             = %[1]q
  schedule_type    = "interval"
  interval_seconds = 300
  host_id          = fivenines_instance.host.id
}
`, name)

	withoutHost := fmt.Sprintf(providerConfig+`
resource "fivenines_instance" "host" {
  display_name = %[1]q
}

resource "fivenines_task" "test" {
  name             = %[1]q
  schedule_type    = "interval"
  interval_seconds = 300
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckTaskDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: withHost,
				Check:  resource.TestCheckResourceAttrPair("fivenines_task.test", "host_id", "fivenines_instance.host", "id"),
			},
			{
				Config: withoutHost,
				Check:  resource.TestCheckNoResourceAttr("fivenines_task.test", "host_id"),
			},
		},
	})
}

// The API rejects these with a 422; the provider should say so at plan time.
func TestAccTask_rejectsIncompleteSchedule(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-task")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(providerConfig+`
resource "fivenines_task" "test" {
  name          = %[1]q
  schedule_type = "cron"
}
`, name),
				PlanOnly:    true,
				ExpectError: regexpMissingRequired,
			},
			{
				Config: fmt.Sprintf(providerConfig+`
resource "fivenines_task" "test" {
  name          = %[1]q
  schedule_type = "interval"
}
`, name),
				PlanOnly:    true,
				ExpectError: regexpMissingRequired,
			},
		},
	})
}
