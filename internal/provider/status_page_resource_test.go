package provider_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCheckStatusPageDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_status_page", func(ctx context.Context, id string) error {
		numericID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return err
		}
		_, _, err = testAccClient().GetStatusPage(ctx, numericID)
		return err
	})
}

func testAccStatusPageConfig(name, theme string) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_status_page" "test" {
  name          = %[1]q
  public        = false
  theme_variant = %[2]q
}
`, name, theme)
}

func TestAccStatusPage_lifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-page")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStatusPageDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccStatusPageConfig(name, "system"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_status_page.test", "name", name),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "public", "false"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "theme_variant", "system"),
					resource.TestCheckResourceAttrSet("fivenines_status_page.test", "slug"),
					resource.TestCheckNoResourceAttr("fivenines_status_page.test", "items"),
				),
			},
			{
				ResourceName:      "fivenines_status_page.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccStatusPageConfig(name+"-renamed", "dark"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_status_page.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "theme_variant", "dark"),
				),
			},
		},
	})
}

// Emptying a page needs an explicit [] on the wire. With `omitempty` the empty
// list marshalled to nothing, the API read that as "keep the items", and the
// page could never be emptied through Terraform.
func TestAccStatusPage_emptiesItems(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-page")

	config := func(items string) string {
		return fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "listed" {
  name     = %[1]q
  protocol = "https"
  url      = "https://example.com/health"
}

resource "fivenines_status_page" "test" {
  name = %[1]q
%[2]s
}
`, name, items)
	}

	withItem := config(`
  items = [{
    item_type = "UptimeMonitor"
    item_id   = fivenines_uptime_monitor.listed.id
  }]`)

	withoutItems := config(`
  items = []`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStatusPageDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: withItem,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_status_page.test", "items.#", "1"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "items.0.item_type", "UptimeMonitor"),
					resource.TestCheckResourceAttrPair("fivenines_status_page.test", "items.0.item_id", "fivenines_uptime_monitor.listed", "id"),
				),
			},
			{
				Config: withoutItems,
				Check:  resource.TestCheckResourceAttr("fivenines_status_page.test", "items.#", "0"),
			},
		},
	})
}
