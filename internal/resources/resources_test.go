package resources

import (
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- optionalString ---

func TestOptionalString_Nil(t *testing.T) {
	result := optionalString(nil)
	if !result.IsNull() {
		t.Errorf("expected null, got %v", result)
	}
}

func TestOptionalString_Value(t *testing.T) {
	v := "hello"
	result := optionalString(&v)
	if result.ValueString() != "hello" {
		t.Errorf("expected 'hello', got %q", result.ValueString())
	}
}

// --- mapInstanceToState ---

func TestMapInstanceToState(t *testing.T) {
	inst := &client.Instance{
		ID:          "uuid-1",
		DisplayName: "web-1",
		Hostname:    "web-1.local",
		Enabled:     true,
		CPUCount:    4,
		MemorySize:  8589934592,
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",
	}

	state := &instanceModel{}
	mapInstanceToState(inst, state)

	if state.ID.ValueString() != "uuid-1" {
		t.Errorf("expected ID uuid-1, got %s", state.ID.ValueString())
	}
	if state.DisplayName.ValueString() != "web-1" {
		t.Errorf("expected display_name web-1, got %s", state.DisplayName.ValueString())
	}
	if !state.Enabled.ValueBool() {
		t.Error("expected enabled true")
	}
	if state.CPUCount.ValueInt64() != 4 {
		t.Errorf("expected cpu_count 4, got %d", state.CPUCount.ValueInt64())
	}
	if state.MemorySize.ValueInt64() != 8589934592 {
		t.Errorf("expected memory_size 8589934592, got %d", state.MemorySize.ValueInt64())
	}
	if !state.LastSyncAt.IsNull() {
		t.Error("expected last_sync_at to be null")
	}
}

// --- mapTaskToState ---

func TestMapTaskToState_Active(t *testing.T) {
	task := &client.Task{
		ID:           "task-uuid",
		Name:         "health-check",
		ScheduleType: "interval",
		Status:       "active",
		PingKey:      "pk_123",
		PingURL:      "https://fivenines.io/ping/pk_123",
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if state.Paused.ValueBool() != false {
		t.Error("expected paused=false for active task")
	}
	if state.PingKey.ValueString() != "pk_123" {
		t.Errorf("expected ping_key pk_123, got %s", state.PingKey.ValueString())
	}
}

func TestMapTaskToState_Paused(t *testing.T) {
	task := &client.Task{
		ID:           "task-uuid",
		Name:         "paused-task",
		ScheduleType: "cron",
		Schedule:     "0 * * * *",
		Status:       "paused",
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if state.Paused.ValueBool() != true {
		t.Error("expected paused=true for paused task")
	}
}

func TestMapTaskToState_IntervalSeconds(t *testing.T) {
	interval := int64(300)
	task := &client.Task{
		ID:              "task-uuid",
		Name:            "interval-task",
		ScheduleType:    "interval",
		Status:          "active",
		IntervalSeconds: &interval,
		CreatedAt:       "2026-01-01T00:00:00Z",
		UpdatedAt:       "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if state.IntervalSeconds.ValueInt64() != 300 {
		t.Errorf("expected interval_seconds 300, got %d", state.IntervalSeconds.ValueInt64())
	}
}

func TestMapTaskToState_NilIntervalSeconds(t *testing.T) {
	task := &client.Task{
		ID:           "task-uuid",
		Name:         "cron-task",
		ScheduleType: "cron",
		Status:       "active",
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if !state.IntervalSeconds.IsNull() {
		t.Error("expected interval_seconds to be null for cron task")
	}
}

func TestMapTaskToState_EmptyScheduleIsNull(t *testing.T) {
	interval := int64(300)
	task := &client.Task{
		ID:              "task-uuid",
		Name:            "interval-task",
		ScheduleType:    "interval",
		Status:          "active",
		Schedule:        "",
		IntervalSeconds: &interval,
		CreatedAt:       "2026-01-01T00:00:00Z",
		UpdatedAt:       "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if !state.Schedule.IsNull() {
		t.Errorf("expected schedule to be null for interval task, got %v", state.Schedule)
	}
}

// --- validateTaskSchedule ---

func TestValidateTaskSchedule_CronRequiresSchedule(t *testing.T) {
	diags := validateTaskSchedule(taskModel{
		ScheduleType: types.StringValue("cron"),
		Schedule:     types.StringNull(),
	})

	if !diags.HasError() {
		t.Fatal("expected an error when schedule_type is cron without schedule")
	}
	if got := diags.Errors()[0].Detail(); got != `"schedule" is required when "schedule_type" is "cron".` {
		t.Errorf("unexpected detail: %s", got)
	}
}

func TestValidateTaskSchedule_IntervalRequiresIntervalSeconds(t *testing.T) {
	diags := validateTaskSchedule(taskModel{
		ScheduleType:    types.StringValue("interval"),
		IntervalSeconds: types.Int64Null(),
	})

	if !diags.HasError() {
		t.Fatal("expected an error when schedule_type is interval without interval_seconds")
	}
}

func TestValidateTaskSchedule_Valid(t *testing.T) {
	cron := validateTaskSchedule(taskModel{
		ScheduleType: types.StringValue("cron"),
		Schedule:     types.StringValue("0 2 * * *"),
	})
	if cron.HasError() {
		t.Errorf("expected no error for a valid cron task, got %v", cron.Errors())
	}

	interval := validateTaskSchedule(taskModel{
		ScheduleType:    types.StringValue("interval"),
		IntervalSeconds: types.Int64Value(300),
	})
	if interval.HasError() {
		t.Errorf("expected no error for a valid interval task, got %v", interval.Errors())
	}
}

func TestValidateTaskSchedule_NullScheduleType(t *testing.T) {
	diags := validateTaskSchedule(taskModel{})
	if diags.HasError() {
		t.Errorf("expected no error for a null schedule_type, got %v", diags.Errors())
	}
}

// A schedule sourced from another resource is unknown at validate time; the API
// gets the last word rather than the plan failing outright.
func TestValidateTaskSchedule_UnknownValuesDeferred(t *testing.T) {
	unknownType := validateTaskSchedule(taskModel{
		ScheduleType: types.StringUnknown(),
		Schedule:     types.StringNull(),
	})
	if unknownType.HasError() {
		t.Errorf("expected no error for unknown schedule_type, got %v", unknownType.Errors())
	}

	unknownSchedule := validateTaskSchedule(taskModel{
		ScheduleType: types.StringValue("cron"),
		Schedule:     types.StringUnknown(),
	})
	if unknownSchedule.HasError() {
		t.Errorf("expected no error for unknown schedule, got %v", unknownSchedule.Errors())
	}

	unknownInterval := validateTaskSchedule(taskModel{
		ScheduleType:    types.StringValue("interval"),
		IntervalSeconds: types.Int64Unknown(),
	})
	if unknownInterval.HasError() {
		t.Errorf("expected no error for unknown interval_seconds, got %v", unknownInterval.Errors())
	}
}

// --- mapWorkflowToState ---

func TestMapWorkflowToState(t *testing.T) {
	interval := int64(60)
	versionID := int64(5)
	wf := &client.Workflow{
		ID:                 42,
		Name:               "CPU Alert",
		Description:        "Alerts on high CPU",
		Status:             "active",
		IntervalSeconds:    &interval,
		TriggerType:        "metric_threshold",
		TriggerTypeLabel:   "Instance Metric",
		PublishedVersionID: &versionID,
		CreatedAt:          "2026-01-01T00:00:00Z",
		UpdatedAt:          "2026-01-01T00:00:00Z",
	}

	state := &workflowModel{}
	mapWorkflowToState(wf, state)

	if state.ID.ValueInt64() != 42 {
		t.Errorf("expected ID 42, got %d", state.ID.ValueInt64())
	}
	if state.IntervalSeconds.ValueInt64() != 60 {
		t.Errorf("expected interval_seconds 60, got %d", state.IntervalSeconds.ValueInt64())
	}
	if state.PublishedVersionID.ValueInt64() != 5 {
		t.Errorf("expected published_version_id 5, got %d", state.PublishedVersionID.ValueInt64())
	}
}

func TestMapWorkflowToState_NilOptionals(t *testing.T) {
	wf := &client.Workflow{
		ID:        1,
		Name:      "Draft WF",
		Status:    "draft",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	state := &workflowModel{}
	mapWorkflowToState(wf, state)

	if !state.IntervalSeconds.IsNull() {
		t.Error("expected interval_seconds to be null")
	}
	if !state.PublishedVersionID.IsNull() {
		t.Error("expected published_version_id to be null")
	}
	if !state.NextEvaluationAt.IsNull() {
		t.Error("expected next_evaluation_at to be null")
	}
}

// --- mapMQTTBrokerToState ---

func TestMapMQTTBrokerToState(t *testing.T) {
	watcher := "host-uuid"
	lastError := "Connection refused: not authorised"
	broker := &client.MQTTBroker{
		ID:                "broker-uuid",
		Name:              "Factory-floor Mosquitto",
		Host:              "mqtt.internal",
		Port:              8883,
		TLS:               true,
		UsernameSet:       true,
		PasswordSet:       true,
		WatcherHostID:     &watcher,
		Status:            "config_error",
		LastErrorMessage:  &lastError,
		Stale:             true,
		TopicMonitorCount: 300,
		CreatedAt:         "2026-01-01T00:00:00Z",
		UpdatedAt:         "2026-01-01T00:00:00Z",
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
	if !state.Stale.ValueBool() {
		t.Error("expected stale true")
	}
	if state.TopicMonitorCount.ValueInt64() != 300 {
		t.Errorf("expected topic_monitor_count 300, got %d", state.TopicMonitorCount.ValueInt64())
	}
	if state.LastErrorMessage.ValueString() != lastError {
		t.Errorf("expected last_error_message %q, got %q", lastError, state.LastErrorMessage.ValueString())
	}
}

func TestMapMQTTBrokerToState_Unassigned(t *testing.T) {
	broker := &client.MQTTBroker{
		ID: "broker-uuid", Name: "Unassigned", Host: "mqtt.internal", Port: 1883,
		Status: "unknown", Stale: true,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}

	state := &mqttBrokerModel{}
	mapMQTTBrokerToState(broker, state)

	if !state.WatcherHostID.IsNull() {
		t.Error("expected watcher_host_id to be null")
	}
	if !state.LastErrorMessage.IsNull() || !state.LastConnectedAt.IsNull() || !state.LastSyncedAt.IsNull() {
		t.Error("expected the never-reported fields to be null")
	}
	if state.UsernameSet.ValueBool() || state.PasswordSet.ValueBool() {
		t.Error("expected username_set and password_set false for an anonymous broker")
	}
}

// --- credentialWrite ---

func TestCredentialWrite_UnchangedIsOmitted(t *testing.T) {
	// The import case: Terraform has never seen the credential, so an unrelated
	// edit must not wipe the one the broker is working with.
	if got := credentialWrite(types.StringNull(), types.StringNull()); got != nil {
		t.Errorf("expected an omitted credential, got %s", got)
	}
	// And a value that did not change is not worth re-sending.
	same := types.StringValue("factory")
	if got := credentialWrite(same, same); got != nil {
		t.Errorf("expected an omitted credential, got %s", got)
	}
}

func TestCredentialWrite_RemovedIsCleared(t *testing.T) {
	got := credentialWrite(types.StringNull(), types.StringValue("factory"))
	if string(got) != "null" {
		t.Errorf("expected explicit null, got %s", got)
	}
}

func TestCredentialWrite_ChangedIsSent(t *testing.T) {
	got := credentialWrite(types.StringValue("rotated"), types.StringValue("factory"))
	if string(got) != `"rotated"` {
		t.Errorf(`expected "rotated", got %s`, got)
	}
}

// --- mapMQTTTopicMonitorToState ---

func TestMapMQTTTopicMonitorToState_Freshness(t *testing.T) {
	stale := int64(300)
	subscribed := "2026-01-01T00:00:00Z"
	monitor := &client.MQTTTopicMonitor{
		ID:                      "monitor-uuid",
		MQTTBrokerID:            "broker-uuid",
		TopicFilter:             "sensors/+/temperature",
		StaleAfterSeconds:       &stale,
		CapturePayload:          true,
		EffectiveCapturePayload: true,
		FreshnessCheck:          true,
		SubscribedSince:         &subscribed,
		CreatedAt:               "2026-01-01T00:00:00Z",
		UpdatedAt:               "2026-01-01T00:00:00Z",
	}

	state := &mqttTopicMonitorModel{}
	mapMQTTTopicMonitorToState(monitor, state)

	if state.MQTTBrokerID.ValueString() != "broker-uuid" {
		t.Errorf("expected mqtt_broker_id broker-uuid, got %s", state.MQTTBrokerID.ValueString())
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
}

func TestMapMQTTTopicMonitorToState_PayloadForcesCapture(t *testing.T) {
	matchKind := "exact"
	expected := "online"
	monitor := &client.MQTTTopicMonitor{
		ID: "monitor-uuid", MQTTBrokerID: "broker-uuid", TopicFilter: "devices/pump-1/status",
		MatchKind: &matchKind, ExpectedValue: &expected,
		// Stored false, but a payload expectation forces capture on server-side.
		CapturePayload: false, EffectiveCapturePayload: true,
		PayloadCheck: true, ExactTopic: true,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}

	state := &mqttTopicMonitorModel{}
	mapMQTTTopicMonitorToState(monitor, state)

	if !state.StaleAfterSeconds.IsNull() {
		t.Error("expected stale_after_seconds to be null")
	}
	if state.MatchKind.ValueString() != "exact" || state.ExpectedValue.ValueString() != "online" {
		t.Error("expected the payload expectation to be mapped")
	}
	if state.CapturePayload.ValueBool() || !state.EffectiveCapturePayload.ValueBool() {
		t.Error("expected capture_payload false and effective_capture_payload true")
	}
	if !state.SubscribedSince.IsNull() {
		t.Error("expected subscribed_since to be null when the watcher holds no subscription")
	}
}

// --- mqttTopicMonitorInput ---

func TestMQTTTopicMonitorInput_DroppedChecksSendNull(t *testing.T) {
	plan := mqttTopicMonitorModel{
		TopicFilter:       types.StringValue("sensors/+/temperature"),
		StaleAfterSeconds: types.Int64Value(300),
		MatchKind:         types.StringNull(),
		ExpectedValue:     types.StringNull(),
		JSONKey:           types.StringNull(),
		CapturePayload:    types.BoolValue(true),
	}

	input := mqttTopicMonitorInput(plan)

	if input.StaleAfterSeconds == nil || *input.StaleAfterSeconds != 300 {
		t.Errorf("expected stale_after_seconds 300, got %v", input.StaleAfterSeconds)
	}
	if input.MatchKind != nil || input.ExpectedValue != nil || input.JSONKey != nil {
		t.Error("expected the unset payload check to marshal as null")
	}
	if !input.CapturePayload {
		t.Error("expected capture_payload true")
	}
}

// --- validateMQTTTopicMonitorChecks ---

func TestValidateMQTTTopicMonitorChecks_NoCheck(t *testing.T) {
	config := mqttTopicMonitorModel{
		TopicFilter:       types.StringValue("sensors/#"),
		StaleAfterSeconds: types.Int64Null(),
		MatchKind:         types.StringNull(),
	}

	if diags := validateMQTTTopicMonitorChecks(config); !diags.HasError() {
		t.Error("expected an error for a monitor carrying no check")
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

	if diags := validateMQTTTopicMonitorChecks(config); !diags.HasError() {
		t.Error("expected an error when match_kind has no expected_value")
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

// An unknown value only resolves at apply time, so the API owns the verdict
// rather than the plan failing on something it cannot see yet.
func TestValidateMQTTTopicMonitorChecks_UnknownDefersToAPI(t *testing.T) {
	config := mqttTopicMonitorModel{
		TopicFilter:       types.StringValue("sensors/#"),
		StaleAfterSeconds: types.Int64Unknown(),
		MatchKind:         types.StringNull(),
	}

	if diags := validateMQTTTopicMonitorChecks(config); diags.HasError() {
		t.Errorf("expected no plan-time error for an unknown check, got %v", diags)
	}
}

// Verify types.String null behavior (framework contract test)
func TestTypesStringNull(t *testing.T) {
	s := types.StringNull()
	if !s.IsNull() {
		t.Error("expected IsNull() to be true")
	}
}
