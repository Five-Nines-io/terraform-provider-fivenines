package provider_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// Like the host group plan tests, these drive REAL Terraform against a fake API,
// so they need no organisation and no key. They exist because the unit tests
// cannot see plan validation: driving Create and Update directly skips the step
// where Terraform compares the plan to the configuration and the applied state
// to the plan.
//
// For MQTT the specific hazard is the write-only credential. `username` and
// `password` are Optional-only and never returned by the API, while
// `username_set` / `password_set` are Computed and do come back. Get that pairing
// wrong and the failure is at plan or apply time, never in a unit test:
//
//	Error: Provider produced inconsistent result after apply
//	.username_set: was cty.True, but now cty.False

func mqttPlanTest(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform CLI not on PATH — skipping plan-validation test")
	}
	srv := httptest.NewServer(http.HandlerFunc(respond))
	t.Cleanup(srv.Close)
	t.Setenv("FIVENINES_BASE_URL", srv.URL)
	t.Setenv("FIVENINES_API_KEY", "fn_test")
	t.Setenv("TF_ACC", "1") // hermetic: the fake server above is the whole API
}

// brokerHandler serves a broker whose stored credentials are whatever the last
// write said, which is what makes the `_set` booleans meaningful across a
// re-plan. Credentials are never echoed back, exactly like the real API.
func brokerHandler() func(http.ResponseWriter, *http.Request) {
	var mu sync.Mutex
	usernameSet, passwordSet := false, false

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		mu.Lock()
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			var body map[string]map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			broker := body["mqtt_broker"]
			// Mirror the server: a key that is absent leaves the stored
			// credential alone; an explicit null clears it; a string sets it.
			if v, present := broker["username"]; present {
				usernameSet = v != nil
			}
			if v, present := broker["password"]; present {
				passwordSet = v != nil
			}
		}
		uSet, pSet := usernameSet, passwordSet
		mu.Unlock()

		w.Header().Set("ETag", `"broker"`)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"mqtt_broker": map[string]interface{}{
			"id": "broker-uuid", "name": "Factory-floor Mosquitto", "host": "mqtt.internal",
			"port": 8883, "tls": true,
			"username_set": uSet, "password_set": pSet,
			"watcher_host_id": nil, "status": "connected", "stale": false,
			"topic_monitor_count": 0,
			"created_at":          "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}})
	}
}

// A configured credential has to survive plan validation, land in state, and
// leave `username_set` true — then plan empty on a second apply. A credential
// the API cannot echo is the classic source of a permanent diff.
func TestMQTTBrokerPlan_ConfiguredCredentialsAreStable(t *testing.T) {
	mqttPlanTest(t, brokerHandler())

	cfg := providerConfig + `
resource "fivenines_mqtt_broker" "test" {
  name     = "Factory-floor Mosquitto"
  host     = "mqtt.internal"
  port     = 8883
  tls      = true
  username = "factory"
  password = "s3cret"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "username", "factory"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "username_set", "true"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "password_set", "true"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// A broker with no credentials in the configuration must not plan an endless
// diff either, and both `_set` booleans have to read false rather than unknown.
func TestMQTTBrokerPlan_AnonymousBrokerIsStable(t *testing.T) {
	mqttPlanTest(t, brokerHandler())

	cfg := providerConfig + `
resource "fivenines_mqtt_broker" "test" {
  name = "Factory-floor Mosquitto"
  host = "mqtt.internal"
  port = 8883
  tls  = true
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "username_set", "false"),
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "password_set", "false"),
					resource.TestCheckNoResourceAttr("fivenines_mqtt_broker.test", "username"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// The whole point of the preserve-on-omission tag: dropping `password` from a
// configuration that had one is an unrelated edit as far as the wire is
// concerned, so the stored credential survives and `password_set` stays true.
// Terraform still records the attribute as gone, which is the honest thing —
// state says "Terraform does not manage a password", `password_set` says "the
// broker has one".
func TestMQTTBrokerPlan_DroppedCredentialLeavesTheStoredValueAlone(t *testing.T) {
	mqttPlanTest(t, brokerHandler())

	withPassword := providerConfig + `
resource "fivenines_mqtt_broker" "test" {
  name     = "Factory-floor Mosquitto"
  host     = "mqtt.internal"
  port     = 8883
  tls      = true
  password = "s3cret"
}`
	withoutPassword := providerConfig + `
resource "fivenines_mqtt_broker" "test" {
  name = "Factory-floor Mosquitto"
  host = "mqtt.internal"
  port = 8883
  tls  = true
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: withPassword,
				Check:  resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "password_set", "true"),
			},
			{
				Config: withoutPassword,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("fivenines_mqtt_broker.test", "password"),
					// Still stored server-side: omission preserves.
					resource.TestCheckResourceAttr("fivenines_mqtt_broker.test", "password_set", "true"),
				),
			},
			{Config: withoutPassword, PlanOnly: true},
		},
	})
}

func topicMonitorHandler() func(http.ResponseWriter, *http.Request) {
	var mu sync.Mutex
	staleAfter := interface{}(nil)
	matchKind := interface{}(nil)
	expectedValue := interface{}(nil)

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		mu.Lock()
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			var body map[string]map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			monitor := body["topic_monitor"]
			if v, present := monitor["stale_after_seconds"]; present {
				staleAfter = v
			}
			if v, present := monitor["match_kind"]; present {
				matchKind = v
			}
			if v, present := monitor["expected_value"]; present {
				expectedValue = v
			}
		}
		stale, kind, expected := staleAfter, matchKind, expectedValue
		mu.Unlock()

		w.Header().Set("ETag", `"monitor"`)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"topic_monitor": map[string]interface{}{
			"id": "monitor-uuid", "mqtt_broker_id": "broker-uuid",
			"topic_filter":        "sensors/+/temperature",
			"stale_after_seconds": stale, "match_kind": kind,
			"expected_value": expected, "json_key": nil,
			"capture_payload": true, "effective_capture_payload": true,
			"freshness_check": stale != nil, "payload_check": kind != nil,
			"exact_topic": false, "subscribed_since": nil, "capped": false,
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}})
	}
}

// The derived booleans are Computed and change whenever a check does. If the
// provider carried a stale one into the plan, Terraform would abort the second
// step with "inconsistent result after apply" on freshness_check.
func TestMQTTTopicMonitorPlan_DerivedFlagsFollowTheChecks(t *testing.T) {
	mqttPlanTest(t, topicMonitorHandler())

	freshnessOnly := providerConfig + `
resource "fivenines_mqtt_topic_monitor" "test" {
  mqtt_broker_id      = "broker-uuid"
  topic_filter        = "sensors/+/temperature"
  stale_after_seconds = 300
}`
	bothChecks := providerConfig + `
resource "fivenines_mqtt_topic_monitor" "test" {
  mqtt_broker_id      = "broker-uuid"
  topic_filter        = "sensors/+/temperature"
  stale_after_seconds = 300
  match_kind          = "exact"
  expected_value      = "online"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: freshnessOnly,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "freshness_check", "true"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "payload_check", "false"),
				),
			},
			{
				Config: bothChecks,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "payload_check", "true"),
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "match_kind", "exact"),
				),
			},
			{Config: bothChecks, PlanOnly: true},
			{
				// Back to freshness only: the payload check has to actually clear,
				// which is the `json:"match_kind"` (no omitempty) half of the tag
				// convention observed end to end.
				Config: freshnessOnly,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "payload_check", "false"),
					resource.TestCheckNoResourceAttr("fivenines_mqtt_topic_monitor.test", "match_kind"),
				),
			},
		},
	})
}

// A monitor is addressable only under its broker: the API has no route that
// takes a monitor id alone, so moving one to a different broker cannot be a
// PATCH — it has to be a destroy and a create. Without RequiresReplace on
// mqtt_broker_id the update would PATCH the new broker's path with the old
// monitor's id and 404, and the unit tests cannot see it because they never run
// plan validation.
func TestMQTTTopicMonitorPlan_ChangingBrokerForcesReplacement(t *testing.T) {
	var deletes int32
	mqttPlanTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deletes, 1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPatch {
			t.Errorf("a monitor must never be PATCHed onto a different broker, got %s %s", r.Method, r.URL.Path)
		}
		brokerID := "broker-a"
		if strings.Contains(r.URL.Path, "broker-b") {
			brokerID = "broker-b"
		}
		w.Header().Set("ETag", `"monitor"`)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"topic_monitor": map[string]interface{}{
			"id": "monitor-uuid", "mqtt_broker_id": brokerID,
			"topic_filter": "sensors/+/temperature", "stale_after_seconds": 300,
			"match_kind": nil, "expected_value": nil, "json_key": nil,
			"capture_payload": true, "effective_capture_payload": true,
			"freshness_check": true, "payload_check": false, "exact_topic": false,
			"subscribed_since": nil, "capped": false,
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}})
	})

	cfg := func(broker string) string {
		return providerConfig + `
resource "fivenines_mqtt_topic_monitor" "test" {
  mqtt_broker_id      = "` + broker + `"
  topic_filter        = "sensors/+/temperature"
  stale_after_seconds = 300
}`
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("broker-a"),
				Check:  resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "mqtt_broker_id", "broker-a"),
			},
			{
				Config: cfg("broker-b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_mqtt_topic_monitor.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr("fivenines_mqtt_topic_monitor.test", "mqtt_broker_id", "broker-b"),
			},
		},
	})

	// The replace has to actually tear the old monitor down, or the practitioner
	// keeps paying for a monitor Terraform has forgotten.
	if got := atomic.LoadInt32(&deletes); got == 0 {
		t.Error("expected the replaced monitor to be destroyed")
	}
}
