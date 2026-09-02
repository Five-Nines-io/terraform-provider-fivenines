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

// --- mapNetworkDeviceToState ---

func TestMapNetworkDeviceToState_Healthy(t *testing.T) {
	polledAt := "2026-01-01T00:05:00Z"
	dev := &client.NetworkDevice{
		ID:                  "dev-uuid",
		Name:                "Core Switch",
		IPAddress:           "192.168.1.1",
		DeviceType:          "switch",
		PollingInterval:     60,
		SNMPVersion:         "v2c",
		Status:              "up",
		Vendor:              "Cisco",
		LastPolledAt:        &polledAt,
		ConsecutiveFailures: 0,
		CreatedAt:           "2026-01-01T00:00:00Z",
		UpdatedAt:           "2026-01-01T00:00:00Z",
	}

	state := &networkDeviceModel{}
	mapNetworkDeviceToState(dev, state)

	if state.ID.ValueString() != "dev-uuid" {
		t.Errorf("expected ID dev-uuid, got %s", state.ID.ValueString())
	}
	if state.SNMPVersion.ValueString() != "v2c" {
		t.Errorf("expected snmp_version v2c, got %s", state.SNMPVersion.ValueString())
	}
	if state.ConsecutiveFailures.ValueInt64() != 0 {
		t.Errorf("expected consecutive_failures 0, got %d", state.ConsecutiveFailures.ValueInt64())
	}
	if !state.LastErrorType.IsNull() {
		t.Error("expected last_error_type to be null")
	}
	if !state.LastErrorMessage.IsNull() {
		t.Error("expected last_error_message to be null")
	}
	if state.LastPolledAt.ValueString() != polledAt {
		t.Errorf("expected last_polled_at %s, got %s", polledAt, state.LastPolledAt.ValueString())
	}
}

func TestMapNetworkDeviceToState_Unreachable(t *testing.T) {
	errType := "timeout"
	errMsg := "no response after 5s"
	dev := &client.NetworkDevice{
		ID:                  "dev-uuid",
		Name:                "Core Switch",
		IPAddress:           "192.168.1.1",
		Status:              "unreachable",
		ConsecutiveFailures: 3,
		LastErrorType:       &errType,
		LastErrorMessage:    &errMsg,
		CreatedAt:           "2026-01-01T00:00:00Z",
		UpdatedAt:           "2026-01-01T00:00:00Z",
	}

	state := &networkDeviceModel{}
	mapNetworkDeviceToState(dev, state)

	if state.Status.ValueString() != "unreachable" {
		t.Errorf("expected status unreachable, got %s", state.Status.ValueString())
	}
	if state.ConsecutiveFailures.ValueInt64() != 3 {
		t.Errorf("expected consecutive_failures 3, got %d", state.ConsecutiveFailures.ValueInt64())
	}
	if state.LastErrorType.ValueString() != errType {
		t.Errorf("expected last_error_type %s, got %s", errType, state.LastErrorType.ValueString())
	}
	if state.LastErrorMessage.ValueString() != errMsg {
		t.Errorf("expected last_error_message %s, got %s", errMsg, state.LastErrorMessage.ValueString())
	}
	if !state.LastPolledAt.IsNull() {
		t.Error("expected last_polled_at to be null")
	}
}

// Verify types.String null behavior (framework contract test)
func TestTypesStringNull(t *testing.T) {
	s := types.StringNull()
	if !s.IsNull() {
		t.Error("expected IsNull() to be true")
	}
}
