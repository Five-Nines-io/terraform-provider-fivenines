package provider_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// MQTT is gated per organisation. Without the feature these suites fail on the
// first create with a 403 naming the entitlement, which is a legible failure
// rather than a confusing one — point them at a staging org with MQTT enabled.

func testAccCheckMQTTBrokerDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_mqtt_broker", func(ctx context.Context, id string) error {
		_, _, err := testAccClient().GetMQTTBroker(ctx, id)
		return err
	})
}

func testAccMQTTBrokerConfig(name, host string, port int) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_mqtt_broker" "test" {
  name = %[1]q
  host = %[2]q
  port = %[3]d
}
`, name, host, port)
}

func testAccMQTTBrokerConfigWithCredentials(name, host string, port int, username, password string) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_mqtt_broker" "test" {
  name     = %[1]q
  host     = %[2]q
  port     = %[3]d
  username = %[4]q
  password = %[5]q
}
`, name, host, port, username, password)
}

// Full CRUD plus import. The import step ignores the two write-only credentials:
// the API never returns either, so an imported broker reports them null no
// matter what is stored — which is exactly what `username_set` is for.
func TestAccMQTTBroker_lifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-mqtt")
	renamed := name + "-renamed"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckMQTTBrokerDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccMQTTBrokerConfig(name, "mqtt.internal", 1883),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "name", name),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "host", "mqtt.internal"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "port", "1883"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "tls", "false"),
					// Anonymous: nothing stored, and the booleans say so.
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "username_set", "false"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "password_set", "false"),
					// No watcher yet, so nothing is collected and status is unknown.
					resource.TestCheckResourceAttrSet("fivenines_mqtt_broker.test", "status"),
					resource.TestCheckResourceAttrSet("fivenines_mqtt_broker.test", "id"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "topic_monitor_count", "0"),
				),
			},
			{
				ResourceName:            "fivenines_mqtt_broker.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"username", "password"},
			},
			{
				Config: testAccMQTTBrokerConfigWithCredentials(renamed, "mqtt.internal", 8883, "factory", "s3cret"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "name", renamed),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "port", "8883"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "username_set", "true"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "password_set", "true"),
				),
			},
		},
	})
}

// The preserve-on-omission contract against the real API: drop both credentials
// from the configuration and the broker keeps them, so `username_set` and
// `password_set` stay true. If the update ever sends an explicit null instead of
// omitting the key, this is the test that catches it — and the only place it
// CAN be caught, since no unit test knows what the server stored.
func TestAccMQTTBroker_droppedCredentialsArePreserved(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-mqtt-creds")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckMQTTBrokerDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccMQTTBrokerConfigWithCredentials(name, "mqtt.internal", 1883, "factory", "s3cret"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "username_set", "true"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "password_set", "true"),
				),
			},
			{
				// Same broker, credentials gone from the configuration, port moved
				// so the update is a real one.
				Config: testAccMQTTBrokerConfig(name, "mqtt.internal", 8883),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "port", "8883"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "username_set", "true"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "password_set", "true"),
				),
			},
		},
	})
}

func testAccCheckMQTTTopicMonitorDestroyed() resource.TestCheckFunc {
	return func(state *terraform.State) error {
		for name, rs := range state.RootModule().Resources {
			if rs.Type != "fivenines_mqtt_topic_monitor" {
				continue
			}
			brokerID := rs.Primary.Attributes["mqtt_broker_id"]
			_, _, err := testAccClient().GetMQTTTopicMonitor(context.Background(), brokerID, rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("%s (%s) still exists", name, rs.Primary.ID)
			}
			if !isNotFound(err) {
				return fmt.Errorf("%s: unexpected error checking destruction: %w", name, err)
			}
		}
		return nil
	}
}

func testAccMQTTTopicMonitorConfig(brokerName, monitor string) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_mqtt_broker" "test" {
  name = %[1]q
  host = "mqtt.internal"
}

%[2]s
`, brokerName, monitor)
}

// Full CRUD plus a composite import. A monitor is only addressable under its
// broker, so the import ID carries both UUIDs — this is the step that proves the
// parser and the nested Read agree.
func TestAccMQTTTopicMonitor_lifecycle(t *testing.T) {
	brokerName := acctest.RandomWithPrefix("tf-acc-mqtt-mon")

	freshnessOnly := `
resource "fivenines_mqtt_topic_monitor" "test" {
  mqtt_broker_id      = fivenines_mqtt_broker.test.id
  topic_filter        = "sensors/+/temperature"
  stale_after_seconds = 300
}`
	bothChecks := `
resource "fivenines_mqtt_topic_monitor" "test" {
  mqtt_broker_id      = fivenines_mqtt_broker.test.id
  topic_filter        = "sensors/+/temperature"
  stale_after_seconds = 600
  match_kind          = "exact"
  expected_value      = "online"
}`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckMQTTTopicMonitorDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccMQTTTopicMonitorConfig(brokerName, freshnessOnly),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "topic_filter", "sensors/+/temperature"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "stale_after_seconds", "300"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "freshness_check", "true"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "payload_check", "false"),
					// A wildcard filter cannot know a topic exists until it sees one.
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "exact_topic", "false"),
					// capture_payload defaults on, and no payload check means the
					// effective value matches the stored one.
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "capture_payload", "true"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "effective_capture_payload", "true"),
				),
			},
			{
				ResourceName:      "fivenines_mqtt_topic_monitor.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(state *terraform.State) (string, error) {
					rs, ok := state.RootModule().Resources["fivenines_mqtt_topic_monitor.test"]
					if !ok {
						return "", fmt.Errorf("fivenines_mqtt_topic_monitor.test not in state")
					}
					return rs.Primary.Attributes["mqtt_broker_id"] + ":" + rs.Primary.ID, nil
				},
			},
			{
				Config: testAccMQTTTopicMonitorConfig(brokerName, bothChecks),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "stale_after_seconds", "600"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "match_kind", "exact"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "payload_check", "true"),
				),
			},
			{
				// Dropping the payload check has to clear it server-side, which
				// only the explicit null on the wire achieves.
				Config: testAccMQTTTopicMonitorConfig(brokerName, freshnessOnly),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("fivenines_mqtt_topic_monitor.test", "match_kind"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "payload_check", "false"),
				),
			},
		},
	})
}

// A monitor with neither check is rejected at plan time, before the API is
// reached — which is what keeps a generated fleet from half-applying.
func TestAccMQTTTopicMonitor_rejectsMonitorWithNoCheck(t *testing.T) {
	brokerName := acctest.RandomWithPrefix("tf-acc-mqtt-nocheck")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccMQTTTopicMonitorConfig(brokerName, `
resource "fivenines_mqtt_topic_monitor" "test" {
  mqtt_broker_id = fivenines_mqtt_broker.test.id
  topic_filter   = "sensors/+/temperature"
}`),
			ExpectError: regexp.MustCompile("stale_after_seconds"),
		}},
	})
}
