package provider_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCheckUptimeMonitorDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_uptime_monitor", func(ctx context.Context, id string) error {
		_, _, err := testAccClient().GetUptimeMonitor(ctx, id)
		return err
	})
}

func testAccUptimeMonitorHTTPSConfig(name, url string, interval int) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "test" {
  name             = %[1]q
  protocol         = "https"
  url              = %[2]q
  interval_seconds = %[3]d
}
`, name, url, interval)
}

func TestAccUptimeMonitor_httpsLifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-monitor")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckUptimeMonitorDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccUptimeMonitorHTTPSConfig(name, "https://example.com/health", 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "name", name),
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "url", "https://example.com/health"),
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "http_method", "GET"),
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "ip_version", "auto"),
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "follow_redirects", "true"),
					// Unset optional attributes must read back as null.
					resource.TestCheckNoResourceAttr("fivenines_uptime_monitor.test", "keyword"),
					resource.TestCheckNoResourceAttr("fivenines_uptime_monitor.test", "port"),
					resource.TestCheckNoResourceAttr("fivenines_uptime_monitor.test", "dns_record_type"),
				),
			},
			{
				ResourceName:      "fivenines_uptime_monitor.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccUptimeMonitorHTTPSConfig(name+"-renamed", "https://example.com/status", 600),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "url", "https://example.com/status"),
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "interval_seconds", "600"),
				),
			},
		},
	})
}

// Dropping keyword from the configuration has to clear it on the API. With
// `omitempty` the key never reached the request body and the keyword stuck.
func TestAccUptimeMonitor_clearsKeyword(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-monitor")

	withKeyword := fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "test" {
  name     = %[1]q
  protocol = "https"
  url      = "https://example.com/health"
  keyword  = "healthy"
}
`, name)

	withoutKeyword := fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "test" {
  name     = %[1]q
  protocol = "https"
  url      = "https://example.com/health"
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckUptimeMonitorDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: withKeyword,
				Check:  resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "keyword", "healthy"),
			},
			{
				Config: withoutKeyword,
				Check:  resource.TestCheckNoResourceAttr("fivenines_uptime_monitor.test", "keyword"),
			},
		},
	})
}

// dns_expected_records is the array version of the same bug: the API normalises
// an emptied list to null, and `omitempty` meant the provider could never send
// the empty list that gets it there.
func TestAccUptimeMonitor_clearsDNSExpectedRecords(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-monitor")

	dnsConfig := func(records string) string {
		return fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "test" {
  name                 = %[1]q
  protocol             = "dns"
  hostname             = "example.com"
  dns_record_type      = "A"
  dns_expected_records = %[2]s
}
`, name, records)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckUptimeMonitorDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: dnsConfig(`["93.184.216.34"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "dns_expected_records.#", "1"),
					resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "dns_expected_records.0", "93.184.216.34"),
				),
			},
			{
				Config: dnsConfig(`[]`),
				Check:  resource.TestCheckResourceAttr("fivenines_uptime_monitor.test", "dns_expected_records.#", "0"),
			},
		},
	})
}

func TestAccUptimeMonitor_rejectsMissingProtocolFields(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-monitor")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "test" {
  name     = %[1]q
  protocol = "https"
}
`, name),
				PlanOnly:    true,
				ExpectError: regexpMissingRequired,
			},
			{
				Config: fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "test" {
  name     = %[1]q
  protocol = "tcp"
  hostname = "example.com"
}
`, name),
				PlanOnly:    true,
				ExpectError: regexpMissingRequired,
			},
			{
				Config: fmt.Sprintf(providerConfig+`
resource "fivenines_uptime_monitor" "test" {
  name     = %[1]q
  protocol = "dns"
  hostname = "example.com"
}
`, name),
				PlanOnly:    true,
				ExpectError: regexpMissingRequired,
			},
		},
	})
}
