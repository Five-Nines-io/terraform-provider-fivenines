package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func mqttTopicMonitorSchema(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewMQTTTopicMonitorResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func newMQTTTopicMonitorResource(t *testing.T, handler http.HandlerFunc) *mqttTopicMonitorResource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &mqttTopicMonitorResource{client: client.NewClient(srv.URL, "test-api-key")}
}

func mqttTopicMonitorJSON(overrides map[string]interface{}) map[string]interface{} {
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

// --- mapMQTTTopicMonitorToState ---

func TestMapMQTTTopicMonitorToState_Freshness(t *testing.T) {
	stale := int64(300)
	subscribed := "2026-01-01T00:00:00Z"
	monitor := &client.MQTTTopicMonitor{
		ID: "monitor-uuid", MQTTBrokerID: "broker-uuid",
		TopicFilter: "sensors/+/temperature", StaleAfterSeconds: &stale,
		CapturePayload: true, EffectiveCapturePayload: true,
		FreshnessCheck: true, SubscribedSince: &subscribed,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
	}

	state := &mqttTopicMonitorModel{}
	mapMQTTTopicMonitorToState(monitor, state)

	if state.ID.ValueString() != "monitor-uuid" {
		t.Errorf("expected ID monitor-uuid, got %s", state.ID.ValueString())
	}
	if state.MQTTBrokerID.ValueString() != "broker-uuid" {
		t.Errorf("expected mqtt_broker_id broker-uuid, got %s", state.MQTTBrokerID.ValueString())
	}
	if state.TopicFilter.ValueString() != "sensors/+/temperature" {
		t.Errorf("expected the topic filter, got %s", state.TopicFilter.ValueString())
	}
	if state.StaleAfterSeconds.ValueInt64() != 300 {
		t.Errorf("expected stale_after_seconds 300, got %d", state.StaleAfterSeconds.ValueInt64())
	}
	if !state.MatchKind.IsNull() || !state.ExpectedValue.IsNull() || !state.JSONKey.IsNull() {
		t.Error("expected the payload check attributes to be null")
	}
	if !state.FreshnessCheck.ValueBool() || state.PayloadCheck.ValueBool() {
		t.Error("expected freshness_check true and payload_check false")
	}
	if state.SubscribedSince.ValueString() != subscribed {
		t.Errorf("expected subscribed_since %s, got %s", subscribed, state.SubscribedSince.ValueString())
	}
	if state.UpdatedAt.ValueString() != "2026-01-02T00:00:00Z" {
		t.Errorf("expected updated_at to be mapped, got %s", state.UpdatedAt.ValueString())
	}
}

func TestMapMQTTTopicMonitorToState_PayloadForcesCapture(t *testing.T) {
	matchKind := "exact"
	expected := "online"
	monitor := &client.MQTTTopicMonitor{
		ID: "monitor-uuid", MQTTBrokerID: "broker-uuid", TopicFilter: "devices/pump-1/status",
		MatchKind: &matchKind, ExpectedValue: &expected,
		// Stored false, but a payload expectation forces capture on server-side.
		CapturePayload: false, EffectiveCapturePayload: true,
		PayloadCheck: true, ExactTopic: true, Capped: true,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}

	state := &mqttTopicMonitorModel{}
	mapMQTTTopicMonitorToState(monitor, state)

	if !state.StaleAfterSeconds.IsNull() {
		t.Errorf("expected stale_after_seconds to be null, got %d", state.StaleAfterSeconds.ValueInt64())
	}
	if state.MatchKind.ValueString() != "exact" || state.ExpectedValue.ValueString() != "online" {
		t.Error("expected the payload expectation to be mapped")
	}
	if state.CapturePayload.ValueBool() || !state.EffectiveCapturePayload.ValueBool() {
		t.Error("expected capture_payload false and effective_capture_payload true")
	}
	if !state.ExactTopic.ValueBool() {
		t.Error("expected exact_topic true for a wildcard-free filter")
	}
	if !state.Capped.ValueBool() {
		t.Error("expected capped to be mapped")
	}
	if !state.SubscribedSince.IsNull() {
		t.Error("expected subscribed_since to be null when the watcher holds no subscription")
	}
}

// --- Create ---

func mqttTopicMonitorCreate(t *testing.T, handler http.HandlerFunc, config map[string]tftypes.Value) *resource.CreateResponse {
	t.Helper()
	ctx := context.Background()
	s := mqttTopicMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTTopicMonitorResource(t, handler)

	raw := nullObjectValue(t, objType, config)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: raw},
		Config: tfsdk.Config{Schema: s, Raw: raw},
	}, resp)
	return resp
}

func TestMQTTTopicMonitorCreate_PostsUnderItsBroker(t *testing.T) {
	var gotPath string
	var body map[string]map[string]interface{}
	resp := mqttTopicMonitorCreate(t, func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		json.NewDecoder(req.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(mqttTopicMonitorJSON(nil))
	}, map[string]tftypes.Value{
		"mqtt_broker_id":      tftypes.NewValue(tftypes.String, "broker-uuid"),
		"topic_filter":        tftypes.NewValue(tftypes.String, "sensors/+/temperature"),
		"stale_after_seconds": tftypes.NewValue(tftypes.Number, 300),
		"capture_payload":     tftypes.NewValue(tftypes.Bool, true),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if gotPath != "/api/v1/mqtt_brokers/broker-uuid/topic_monitors" {
		t.Errorf("a monitor is addressable only under its broker, got %s", gotPath)
	}
	sent := body["topic_monitor"]
	if sent["stale_after_seconds"] != float64(300) {
		t.Errorf("expected stale_after_seconds 300, got %v", sent["stale_after_seconds"])
	}
	// Nothing exists to clear on create, so an absent payload check is omitted.
	for _, key := range []string{"match_kind", "expected_value", "json_key"} {
		if _, present := sent[key]; present {
			t.Errorf("create must omit an absent %s, got %v", key, sent[key])
		}
	}
}

func TestMQTTTopicMonitorCreate_SurfacesMonitorLimit(t *testing.T) {
	resp := mqttTopicMonitorCreate(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": "Monitor limit reached for your plan"})
	}, map[string]tftypes.Value{
		"mqtt_broker_id":      tftypes.NewValue(tftypes.String, "broker-uuid"),
		"topic_filter":        tftypes.NewValue(tftypes.String, "sensors/#"),
		"stale_after_seconds": tftypes.NewValue(tftypes.Number, 300),
		"capture_payload":     tftypes.NewValue(tftypes.Bool, true),
	})

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the plan limit to surface")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "Monitor limit reached") {
		t.Errorf("expected the API's own wording, got %q", resp.Diagnostics.Errors()[0].Detail())
	}
}

// --- Update ---

func mqttTopicMonitorUpdate(t *testing.T, handler http.HandlerFunc, plan, state map[string]tftypes.Value) *resource.UpdateResponse {
	t.Helper()
	ctx := context.Background()
	s := mqttTopicMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTTopicMonitorResource(t, handler)

	planRaw := nullObjectValue(t, objType, plan)
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: nullObjectValue(t, objType, state)}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: planRaw},
		State:  tfsdk.State{Schema: s, Raw: nullObjectValue(t, objType, state)},
		Config: tfsdk.Config{Schema: s, Raw: planRaw},
	}, resp)
	return resp
}

// Dropping the payload expectation has to clear it server-side. An omitted key
// leaves the monitor firing on an expectation the configuration no longer has.
func TestMQTTTopicMonitorUpdate_DroppedPayloadCheckSendsNull(t *testing.T) {
	var body map[string]map[string]interface{}
	resp := mqttTopicMonitorUpdate(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			w.Header().Set("ETag", `"monitor-etag"`)
			json.NewEncoder(w).Encode(mqttTopicMonitorJSON(nil))
			return
		}
		json.NewDecoder(req.Body).Decode(&body)
		json.NewEncoder(w).Encode(mqttTopicMonitorJSON(nil))
	}, map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, "monitor-uuid"),
		"mqtt_broker_id":      tftypes.NewValue(tftypes.String, "broker-uuid"),
		"topic_filter":        tftypes.NewValue(tftypes.String, "sensors/+/temperature"),
		"stale_after_seconds": tftypes.NewValue(tftypes.Number, 300),
		"capture_payload":     tftypes.NewValue(tftypes.Bool, true),
	}, map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, "monitor-uuid"),
		"mqtt_broker_id":      tftypes.NewValue(tftypes.String, "broker-uuid"),
		"topic_filter":        tftypes.NewValue(tftypes.String, "sensors/+/temperature"),
		"stale_after_seconds": tftypes.NewValue(tftypes.Number, 300),
		"match_kind":          tftypes.NewValue(tftypes.String, "exact"),
		"expected_value":      tftypes.NewValue(tftypes.String, "online"),
		"capture_payload":     tftypes.NewValue(tftypes.Bool, true),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	for _, key := range []string{"match_kind", "expected_value", "json_key"} {
		v, present := body["topic_monitor"][key]
		if !present || v != nil {
			t.Errorf("expected an explicit null %s, got %v (present=%v)", key, v, present)
		}
	}
	// NOT NULL server-side: nulling capture_payload is a documented 400.
	if body["topic_monitor"]["capture_payload"] != true {
		t.Errorf("expected capture_payload true, got %v", body["topic_monitor"]["capture_payload"])
	}
}

func TestMQTTTopicMonitorUpdate_DroppedFreshnessSendsNull(t *testing.T) {
	var body map[string]map[string]interface{}
	resp := mqttTopicMonitorUpdate(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			w.Header().Set("ETag", `"monitor-etag"`)
			json.NewEncoder(w).Encode(mqttTopicMonitorJSON(nil))
			return
		}
		json.NewDecoder(req.Body).Decode(&body)
		json.NewEncoder(w).Encode(mqttTopicMonitorJSON(nil))
	}, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, "monitor-uuid"),
		"mqtt_broker_id":  tftypes.NewValue(tftypes.String, "broker-uuid"),
		"topic_filter":    tftypes.NewValue(tftypes.String, "devices/pump-1/status"),
		"match_kind":      tftypes.NewValue(tftypes.String, "exact"),
		"expected_value":  tftypes.NewValue(tftypes.String, "online"),
		"capture_payload": tftypes.NewValue(tftypes.Bool, true),
	}, map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, "monitor-uuid"),
		"mqtt_broker_id":      tftypes.NewValue(tftypes.String, "broker-uuid"),
		"topic_filter":        tftypes.NewValue(tftypes.String, "devices/pump-1/status"),
		"stale_after_seconds": tftypes.NewValue(tftypes.Number, 300),
		"match_kind":          tftypes.NewValue(tftypes.String, "exact"),
		"expected_value":      tftypes.NewValue(tftypes.String, "online"),
		"capture_payload":     tftypes.NewValue(tftypes.Bool, true),
	})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	v, present := body["topic_monitor"]["stale_after_seconds"]
	if !present || v != nil {
		t.Errorf("expected an explicit null stale_after_seconds, got %v (present=%v)", v, present)
	}
}

// --- Read ---

// A broker takes its monitors with it, so a 404 here can mean either row is
// gone. Both leave nothing to refresh.
func TestMQTTTopicMonitorRead_RemovesVanishedMonitor(t *testing.T) {
	ctx := context.Background()
	s := mqttTopicMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := newMQTTTopicMonitorResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":             tftypes.NewValue(tftypes.String, "monitor-uuid"),
		"mqtt_broker_id": tftypes.NewValue(tftypes.String, "broker-uuid"),
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

func TestMQTTTopicMonitorRead_ReadsUnderTheBrokerFromState(t *testing.T) {
	ctx := context.Background()
	s := mqttTopicMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)
	var gotPath string
	r := newMQTTTopicMonitorResource(t, func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		json.NewEncoder(w).Encode(mqttTopicMonitorJSON(nil))
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":             tftypes.NewValue(tftypes.String, "monitor-uuid"),
		"mqtt_broker_id": tftypes.NewValue(tftypes.String, "broker-uuid"),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: state}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if gotPath != "/api/v1/mqtt_brokers/broker-uuid/topic_monitors/monitor-uuid" {
		t.Errorf("read must address the monitor under its broker, got %s", gotPath)
	}
}

// --- ImportState ---

func TestMQTTTopicMonitorImportState(t *testing.T) {
	ctx := context.Background()
	s := mqttTopicMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := &mqttTopicMonitorResource{}

	resp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "broker-uuid:monitor-uuid"}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var state mqttTopicMonitorModel
	resp.State.Get(ctx, &state)
	if state.MQTTBrokerID.ValueString() != "broker-uuid" {
		t.Errorf("expected mqtt_broker_id broker-uuid, got %s", state.MQTTBrokerID.ValueString())
	}
	if state.ID.ValueString() != "monitor-uuid" {
		t.Errorf("expected id monitor-uuid, got %s", state.ID.ValueString())
	}
}

// A bare monitor UUID cannot be read back — the API has no unnested route for
// one — so the import has to fail loudly rather than 404 on the next refresh.
func TestMQTTTopicMonitorImportState_RejectsMalformedID(t *testing.T) {
	ctx := context.Background()
	s := mqttTopicMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)

	for _, id := range []string{"monitor-uuid", "", ":monitor-uuid", "broker-uuid:", ":"} {
		r := &mqttTopicMonitorResource{}
		resp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
		r.ImportState(ctx, resource.ImportStateRequest{ID: id}, resp)

		if !resp.Diagnostics.HasError() {
			t.Errorf("id %q: expected an error naming the expected format", id)
			continue
		}
		if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "<mqtt_broker_id>:<id>") {
			t.Errorf("id %q: the error must name the format, got %q", id, resp.Diagnostics.Errors()[0].Detail())
		}
	}
}

// --- ValidateConfig ---

func TestValidateMQTTTopicMonitorChecks_NoCheck(t *testing.T) {
	config := mqttTopicMonitorModel{
		TopicFilter:       types.StringValue("sensors/#"),
		StaleAfterSeconds: types.Int64Null(),
		MatchKind:         types.StringNull(),
	}

	diags := validateMQTTTopicMonitorChecks(config)
	if !diags.HasError() {
		t.Fatal("expected an error for a monitor carrying no check")
	}
	if !strings.Contains(diags.Errors()[0].Detail(), "stale_after_seconds") {
		t.Errorf("the error must name the two checks, got %q", diags.Errors()[0].Detail())
	}
}

func TestValidateMQTTTopicMonitorChecks_FreshnessOnly(t *testing.T) {
	config := mqttTopicMonitorModel{
		TopicFilter:       types.StringValue("sensors/#"),
		StaleAfterSeconds: types.Int64Value(300),
		MatchKind:         types.StringNull(),
	}

	if diags := validateMQTTTopicMonitorChecks(config); diags.HasError() {
		t.Errorf("expected a freshness-only monitor to validate, got %v", diags)
	}
}

func TestValidateMQTTTopicMonitorChecks_MatchKindNeedsExpectedValue(t *testing.T) {
	config := mqttTopicMonitorModel{
		TopicFilter:       types.StringValue("devices/pump-1/status"),
		StaleAfterSeconds: types.Int64Null(),
		MatchKind:         types.StringValue("exact"),
		ExpectedValue:     types.StringNull(),
	}

	diags := validateMQTTTopicMonitorChecks(config)
	if !diags.HasError() {
		t.Fatal("expected an error when match_kind has no expected_value")
	}
	if got := diags.Errors()[0]; !strings.Contains(got.Detail(), "expected_value") {
		t.Errorf("expected the error to blame expected_value, got %q", got.Detail())
	}
}

func TestValidateMQTTTopicMonitorChecks_JSONKeyRequired(t *testing.T) {
	config := mqttTopicMonitorModel{
		TopicFilter:   types.StringValue("devices/pump-1/telemetry"),
		MatchKind:     types.StringValue("json_key"),
		ExpectedValue: types.StringValue("ok"),
		JSONKey:       types.StringNull(),
	}

	if diags := validateMQTTTopicMonitorChecks(config); !diags.HasError() {
		t.Error("expected an error when match_kind is json_key without json_key")
	}

	config.JSONKey = types.StringValue("battery.level")
	if diags := validateMQTTTopicMonitorChecks(config); diags.HasError() {
		t.Errorf("expected a complete json_key monitor to validate, got %v", diags)
	}
}

// json_key is only required for the json_key kind — an exact match must not be
// asked for one.
func TestValidateMQTTTopicMonitorChecks_ExactNeedsNoJSONKey(t *testing.T) {
	config := mqttTopicMonitorModel{
		TopicFilter:   types.StringValue("devices/pump-1/status"),
		MatchKind:     types.StringValue("exact"),
		ExpectedValue: types.StringValue("online"),
		JSONKey:       types.StringNull(),
	}

	if diags := validateMQTTTopicMonitorChecks(config); diags.HasError() {
		t.Errorf("expected an exact match with no json_key to validate, got %v", diags)
	}
}

// An unknown value only resolves at apply time, so the API owns the verdict
// rather than the plan failing on something it cannot see yet.
func TestValidateMQTTTopicMonitorChecks_UnknownDefersToAPI(t *testing.T) {
	for _, config := range []mqttTopicMonitorModel{
		{
			TopicFilter:       types.StringValue("sensors/#"),
			StaleAfterSeconds: types.Int64Unknown(),
			MatchKind:         types.StringNull(),
		},
		{
			TopicFilter:       types.StringValue("sensors/#"),
			StaleAfterSeconds: types.Int64Null(),
			MatchKind:         types.StringUnknown(),
		},
	} {
		if diags := validateMQTTTopicMonitorChecks(config); diags.HasError() {
			t.Errorf("expected no plan-time error for an unknown check, got %v", diags)
		}
	}
}

// The blame has to land on the attribute, or the practitioner gets an error with
// no line number in a file of three hundred generated monitors.
func TestValidateMQTTTopicMonitorChecks_BlamesTheAttribute(t *testing.T) {
	config := mqttTopicMonitorModel{
		TopicFilter:   types.StringValue("devices/pump-1/telemetry"),
		MatchKind:     types.StringValue("json_key"),
		ExpectedValue: types.StringValue("ok"),
		JSONKey:       types.StringNull(),
	}

	diags := validateMQTTTopicMonitorChecks(config)
	if len(diags.Errors()) == 0 {
		t.Fatal("expected an error")
	}
	want := path.Root("json_key").String()
	var found bool
	for _, got := range errorPaths(diags) {
		if got == want {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error carrying the %s path, got %v", want, errorPaths(diags))
	}
}
