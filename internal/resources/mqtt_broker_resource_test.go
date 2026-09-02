package resources

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func mqttBrokerSchema(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewMQTTBrokerResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func newMQTTBrokerResource(t *testing.T, handler http.HandlerFunc) *mqttBrokerResource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &mqttBrokerResource{client: client.NewClient(srv.URL, "test-api-key")}
}

func mqttBrokerJSON(overrides map[string]interface{}) map[string]interface{} {
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

// --- mapMQTTBrokerToState ---

func TestMapMQTTBrokerToState(t *testing.T) {
	watcher := "host-uuid"
	lastError := "Connection refused: not authorised"
	lastConnected := "2026-01-01T00:00:00Z"
	lastSynced := "2026-01-01T00:05:00Z"
	broker := &client.MQTTBroker{
		ID: "broker-uuid", Name: "Factory-floor Mosquitto", Host: "mqtt.internal",
		Port: 8883, TLS: true, UsernameSet: true, PasswordSet: true,
		WatcherHostID: &watcher, Status: "config_error", LastErrorMessage: &lastError,
		LastConnectedAt: &lastConnected, LastSyncedAt: &lastSynced,
		Stale: true, TopicMonitorCount: 300,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
	}

	// The configured credentials are already in state; the mapping must leave
	// them alone, because the API never returns either one.
	state := &mqttBrokerModel{
		Username: types.StringValue("factory"),
		Password: types.StringValue("s3cret"),
	}
	mapMQTTBrokerToState(broker, state)

	if state.ID.ValueString() != "broker-uuid" {
		t.Errorf("expected ID broker-uuid, got %s", state.ID.ValueString())
	}
	if state.Name.ValueString() != "Factory-floor Mosquitto" {
		t.Errorf("expected the name, got %s", state.Name.ValueString())
	}
	if state.Host.ValueString() != "mqtt.internal" {
		t.Errorf("expected host mqtt.internal, got %s", state.Host.ValueString())
	}
	if state.Port.ValueInt64() != 8883 {
		t.Errorf("expected port 8883, got %d", state.Port.ValueInt64())
	}
	if !state.TLS.ValueBool() {
		t.Error("expected tls true")
	}
	if state.Username.ValueString() != "factory" || state.Password.ValueString() != "s3cret" {
		t.Error("expected write-only credentials to be preserved from config")
	}
	if !state.UsernameSet.ValueBool() || !state.PasswordSet.ValueBool() {
		t.Error("expected username_set and password_set true")
	}
	if state.WatcherHostID.ValueString() != "host-uuid" {
		t.Errorf("expected watcher_host_id host-uuid, got %s", state.WatcherHostID.ValueString())
	}
	if state.Status.ValueString() != "config_error" {
		t.Errorf("expected status config_error, got %s", state.Status.ValueString())
	}
	if !state.Stale.ValueBool() {
		t.Error("expected stale true")
	}
	if state.TopicMonitorCount.ValueInt64() != 300 {
		t.Errorf("expected topic_monitor_count 300, got %d", state.TopicMonitorCount.ValueInt64())
	}
	if state.LastErrorMessage.ValueString() != lastError {
		t.Errorf("expected last_error_message %q, got %q", lastError, state.LastErrorMessage.ValueString())
	}
	if state.LastConnectedAt.ValueString() != lastConnected {
		t.Errorf("expected last_connected_at %q, got %q", lastConnected, state.LastConnectedAt.ValueString())
	}
	if state.LastSyncedAt.ValueString() != lastSynced {
		t.Errorf("expected last_synced_at %q, got %q", lastSynced, state.LastSyncedAt.ValueString())
	}
	if state.UpdatedAt.ValueString() != "2026-01-02T00:00:00Z" {
		t.Errorf("expected updated_at to be mapped, got %s", state.UpdatedAt.ValueString())
	}
}

// An API null has to become a Terraform null, not "": an Optional-only attribute
// that reads back "" drifts on every plan.
func TestMapMQTTBrokerToState_Unassigned(t *testing.T) {
	broker := &client.MQTTBroker{
		ID: "broker-uuid", Name: "Unassigned", Host: "mqtt.internal", Port: 1883,
		Status: "unknown", Stale: true,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}

	state := &mqttBrokerModel{}
	mapMQTTBrokerToState(broker, state)

	if !state.WatcherHostID.IsNull() {
		t.Errorf("expected watcher_host_id to be null, got %q", state.WatcherHostID.ValueString())
	}
	if !state.LastErrorMessage.IsNull() || !state.LastConnectedAt.IsNull() || !state.LastSyncedAt.IsNull() {
		t.Error("expected the never-reported fields to be null")
	}
	if state.UsernameSet.ValueBool() || state.PasswordSet.ValueBool() {
		t.Error("expected username_set and password_set false for an anonymous broker")
	}
	if state.TopicMonitorCount.ValueInt64() != 0 {
		t.Errorf("expected topic_monitor_count 0, got %d", state.TopicMonitorCount.ValueInt64())
	}
}

// --- Schema ---

func TestMQTTBrokerSchema_CredentialsAreSensitiveAndRejectBlank(t *testing.T) {
	ctx := context.Background()
	s := mqttBrokerSchema(t)

	for _, name := range []string{"username", "password"} {
		attr, ok := s.Attributes[name].(rschema.StringAttribute)
		if !ok {
			t.Fatalf("%s: expected a StringAttribute, got %T", name, s.Attributes[name])
		}
		if !attr.Sensitive {
			t.Errorf("%s must be Sensitive — it is a broker credential", name)
		}
		if attr.Computed {
			t.Errorf("%s must not be Computed: the API never returns it, so it would stay unknown forever", name)
		}
		if len(attr.Validators) == 0 {
			t.Fatalf("%s must reject the empty string: the API reads \"\" as \"keep\", which would "+
				"leave state claiming \"\" while the server still holds a credential", name)
		}

		resp := &validator.StringResponse{}
		attr.Validators[0].ValidateString(ctx, validator.StringRequest{
			Path:        path.Root(name),
			ConfigValue: types.StringValue(""),
		}, resp)
		if !resp.Diagnostics.HasError() {
			t.Errorf("%s: expected an empty string to be rejected at plan time", name)
		}
	}
}

// --- Create ---

func mqttBrokerCreate(t *testing.T, handler http.HandlerFunc, config map[string]tftypes.Value) (*resource.CreateResponse, rschema.Schema) {
	t.Helper()
	ctx := context.Background()
	s := mqttBrokerSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTBrokerResource(t, handler)

	raw := nullObjectValue(t, objType, config)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: raw},
		Config: tfsdk.Config{Schema: s, Raw: raw},
	}, resp)
	return resp, s
}

func TestMQTTBrokerCreate_SendsConfiguredCredentials(t *testing.T) {
	var body map[string]map[string]interface{}
	resp, _ := mqttBrokerCreate(t, func(w http.ResponseWriter, req *http.Request) {
		json.NewDecoder(req.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(mqttBrokerJSON(nil))
	}, map[string]tftypes.Value{
		"name":            tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host":            tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port":            tftypes.NewValue(tftypes.Number, 8883),
		"tls":             tftypes.NewValue(tftypes.Bool, true),
		"username":        tftypes.NewValue(tftypes.String, "factory"),
		"password":        tftypes.NewValue(tftypes.String, "s3cret"),
		"watcher_host_id": tftypes.NewValue(tftypes.String, "host-uuid"),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent := body["mqtt_broker"]
	if sent["username"] != "factory" || sent["password"] != "s3cret" {
		t.Errorf("create must carry the configured credentials, got %v / %v", sent["username"], sent["password"])
	}
	if sent["port"] != float64(8883) || sent["tls"] != true {
		t.Errorf("create must carry port and tls, got %v / %v", sent["port"], sent["tls"])
	}
	if sent["watcher_host_id"] != "host-uuid" {
		t.Errorf("create must carry the watcher, got %v", sent["watcher_host_id"])
	}
}

// Nothing exists to clear yet, so an anonymous broker sends no credential keys —
// and `tls: null` is a documented 400, so an unconfigured tls must not be nulled
// either (the schema default means it never is).
func TestMQTTBrokerCreate_OmitsAbsentOptionals(t *testing.T) {
	var body map[string]map[string]interface{}
	resp, _ := mqttBrokerCreate(t, func(w http.ResponseWriter, req *http.Request) {
		json.NewDecoder(req.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(mqttBrokerJSON(map[string]interface{}{
			"username_set": false, "password_set": false, "watcher_host_id": nil,
		}))
	}, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Lab broker"),
		"host": tftypes.NewValue(tftypes.String, "10.0.4.20"),
		// port and tls carry schema defaults, so the plan holds them even here.
		"port": tftypes.NewValue(tftypes.Number, 1883),
		"tls":  tftypes.NewValue(tftypes.Bool, false),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent := body["mqtt_broker"]
	for _, key := range []string{"username", "password", "watcher_host_id"} {
		if _, present := sent[key]; present {
			t.Errorf("create must omit an absent %s, got %v", key, sent[key])
		}
	}
}

func TestMQTTBrokerCreate_SurfacesFeatureGate(t *testing.T) {
	resp, _ := mqttBrokerCreate(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "MQTT monitoring is not available for your organization",
		})
	}, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host": tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port": tftypes.NewValue(tftypes.Number, 1883),
		"tls":  tftypes.NewValue(tftypes.Bool, false),
	})

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when the feature is not enabled")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "enabled per organization") {
		t.Errorf("expected the entitlement hint, got %q", resp.Diagnostics.Errors()[0].Detail())
	}
}

// --- Update ---

// mqttBrokerUpdate drives the real Update. state carries what a prior apply
// recorded; config/plan carry what the practitioner has now.
func mqttBrokerUpdate(t *testing.T, handler http.HandlerFunc, plan, state map[string]tftypes.Value) *resource.UpdateResponse {
	t.Helper()
	ctx := context.Background()
	s := mqttBrokerSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTBrokerResource(t, handler)

	planRaw := nullObjectValue(t, objType, plan)
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: nullObjectValue(t, objType, state)}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: planRaw},
		State:  tfsdk.State{Schema: s, Raw: nullObjectValue(t, objType, state)},
		Config: tfsdk.Config{Schema: s, Raw: planRaw},
	}, resp)
	return resp
}

// THE case the #31 tag convention exists for. A broker imported with
// credentials has null on both sides here — Terraform has never seen either
// one. An unrelated port change must not wipe them, and since the API never
// returns a credential, a wipe would be invisible until the subscription broke.
func TestMQTTBrokerUpdate_OmitsCredentialsTerraformNeverKnew(t *testing.T) {
	var body map[string]map[string]interface{}
	resp := mqttBrokerUpdate(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			w.Header().Set("ETag", `"broker-etag"`)
			json.NewEncoder(w).Encode(mqttBrokerJSON(nil))
			return
		}
		json.NewDecoder(req.Body).Decode(&body)
		json.NewEncoder(w).Encode(mqttBrokerJSON(map[string]interface{}{"port": 8883}))
	}, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, "broker-uuid"),
		"name": tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host": tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port": tftypes.NewValue(tftypes.Number, 8883),
		"tls":  tftypes.NewValue(tftypes.Bool, false),
	}, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, "broker-uuid"),
		"name": tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host": tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port": tftypes.NewValue(tftypes.Number, 1883),
		"tls":  tftypes.NewValue(tftypes.Bool, false),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent := body["mqtt_broker"]
	for _, key := range []string{"username", "password"} {
		if _, present := sent[key]; present {
			t.Errorf("an unrelated update must not mention %s at all, got %v", key, sent[key])
		}
	}
	if sent["port"] != float64(8883) {
		t.Errorf("the update itself must still be sent, got port %v", sent["port"])
	}
}

func TestMQTTBrokerUpdate_SendsRotatedCredential(t *testing.T) {
	var body map[string]map[string]interface{}
	resp := mqttBrokerUpdate(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			w.Header().Set("ETag", `"broker-etag"`)
			json.NewEncoder(w).Encode(mqttBrokerJSON(nil))
			return
		}
		json.NewDecoder(req.Body).Decode(&body)
		json.NewEncoder(w).Encode(mqttBrokerJSON(nil))
	}, map[string]tftypes.Value{
		"id":       tftypes.NewValue(tftypes.String, "broker-uuid"),
		"name":     tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host":     tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port":     tftypes.NewValue(tftypes.Number, 1883),
		"tls":      tftypes.NewValue(tftypes.Bool, false),
		"password": tftypes.NewValue(tftypes.String, "rotated"),
	}, map[string]tftypes.Value{
		"id":       tftypes.NewValue(tftypes.String, "broker-uuid"),
		"name":     tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host":     tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port":     tftypes.NewValue(tftypes.Number, 1883),
		"tls":      tftypes.NewValue(tftypes.Bool, false),
		"password": tftypes.NewValue(tftypes.String, "old"),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if body["mqtt_broker"]["password"] != "rotated" {
		t.Errorf("a changed credential must be sent, got %v", body["mqtt_broker"]["password"])
	}
}

// watcher_host_id is the opposite call: readable and Optional-only, so dropping
// it has to unassign the watcher, which only an explicit null does.
func TestMQTTBrokerUpdate_DroppedWatcherHostSendsNull(t *testing.T) {
	var body map[string]map[string]interface{}
	resp := mqttBrokerUpdate(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			w.Header().Set("ETag", `"broker-etag"`)
			json.NewEncoder(w).Encode(mqttBrokerJSON(nil))
			return
		}
		json.NewDecoder(req.Body).Decode(&body)
		json.NewEncoder(w).Encode(mqttBrokerJSON(map[string]interface{}{"watcher_host_id": nil}))
	}, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, "broker-uuid"),
		"name": tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host": tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port": tftypes.NewValue(tftypes.Number, 1883),
		"tls":  tftypes.NewValue(tftypes.Bool, false),
	}, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, "broker-uuid"),
		"name":            tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host":            tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port":            tftypes.NewValue(tftypes.Number, 1883),
		"tls":             tftypes.NewValue(tftypes.Bool, false),
		"watcher_host_id": tftypes.NewValue(tftypes.String, "host-uuid"),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	v, present := body["mqtt_broker"]["watcher_host_id"]
	if !present || v != nil {
		t.Errorf("expected an explicit null watcher_host_id, got %v (present=%v)", v, present)
	}
}

func TestMQTTBrokerUpdate_RetriesOnETagMismatch(t *testing.T) {
	var patches int32
	resp := mqttBrokerUpdate(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			w.Header().Set("ETag", `"broker-etag"`)
			json.NewEncoder(w).Encode(mqttBrokerJSON(nil))
			return
		}
		if atomic.AddInt32(&patches, 1) == 1 {
			w.WriteHeader(http.StatusPreconditionFailed)
			json.NewEncoder(w).Encode(map[string]string{"error": "stale"})
			return
		}
		json.NewEncoder(w).Encode(mqttBrokerJSON(nil))
	}, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, "broker-uuid"),
		"name": tftypes.NewValue(tftypes.String, "Renamed"),
		"host": tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port": tftypes.NewValue(tftypes.Number, 1883),
		"tls":  tftypes.NewValue(tftypes.Bool, false),
	}, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, "broker-uuid"),
		"name": tftypes.NewValue(tftypes.String, "Factory-floor Mosquitto"),
		"host": tftypes.NewValue(tftypes.String, "mqtt.internal"),
		"port": tftypes.NewValue(tftypes.Number, 1883),
		"tls":  tftypes.NewValue(tftypes.Bool, false),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("expected the retry to succeed, got %v", resp.Diagnostics.Errors())
	}
	if got := atomic.LoadInt32(&patches); got != 2 {
		t.Errorf("expected 2 PATCH attempts, got %d", got)
	}
}

// --- Read and Delete ---

func TestMQTTBrokerRead_RemovesVanishedBroker(t *testing.T) {
	ctx := context.Background()
	s := mqttBrokerSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTBrokerResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "broker-uuid"),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: state}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a 404 is not an error on read: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Error("expected the resource to be removed from state")
	}
}

func TestMQTTBrokerRead_KeepsStateOnServerError(t *testing.T) {
	ctx := context.Background()
	s := mqttBrokerSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTBrokerResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "broker-uuid"),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: state}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a 500 to surface rather than silently dropping the resource")
	}
	if resp.State.Raw.IsNull() {
		t.Error("a server error must not remove the resource from state")
	}
}

func TestMQTTBrokerDelete_ToleratesAlreadyDeleted(t *testing.T) {
	ctx := context.Background()
	s := mqttBrokerSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTBrokerResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	})

	resp := &resource.DeleteResponse{}
	r.Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: nullObjectValue(t, objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "broker-uuid"),
	})}}, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("a broker already gone is a successful delete, got %v", resp.Diagnostics.Errors())
	}
}

func TestMQTTBrokerDelete_SurfacesServerError(t *testing.T) {
	ctx := context.Background()
	s := mqttBrokerSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTBrokerResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
	})

	resp := &resource.DeleteResponse{}
	r.Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: nullObjectValue(t, objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "broker-uuid"),
	})}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Error("expected a failed delete to surface")
	}
}

// --- mqttErrorDetail ---

func TestMQTTErrorDetail(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"feature gate", &client.APIError{StatusCode: http.StatusForbidden, Message: "nope"}, "enabled per organization"},
		{"billing", &client.APIError{StatusCode: http.StatusPaymentRequired, Message: "nope"}, "Settle billing"},
		{"validation passes through", &client.APIError{StatusCode: http.StatusUnprocessableEntity, Message: "Monitor limit reached for your plan"}, "Monitor limit reached for your plan"},
		{"transport error", errors.New("dial tcp: connection refused"), "connection refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mqttErrorDetail(tc.err); !strings.Contains(got, tc.want) {
				t.Errorf("expected %q in the detail, got %q", tc.want, got)
			}
		})
	}
}

// A 422 must not pick up the entitlement hint: the API's own wording already
// names the real problem, and a plan-limit story on a name clash misleads.
func TestMQTTErrorDetail_DoesNotGuessOnValidationErrors(t *testing.T) {
	got := mqttErrorDetail(&client.APIError{
		StatusCode: http.StatusUnprocessableEntity,
		Errors:     []string{"Name has already been taken"},
	})
	if strings.Contains(got, "enabled per organization") || strings.Contains(got, "Settle billing") {
		t.Errorf("a 422 must carry only the API's own message, got %q", got)
	}
}
