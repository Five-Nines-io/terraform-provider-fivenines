package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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

// --- JSON attribute helpers ---

func TestJSONEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"key order", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"whitespace", "{\n  \"a\": 1\n}", `{"a":1}`, true},
		{"nested arrays keep order", `{"n":[1,2]}`, `{"n":[2,1]}`, false},
		{"different values", `{"a":1}`, `{"a":2}`, false},
		{"invalid left", `not json`, `{}`, false},
		{"invalid right", `{}`, `not json`, false},
		{"both invalid", `nope`, `nope`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jsonEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("jsonEqual(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestPreserveJSONIfEqual_KeepsFormattingWhenEquivalent(t *testing.T) {
	prior := types.StringValue("{\n  \"nodes\": [],\n  \"edges\": []\n}")

	got := preserveJSONIfEqual(prior, `{"edges":[],"nodes":[]}`)

	if got.ValueString() != prior.ValueString() {
		t.Errorf("expected the state formatting to survive, got %q", got.ValueString())
	}
}

func TestPreserveJSONIfEqual_TakesAPIValueOnDrift(t *testing.T) {
	prior := types.StringValue(`{"nodes":[]}`)

	got := preserveJSONIfEqual(prior, `{"nodes":[{"id":"n1"}]}`)

	if got.ValueString() != `{"nodes":[{"id":"n1"}]}` {
		t.Errorf("expected the API value on drift, got %q", got.ValueString())
	}
}

func TestPreserveJSONIfEqual_NullPrior(t *testing.T) {
	got := preserveJSONIfEqual(types.StringNull(), `{"nodes":[]}`)

	if got.ValueString() != `{"nodes":[]}` {
		t.Errorf("expected the API value to populate an empty state, got %q", got.ValueString())
	}
}

func TestPreserveJSONIfEqual_NoPublishedVersion(t *testing.T) {
	got := preserveJSONIfEqual(types.StringValue(`{"nodes":[]}`), "")

	if !got.IsNull() {
		t.Errorf("expected null when nothing is published, got %q", got.ValueString())
	}
}

func TestJSONAttrEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b types.String
		want bool
	}{
		{"both null", types.StringNull(), types.StringNull(), true},
		{"null vs set", types.StringNull(), types.StringValue(`{}`), false},
		{"set vs null", types.StringValue(`{}`), types.StringNull(), false},
		{"reformatted", types.StringValue(`{"a":1,"b":2}`), types.StringValue(`{"b":2,"a":1}`), true},
		{"different", types.StringValue(`{"a":1}`), types.StringValue(`{"a":2}`), false},
		{"unknown vs null", types.StringUnknown(), types.StringNull(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jsonAttrEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("jsonAttrEqual() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- jsonSemanticEquality plan modifier ---

func TestJSONSemanticEquality_SuppressesFormattingDiff(t *testing.T) {
	state := types.StringValue(`{"nodes":[],"edges":[]}`)
	req := planmodifier.StringRequest{
		StateValue: state,
		PlanValue:  types.StringValue("{\n  \"edges\": [],\n  \"nodes\": []\n}"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}

	jsonSemanticEquality{}.PlanModifyString(context.Background(), req, resp)

	if resp.PlanValue.ValueString() != state.ValueString() {
		t.Errorf("expected the plan to collapse onto state, got %q", resp.PlanValue.ValueString())
	}
}

func TestJSONSemanticEquality_KeepsRealDiff(t *testing.T) {
	planned := types.StringValue(`{"nodes":[{"id":"n1"}]}`)
	req := planmodifier.StringRequest{
		StateValue: types.StringValue(`{"nodes":[]}`),
		PlanValue:  planned,
	}
	resp := &planmodifier.StringResponse{PlanValue: planned}

	jsonSemanticEquality{}.PlanModifyString(context.Background(), req, resp)

	if resp.PlanValue.ValueString() != planned.ValueString() {
		t.Errorf("expected a real change to survive, got %q", resp.PlanValue.ValueString())
	}
}

func TestJSONSemanticEquality_LeavesUnknownAlone(t *testing.T) {
	req := planmodifier.StringRequest{
		StateValue: types.StringValue(`{"nodes":[]}`),
		PlanValue:  types.StringUnknown(),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}

	jsonSemanticEquality{}.PlanModifyString(context.Background(), req, resp)

	if !resp.PlanValue.IsUnknown() {
		t.Errorf("expected the unknown plan value to stay unknown, got %q", resp.PlanValue.ValueString())
	}
}

// --- marshalGraph ---

func TestMarshalGraph_NilIsEmpty(t *testing.T) {
	got, err := marshalGraph(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for an absent graph, got %q", got)
	}
}

func TestMarshalGraph_StableKeyOrder(t *testing.T) {
	got, err := marshalGraph(map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"edges":[],"nodes":[]}` {
		t.Errorf("expected sorted keys, got %q", got)
	}
}

// --- activationErrorDetail ---

func TestActivationErrorDetail_ExplainsMissingVersion(t *testing.T) {
	err := &client.APIError{StatusCode: 422, Message: "cannot activate"}

	detail := activationErrorDetail(err)

	if !strings.Contains(detail, "published version") {
		t.Errorf("expected the 422 to explain the missing published version, got %q", detail)
	}
}

func TestActivationErrorDetail_PassesOtherErrorsThrough(t *testing.T) {
	err := &client.APIError{StatusCode: 500, Message: "boom"}

	if detail := activationErrorDetail(err); detail != err.Error() {
		t.Errorf("expected the raw error, got %q", detail)
	}
}

// Verify types.String null behavior (framework contract test)
func TestTypesStringNull(t *testing.T) {
	s := types.StringNull()
	if !s.IsNull() {
		t.Error("expected IsNull() to be true")
	}
}
