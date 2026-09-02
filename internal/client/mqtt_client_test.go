package client

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
)

// --- MQTT Brokers ---

// brokerResponse is the read shape the API renders back: no username or
// password, only whether one is stored.
func brokerResponse(overrides map[string]interface{}) map[string]interface{} {
	broker := map[string]interface{}{
		"id": "broker-uuid", "name": "Factory-floor Mosquitto", "host": "mqtt.internal",
		"port": 1883, "tls": false, "username_set": true, "password_set": true,
		"watcher_host_id": "host-uuid", "status": "connected", "stale": false,
		"topic_monitor_count": 300,
		"created_at":          "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
	}
	for k, v := range overrides {
		broker[k] = v
	}
	return map[string]interface{}{"mqtt_broker": broker}
}

func TestClient_CreateMQTTBroker(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/mqtt_brokers" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(brokerResponse(nil))
	})

	port, tls := 8883, true
	username, password, watcher := "factory", "s3cret", "host-uuid"
	broker, err := c.CreateMQTTBroker(context.Background(), CreateMQTTBrokerInput{
		Name: "Factory-floor Mosquitto", Host: "mqtt.internal",
		Port: &port, TLS: &tls,
		Username: &username, Password: &password, WatcherHostID: &watcher,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.ID != "broker-uuid" {
		t.Errorf("expected ID broker-uuid, got %s", broker.ID)
	}
	if !broker.UsernameSet || !broker.PasswordSet {
		t.Error("expected username_set and password_set to be true")
	}
	if broker.TopicMonitorCount != 300 {
		t.Errorf("expected topic_monitor_count 300, got %d", broker.TopicMonitorCount)
	}
	body := gotBody["mqtt_broker"]
	if body["username"] != "factory" || body["password"] != "s3cret" {
		t.Errorf("expected the credentials on the wire, got %v / %v", body["username"], body["password"])
	}
	if body["port"] != float64(8883) || body["tls"] != true {
		t.Errorf("expected port 8883 and tls true, got %v / %v", body["port"], body["tls"])
	}
}

// Nothing is stored on a broker that does not exist yet, so an absent optional
// is omitted rather than nulled — and `tls: null` is a documented 400.
func TestClient_CreateMQTTBroker_OmitsAbsentOptionals(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(brokerResponse(nil))
	})

	if _, err := c.CreateMQTTBroker(context.Background(), CreateMQTTBrokerInput{
		Name: "Lab broker", Host: "10.0.4.20",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"port", "tls", "username", "password", "watcher_host_id"} {
		if _, present := gotBody["mqtt_broker"][key]; present {
			t.Errorf("expected %s to be omitted on create, got %v", key, gotBody["mqtt_broker"][key])
		}
	}
}

// The credential half of the #31 tag convention: a credential the caller did not
// set must not reach the wire at all. The value is unreadable, so an explicit
// null on an unrelated edit would wipe a working credential with nothing in the
// plan to show for it.
func TestClient_UpdateMQTTBroker_OmitsUnsetCredentials(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(brokerResponse(nil))
	})

	name, host, watcher := "Factory-floor Mosquitto", "mqtt.internal", "host-uuid"
	port := 8883
	if _, err := c.UpdateMQTTBroker(context.Background(), "broker-uuid", "", UpdateMQTTBrokerInput{
		Name: &name, Host: &host, Port: &port, WatcherHostID: &watcher,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"username", "password"} {
		if _, present := gotBody["mqtt_broker"][key]; present {
			t.Errorf("expected %s to be omitted when unset, got %v", key, gotBody["mqtt_broker"][key])
		}
	}
	if gotBody["mqtt_broker"]["port"] != float64(8883) {
		t.Errorf("expected the rest of the update to still be sent, got port %v", gotBody["mqtt_broker"]["port"])
	}
}

func TestClient_UpdateMQTTBroker_SendsRotatedCredentials(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	var gotIfMatch string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotIfMatch = r.Header.Get("If-Match")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(brokerResponse(nil))
	})

	name, host := "Factory-floor Mosquitto", "mqtt.internal"
	username, password := "rotated", "rotated-secret"
	if _, err := c.UpdateMQTTBroker(context.Background(), "broker-uuid", `"broker-etag"`, UpdateMQTTBrokerInput{
		Name: &name, Host: &host, Username: &username, Password: &password,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIfMatch != `"broker-etag"` {
		t.Errorf("expected If-Match %q, got %q", `"broker-etag"`, gotIfMatch)
	}
	if gotBody["mqtt_broker"]["username"] != "rotated" || gotBody["mqtt_broker"]["password"] != "rotated-secret" {
		t.Errorf("expected the rotated credentials on the wire, got %v / %v",
			gotBody["mqtt_broker"]["username"], gotBody["mqtt_broker"]["password"])
	}
}

// watcher_host_id is readable and Optional-only, so it is the opposite call from
// the credentials: nil has to marshal as null, or dropping it from the
// configuration would leave the broker collecting after Terraform says stop.
func TestClient_UpdateMQTTBroker_NilWatcherHostSendsNull(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(brokerResponse(map[string]interface{}{"watcher_host_id": nil}))
	})

	name, host := "Factory-floor Mosquitto", "mqtt.internal"
	broker, err := c.UpdateMQTTBroker(context.Background(), "broker-uuid", "", UpdateMQTTBrokerInput{
		Name: &name, Host: &host,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, present := gotBody["mqtt_broker"]["watcher_host_id"]; !present || v != nil {
		t.Errorf("expected explicit null watcher_host_id, got %v (present=%v)", v, present)
	}
	if broker.WatcherHostID != nil {
		t.Errorf("expected nil watcher_host_id, got %v", *broker.WatcherHostID)
	}
}

func TestClient_GetMQTTBroker(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mqtt_brokers/broker-uuid" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"broker-etag"`)
		json.NewEncoder(w).Encode(brokerResponse(map[string]interface{}{
			"status": "config_error", "stale": true,
			"last_error_message": "Connection refused: not authorised",
			"last_connected_at":  "2026-01-01T00:00:00Z",
			"last_synced_at":     "2026-01-01T00:05:00Z",
		}))
	})

	broker, etag, err := c.GetMQTTBroker(context.Background(), "broker-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"broker-etag"` {
		t.Errorf("expected etag %q, got %q", `"broker-etag"`, etag)
	}
	if broker.Status != "config_error" {
		t.Errorf("expected status config_error, got %s", broker.Status)
	}
	if !broker.Stale {
		t.Error("expected stale true")
	}
	if broker.LastErrorMessage == nil || *broker.LastErrorMessage != "Connection refused: not authorised" {
		t.Errorf("expected last_error_message, got %v", broker.LastErrorMessage)
	}
	if broker.LastSyncedAt == nil || *broker.LastSyncedAt != "2026-01-01T00:05:00Z" {
		t.Errorf("expected last_synced_at, got %v", broker.LastSyncedAt)
	}
}

// A fleet broker carries hundreds of monitors and the index pages at 100, so a
// walk that stops after page one silently truncates the resource's view.
func TestClient_ListMQTTBrokers_Pagination(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := int(atomic.AddInt32(&requestCount, 1))
		id := "a"
		if page != 1 {
			id = "b"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"mqtt_brokers": []map[string]interface{}{{"id": id, "name": id, "host": "mqtt.internal"}},
			"meta": map[string]int{
				"current_page": page, "total_pages": 2, "total_count": 2, "per_page": 100,
			},
		})
	})

	brokers, err := c.ListMQTTBrokers(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(brokers) != 2 {
		t.Fatalf("expected 2 brokers, got %d", len(brokers))
	}
	if brokers[0].ID != "a" || brokers[1].ID != "b" {
		t.Errorf("expected both pages in order, got %s and %s", brokers[0].ID, brokers[1].ID)
	}
}

func TestClient_DeleteMQTTBroker(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/mqtt_brokers/broker-uuid" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.DeleteMQTTBroker(context.Background(), "broker-uuid"); err != nil {
		t.Fatalf("expected no error for 204, got: %v", err)
	}
}

// MQTT is a gated feature, so a 403 is an org entitlement rather than a bad key.
func TestClient_CreateMQTTBroker_FeatureDisabled_403(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "MQTT monitoring is not available for your organization",
		})
	})

	_, err := c.CreateMQTTBroker(context.Background(), CreateMQTTBrokerInput{Name: "b", Host: "h"})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "MQTT monitoring is not available for your organization" {
		t.Errorf("unexpected message: %s", apiErr.Message)
	}
}

// --- MQTT Topic Monitors ---

func topicMonitorResponse(overrides map[string]interface{}) map[string]interface{} {
	monitor := map[string]interface{}{
		"id": "monitor-uuid", "mqtt_broker_id": "broker-uuid",
		"topic_filter": "sensors/+/temperature", "stale_after_seconds": 300,
		"match_kind": nil, "expected_value": nil, "json_key": nil,
		"capture_payload": true, "effective_capture_payload": true,
		"freshness_check": true, "payload_check": false, "exact_topic": false,
		"subscribed_since": nil, "capped": false,
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
	}
	for k, v := range overrides {
		monitor[k] = v
	}
	return map[string]interface{}{"topic_monitor": monitor}
}

func TestClient_CreateMQTTTopicMonitor(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/mqtt_brokers/broker-uuid/topic_monitors" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(topicMonitorResponse(nil))
	})

	stale := int64(300)
	capture := true
	monitor, err := c.CreateMQTTTopicMonitor(context.Background(), "broker-uuid", CreateMQTTTopicMonitorInput{
		TopicFilter: "sensors/+/temperature", StaleAfterSeconds: &stale, CapturePayload: &capture,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if monitor.ID != "monitor-uuid" {
		t.Errorf("expected ID monitor-uuid, got %s", monitor.ID)
	}
	if !monitor.FreshnessCheck || monitor.PayloadCheck {
		t.Error("expected a freshness check and no payload check")
	}
	if got := gotBody["topic_monitor"]["stale_after_seconds"]; got != float64(300) {
		t.Errorf("expected stale_after_seconds 300, got %v", got)
	}
	// A monitor with no payload expectation must not carry null keys on create:
	// nothing exists to clear yet.
	for _, key := range []string{"match_kind", "expected_value", "json_key"} {
		if _, present := gotBody["topic_monitor"][key]; present {
			t.Errorf("expected %s to be omitted on create, got %v", key, gotBody["topic_monitor"][key])
		}
	}
}

func TestClient_GetMQTTTopicMonitor(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mqtt_brokers/broker-uuid/topic_monitors/monitor-uuid" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"monitor-etag"`)
		json.NewEncoder(w).Encode(topicMonitorResponse(map[string]interface{}{
			"topic_filter": "devices/pump-1/status", "stale_after_seconds": nil,
			"match_kind": "exact", "expected_value": "online",
			"capture_payload": false, "effective_capture_payload": true,
			"freshness_check": false, "payload_check": true, "exact_topic": true,
			"subscribed_since": "2026-01-01T00:00:00Z",
		}))
	})

	monitor, etag, err := c.GetMQTTTopicMonitor(context.Background(), "broker-uuid", "monitor-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"monitor-etag"` {
		t.Errorf("expected etag %q, got %q", `"monitor-etag"`, etag)
	}
	if monitor.StaleAfterSeconds != nil {
		t.Errorf("expected nil stale_after_seconds, got %d", *monitor.StaleAfterSeconds)
	}
	if monitor.MatchKind == nil || *monitor.MatchKind != "exact" {
		t.Errorf("expected match_kind exact, got %v", monitor.MatchKind)
	}
	// A payload expectation forces capture on, whatever the stored flag says.
	if monitor.CapturePayload || !monitor.EffectiveCapturePayload {
		t.Error("expected capture_payload false and effective_capture_payload true")
	}
	if !monitor.ExactTopic {
		t.Error("expected exact_topic true for a wildcard-free filter")
	}
	if monitor.SubscribedSince == nil {
		t.Error("expected subscribed_since to be carried through")
	}
}

// Dropping a check has to reach the API as an explicit null; an omitted key
// would leave the old expectation in place, and the monitor would keep firing on
// a payload the configuration no longer mentions.
func TestClient_UpdateMQTTTopicMonitor_NullClearsPayloadCheck(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/mqtt_brokers/broker-uuid/topic_monitors/monitor-uuid" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(topicMonitorResponse(nil))
	})

	filter := "sensors/+/temperature"
	stale := int64(300)
	capture := true
	if _, err := c.UpdateMQTTTopicMonitor(context.Background(), "broker-uuid", "monitor-uuid", "",
		UpdateMQTTTopicMonitorInput{
			TopicFilter: &filter, StaleAfterSeconds: &stale, CapturePayload: &capture,
		}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"match_kind", "expected_value", "json_key"} {
		v, present := gotBody["topic_monitor"][key]
		if !present || v != nil {
			t.Errorf("expected explicit null %s, got %v (present=%v)", key, v, present)
		}
	}
	// capture_payload is NOT NULL server-side, so it must never be nulled.
	if gotBody["topic_monitor"]["capture_payload"] != true {
		t.Errorf("expected capture_payload true, got %v", gotBody["topic_monitor"]["capture_payload"])
	}
}

// The other direction: dropping the freshness timeout has to clear it too.
func TestClient_UpdateMQTTTopicMonitor_NullClearsFreshnessCheck(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(topicMonitorResponse(nil))
	})

	filter, kind, expected := "devices/pump-1/status", "exact", "online"
	capture := true
	if _, err := c.UpdateMQTTTopicMonitor(context.Background(), "broker-uuid", "monitor-uuid", "",
		UpdateMQTTTopicMonitorInput{
			TopicFilter: &filter, MatchKind: &kind, ExpectedValue: &expected, CapturePayload: &capture,
		}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, present := gotBody["topic_monitor"]["stale_after_seconds"]
	if !present || v != nil {
		t.Errorf("expected explicit null stale_after_seconds, got %v (present=%v)", v, present)
	}
}

func TestClient_ListMQTTTopicMonitors_Pagination(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := int(atomic.AddInt32(&requestCount, 1))
		if r.URL.Path != "/api/v1/mqtt_brokers/broker-uuid/topic_monitors" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"topic_monitors": []map[string]interface{}{
				{"id": "m", "mqtt_broker_id": "broker-uuid", "topic_filter": "sensors/#"},
			},
			"meta": map[string]int{
				"current_page": page, "total_pages": 3, "total_count": 3, "per_page": 100,
			},
		})
	})

	monitors, err := c.ListMQTTTopicMonitors(context.Background(), "broker-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Generated fleets run to hundreds of monitors: stopping early is how a
	// resource silently loses the tail of the list.
	if len(monitors) != 3 {
		t.Errorf("expected 3 monitors across 3 pages, got %d", len(monitors))
	}
}

func TestClient_DeleteMQTTTopicMonitor(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/mqtt_brokers/broker-uuid/topic_monitors/monitor-uuid" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.DeleteMQTTTopicMonitor(context.Background(), "broker-uuid", "monitor-uuid"); err != nil {
		t.Fatalf("expected no error for 204, got: %v", err)
	}
}

// Topic monitors are the billable unit, so a create at the plan limit is a 422.
func TestClient_CreateMQTTTopicMonitor_MonitorLimit_422(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": "Monitor limit reached for your plan"})
	})

	stale := int64(300)
	_, err := c.CreateMQTTTopicMonitor(context.Background(), "broker-uuid", CreateMQTTTopicMonitorInput{
		TopicFilter: "sensors/#", StaleAfterSeconds: &stale,
	})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "Monitor limit reached for your plan" {
		t.Errorf("unexpected message: %s", apiErr.Message)
	}
}
