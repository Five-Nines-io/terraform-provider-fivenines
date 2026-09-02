package resources

import (
	"context"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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

// --- mapHostGroupToState ---

func TestMapHostGroupToState(t *testing.T) {
	group := &client.HostGroup{
		ID:        7,
		Name:      "Production",
		Position:  2,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-02T00:00:00Z",
	}

	state := &hostGroupModel{}
	mapHostGroupToState(group, state)

	if state.ID.ValueInt64() != 7 {
		t.Errorf("expected ID 7, got %d", state.ID.ValueInt64())
	}
	if state.Name.ValueString() != "Production" {
		t.Errorf("expected name Production, got %s", state.Name.ValueString())
	}
	if state.Position.ValueInt64() != 2 {
		t.Errorf("expected position 2, got %d", state.Position.ValueInt64())
	}
	if state.UpdatedAt.ValueString() != "2026-01-02T00:00:00Z" {
		t.Errorf("expected updated_at 2026-01-02T00:00:00Z, got %s", state.UpdatedAt.ValueString())
	}
}

// Groups that were never explicitly positioned come back with position 0, which
// must land in state as a known 0 rather than null.
func TestMapHostGroupToState_UnpositionedGroup(t *testing.T) {
	group := &client.HostGroup{
		ID:        1,
		Name:      "Legacy",
		Position:  0,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	state := &hostGroupModel{}
	mapHostGroupToState(group, state)

	if state.Position.IsNull() {
		t.Fatal("expected position to be known")
	}
	if state.Position.ValueInt64() != 0 {
		t.Errorf("expected position 0, got %d", state.Position.ValueInt64())
	}
}

// --- unknownOnPositionChange ---

var hostGroupObjectType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{"position": tftypes.Number},
}

// hostGroupRaw builds the raw plan/state value the plan modifier inspects. A nil
// position stands for the whole object being absent (create or destroy).
func hostGroupRaw(present bool) tftypes.Value {
	if !present {
		return tftypes.NewValue(hostGroupObjectType, nil)
	}
	return tftypes.NewValue(hostGroupObjectType, map[string]tftypes.Value{
		"position": tftypes.NewValue(tftypes.Number, 1),
	})
}

func TestUnknownOnPositionChange(t *testing.T) {
	tests := []struct {
		name         string
		planPresent  bool
		statePresent bool
		planValue    types.Int64
		stateValue   types.Int64
		wantUnknown  bool
	}{
		{
			name:         "create with an explicit position defers to the API",
			planPresent:  true,
			statePresent: false,
			planValue:    types.Int64Value(2),
			stateValue:   types.Int64Null(),
			wantUnknown:  true,
		},
		{
			name:         "update that moves the group defers to the API",
			planPresent:  true,
			statePresent: true,
			planValue:    types.Int64Value(3),
			stateValue:   types.Int64Value(1),
			wantUnknown:  true,
		},
		{
			name:         "unchanged position stays known",
			planPresent:  true,
			statePresent: true,
			planValue:    types.Int64Value(1),
			stateValue:   types.Int64Value(1),
			wantUnknown:  false,
		},
		{
			name:         "destroy plan is left alone",
			planPresent:  false,
			statePresent: true,
			planValue:    types.Int64Null(),
			stateValue:   types.Int64Value(1),
			wantUnknown:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := planmodifier.Int64Request{
				Plan:       tfsdk.Plan{Raw: hostGroupRaw(tt.planPresent)},
				State:      tfsdk.State{Raw: hostGroupRaw(tt.statePresent)},
				PlanValue:  tt.planValue,
				StateValue: tt.stateValue,
			}
			resp := &planmodifier.Int64Response{PlanValue: tt.planValue}

			unknownOnPositionChange{}.PlanModifyInt64(context.Background(), req, resp)

			if got := resp.PlanValue.IsUnknown(); got != tt.wantUnknown {
				t.Errorf("expected unknown=%v, got plan value %v", tt.wantUnknown, resp.PlanValue)
			}
		})
	}
}

// Verify types.String null behavior (framework contract test)
func TestTypesStringNull(t *testing.T) {
	s := types.StringNull()
	if !s.IsNull() {
		t.Error("expected IsNull() to be true")
	}
}
