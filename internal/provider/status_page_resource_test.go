package provider_test

import (
	"context"
	"fmt"
	"regexp"
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

// The page composition fields added in #12: sections, per-item labels and the
// uptime bar settings. `logo` is deliberately absent — it needs a white-label
// plan, so an acceptance run on a standard staging org would 422.
func TestAccStatusPage_composition(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-page")

	config := fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "api" {
  name     = "%[1]s-api"
  protocol = "https"
  url      = "https://example.com/health"
}

resource "fivenines_status_page" "test" {
  name                           = %[1]q
  contact_url                    = "https://example.com/support"
  subscriptions_enabled          = false
  search_indexing_enabled        = false
  uptime_green_tolerance_seconds = 120
  uptime_window_days             = 90
  sections                       = ["Core services"]

  items = [{
    item_type     = "UptimeMonitor"
    item_id       = fivenines_uptime_monitor.api.id
    display_label = "Public API"
    description   = "REST and GraphQL endpoints"
    section       = "Core services"
  }]
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStatusPageDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_status_page.test", "contact_url", "https://example.com/support"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "subscriptions_enabled", "false"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "search_indexing_enabled", "false"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "uptime_green_tolerance_seconds", "120"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "uptime_window_days", "90"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "sections.#", "1"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "sections.0", "Core services"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "items.0.display_label", "Public API"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "items.0.description", "REST and GraphQL endpoints"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "items.0.section", "Core services"),
				),
			},
			{
				ResourceName:      "fivenines_status_page.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The API never returns the image, so it has nothing to verify
				// against and an import always reports it as null.
				ImportStateVerifyIgnore: []string{"logo"},
			},
		},
	})
}

// Terraform pairs list elements by index, so prepending an item lines the new
// one up against the previous occupant's row. Its label must NOT follow: the
// label belongs to the item, not to the position. This is the end-to-end guard
// on PreserveForSameItem and mergePlannedItems working together.
func TestAccStatusPage_reorderKeepsLabelsWithTheirItems(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-page")

	config := func(items string) string {
		return fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "api" {
  name     = "%[1]s-api"
  protocol = "https"
  url      = "https://example.com/api"
}

resource "fivenines_uptime_monitor" "web" {
  name     = "%[1]s-web"
  protocol = "https"
  url      = "https://example.com/web"
}

resource "fivenines_status_page" "test" {
  name  = %[1]q
  items = %[2]s
}
`, name, items)
	}

	// Only the API monitor is listed, and only it carries a label.
	labelled := config(`[{
    item_type     = "UptimeMonitor"
    item_id       = fivenines_uptime_monitor.api.id
    display_label = "Public API"
  }]`)

	// The web monitor is prepended and never given a label. It now sits at the
	// index the API monitor held.
	prepended := config(`[
    {
      item_type = "UptimeMonitor"
      item_id   = fivenines_uptime_monitor.web.id
    },
    {
      item_type     = "UptimeMonitor"
      item_id       = fivenines_uptime_monitor.api.id
      display_label = "Public API"
    },
  ]`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStatusPageDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: labelled,
				Check:  resource.TestCheckResourceAttr("fivenines_status_page.test", "items.0.display_label", "Public API"),
			},
			{
				Config: prepended,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_status_page.test", "items.#", "2"),
					resource.TestCheckResourceAttrPair("fivenines_status_page.test", "items.0.item_id", "fivenines_uptime_monitor.web", "id"),
					// The bug this guards: "Public API" leaking onto the web monitor.
					resource.TestCheckNoResourceAttr("fivenines_status_page.test", "items.0.display_label"),
					resource.TestCheckResourceAttr("fivenines_status_page.test", "items.1.display_label", "Public API"),
				),
			},
		},
	})
}

// A section an item references has to be declared in `sections`, which the API
// enforces with a 422. ValidateConfig catches it at plan time instead.
func TestAccStatusPage_rejectsUndeclaredSection(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-page")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(providerConfig+`
resource "fivenines_status_page" "test" {
  name     = %[1]q
  sections = ["Core services"]

  items = [{
    item_type = "Host"
    item_id   = "00000000-0000-0000-0000-000000000000"
    section   = "Edge"
  }]
}
`, name),
				ExpectError: regexp.MustCompile(`Undeclared Section`),
			},
		},
	})
}
