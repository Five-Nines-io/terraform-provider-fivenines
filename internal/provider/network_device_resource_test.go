package provider_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCheckNetworkDeviceDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_network_device", func(ctx context.Context, id string) error {
		_, _, err := testAccClient().GetNetworkDevice(ctx, id)
		return err
	})
}

func testAccNetworkDeviceConfig(name, ip string, interval int) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_network_device" "test" {
  name             = %[1]q
  ip_address       = %[2]q
  snmp_version     = "v2c"
  snmp_community   = "tf-acc-community"
  polling_interval = %[3]d
}
`, name, ip, interval)
}

func TestAccNetworkDevice_lifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-device")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Device deletion is a 202 like instances: this catches the provider
		// returning before the teardown finished.
		CheckDestroy: testAccCheckNetworkDeviceDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkDeviceConfig(name, "192.0.2.10", 60),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_network_device.test", "name", name),
					resource.TestCheckResourceAttr("fivenines_network_device.test", "ip_address", "192.0.2.10"),
					resource.TestCheckResourceAttr("fivenines_network_device.test", "snmp_version", "v2c"),
					resource.TestCheckResourceAttr("fivenines_network_device.test", "device_type", "other"),
					resource.TestCheckResourceAttr("fivenines_network_device.test", "polling_interval", "60"),
					// Defaults must survive the read even though a v2c device
					// leaves the SNMPv3 fields null on the API side.
					resource.TestCheckResourceAttr("fivenines_network_device.test", "snmp_security_level", "no_auth_no_priv"),
					resource.TestCheckNoResourceAttr("fivenines_network_device.test", "polling_host_id"),
				),
			},
			{
				ResourceName:      "fivenines_network_device.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Write-only credentials are never returned, so an imported
				// device legitimately has none of them.
				ImportStateVerifyIgnore: []string{
					"snmp_community",
					"snmp_auth_password",
					"snmp_priv_password",
				},
			},
			{
				Config: testAccNetworkDeviceConfig(name+"-renamed", "192.0.2.11", 120),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_network_device.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("fivenines_network_device.test", "ip_address", "192.0.2.11"),
					resource.TestCheckResourceAttr("fivenines_network_device.test", "polling_interval", "120"),
				),
			},
		},
	})
}
