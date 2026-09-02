package resources

import (
	"context"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func ptr[T any](v T) *T { return &v }

// --- mapInstanceToState ---

func TestMapInstanceToState(t *testing.T) {
	inst := &client.Instance{
		ID:          "uuid-1",
		DisplayName: "web-1",
		Hostname:    ptr("web-1.local"),
		Enabled:     true,
		CPUCount:    ptr(int64(4)),
		MemorySize:  ptr(int64(8589934592)),
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
	// An instance that never synced reports null for everything the agent
	// fills in; those must stay null instead of collapsing to "".
	if !state.IPv4.IsNull() {
		t.Errorf("expected ipv4 to be null, got %q", state.IPv4.ValueString())
	}
	if !state.KernelVersion.IsNull() {
		t.Errorf("expected kernel_version to be null, got %q", state.KernelVersion.ValueString())
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
		Schedule:     ptr("0 * * * *"),
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
		Schedule:        ptr(""),
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
		Description:        ptr("Alerts on high CPU"),
		Status:             "active",
		IntervalSeconds:    &interval,
		TriggerType:        ptr("metric_threshold"),
		TriggerTypeLabel:   ptr("Instance Metric"),
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

// Verify types.String null behavior (framework contract test)
func TestTypesStringNull(t *testing.T) {
	s := types.StringNull()
	if !s.IsNull() {
		t.Error("expected IsNull() to be true")
	}
}

// --- uptime monitor: protocol cross-field validation ---

func TestMissingProtocolAttributes(t *testing.T) {
	tests := []struct {
		name   string
		config uptimeMonitorModel
		want   []string
	}{
		{
			name:   "https without url",
			config: uptimeMonitorModel{Protocol: types.StringValue("https")},
			want:   []string{"url"},
		},
		{
			name: "https with url",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("https"),
				URL:      types.StringValue("https://example.com"),
			},
		},
		{
			name:   "tcp without hostname or port",
			config: uptimeMonitorModel{Protocol: types.StringValue("tcp")},
			want:   []string{"hostname", "port"},
		},
		{
			name: "tcp with hostname but no port",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("tcp"),
				Hostname: types.StringValue("db.example.com"),
			},
			want: []string{"port"},
		},
		{
			name:   "icmp without hostname",
			config: uptimeMonitorModel{Protocol: types.StringValue("icmp")},
			want:   []string{"hostname"},
		},
		{
			name:   "dns without record type",
			config: uptimeMonitorModel{Protocol: types.StringValue("dns")},
			want:   []string{"dns_record_type"},
		},
		{
			name: "dns with record type",
			config: uptimeMonitorModel{
				Protocol:      types.StringValue("dns"),
				DNSRecordType: types.StringValue("A"),
			},
		},
		{
			name: "unknown value is not treated as missing",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("https"),
				URL:      types.StringUnknown(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingProtocolAttributes(tt.config)
			if len(got) != len(tt.want) {
				t.Fatalf("expected missing %v, got %v", tt.want, got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("expected missing %v, got %v", tt.want, got)
				}
			}
		})
	}
}

// --- uptime monitor: mapToState ---

func mapUptimeMonitor(t *testing.T, m *client.UptimeMonitor, state *uptimeMonitorModel) {
	t.Helper()
	var diags diag.Diagnostics
	(&uptimeMonitorResource{}).mapToState(context.Background(), m, state, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestMapUptimeMonitorToState_PausedFromStatus(t *testing.T) {
	for status, wantPaused := range map[string]bool{
		"paused": true, "up": false, "down": false, "unknown": false, "recovering": false,
	} {
		state := &uptimeMonitorModel{}
		mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", Status: status}, state)

		if state.Paused.ValueBool() != wantPaused {
			t.Errorf("status %q: expected paused %v, got %v", status, wantPaused, state.Paused.ValueBool())
		}
		if state.Status.ValueString() != status {
			t.Errorf("expected status %q, got %q", status, state.Status.ValueString())
		}
	}
}

func TestMapUptimeMonitorToState_DNSExpectedRecords(t *testing.T) {
	emptyList := types.ListValueMust(types.StringType, []attr.Value{})

	tests := []struct {
		name      string
		apiValue  []string
		prior     types.List
		wantNull  bool
		wantElems int
	}{
		{
			name:     "unset stays null",
			prior:    types.ListNull(types.StringType),
			wantNull: true,
		},
		{
			// The API normalises a pinned [] to "no expectation" and reads it back
			// as [], so a config of [] must not flip to null and diff forever.
			name:      "explicit empty list is preserved",
			prior:     emptyList,
			wantNull:  false,
			wantElems: 0,
		},
		{
			name:      "records are read back",
			apiValue:  []string{"1.2.3.4", "5.6.7.8"},
			prior:     types.ListNull(types.StringType),
			wantNull:  false,
			wantElems: 2,
		},
		{
			name:      "records replace a previously empty list",
			apiValue:  []string{"1.2.3.4"},
			prior:     emptyList,
			wantNull:  false,
			wantElems: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &uptimeMonitorModel{DNSExpectedRecords: tt.prior}
			mapUptimeMonitor(t, &client.UptimeMonitor{
				ID:                 "mon-uuid",
				Protocol:           "dns",
				DNSExpectedRecords: tt.apiValue,
			}, state)

			if state.DNSExpectedRecords.IsNull() != tt.wantNull {
				t.Fatalf("expected null=%v, got %v", tt.wantNull, state.DNSExpectedRecords)
			}
			if !tt.wantNull && len(state.DNSExpectedRecords.Elements()) != tt.wantElems {
				t.Errorf("expected %d elements, got %v", tt.wantElems, state.DNSExpectedRecords.Elements())
			}
		})
	}
}

func TestIsEmptyList(t *testing.T) {
	if isEmptyList(types.ListNull(types.StringType)) {
		t.Error("null list is not an empty list")
	}
	if isEmptyList(types.ListUnknown(types.StringType)) {
		t.Error("unknown list is not an empty list")
	}
	if !isEmptyList(types.ListValueMust(types.StringType, []attr.Value{})) {
		t.Error("expected empty list")
	}
	if isEmptyList(types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")})) {
		t.Error("populated list is not empty")
	}
}

// --- uptime monitor: the inverse protocol table ---

func TestForbiddenProtocolAttributes(t *testing.T) {
	tests := []struct {
		name   string
		config uptimeMonitorModel
		want   []string
	}{
		{
			name: "https with a dns attribute left in config",
			config: uptimeMonitorModel{
				Protocol:      types.StringValue("https"),
				DNSRecordType: types.StringValue("A"),
			},
			want: []string{"dns_record_type"},
		},
		{
			// The switch that motivated the table: leaving headers on a dns monitor
			// plans a known map that the apply nulls out.
			name: "dns with http attributes left in config",
			config: uptimeMonitorModel{
				Protocol:      types.StringValue("dns"),
				CustomHeaders: types.MapValueMust(types.StringType, map[string]attr.Value{"X": types.StringValue("y")}),
				Keyword:       types.StringValue("ok"),
			},
			want: []string{"keyword", "custom_headers"},
		},
		{
			name: "tcp with a keyword left in config",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("tcp"),
				Keyword:  types.StringValue("ok"),
			},
			want: []string{"keyword"},
		},
		{
			name:   "a clean https config forbids nothing",
			config: uptimeMonitorModel{Protocol: types.StringValue("https"), URL: types.StringValue("https://example.com")},
		},
		{
			// Unknown comes from an unresolved reference, not from the user setting
			// a value the protocol cannot use.
			name: "unknown values are not flagged",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("tcp"),
				Keyword:  types.StringUnknown(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forbiddenProtocolAttributes(tt.config)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("expected %v, got %v", tt.want, got)
				}
			}
		})
	}
}

// Every name in either protocol table must resolve in protocolScopedValues, or
// the lookup hands .IsNull() a nil interface and panics mid-plan.
func TestProtocolTablesResolve(t *testing.T) {
	values := protocolScopedValues(uptimeMonitorModel{})
	for _, table := range []map[string][]string{protocolRequirements, protocolForbidden} {
		for protocol, names := range table {
			for _, name := range names {
				if _, ok := values[name]; !ok {
					t.Errorf("protocol %q references %q, which protocolScopedValues does not provide", protocol, name)
				}
			}
		}
	}
}

func TestMapUptimeMonitorToState_KeywordEmptyVsNull(t *testing.T) {
	// An explicitly configured "" must survive; an absent keyword must be null.
	state := &uptimeMonitorModel{Keyword: types.StringValue("")}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", Keyword: ""}, state)
	if state.Keyword.IsNull() {
		t.Error("an explicitly empty keyword must stay an empty string, not become null")
	}

	state = &uptimeMonitorModel{Keyword: types.StringNull()}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", Keyword: ""}, state)
	if !state.Keyword.IsNull() {
		t.Errorf("an unset keyword must be null, got %q", state.Keyword.ValueString())
	}

	state = &uptimeMonitorModel{Keyword: types.StringNull()}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", Keyword: "healthy"}, state)
	if state.Keyword.ValueString() != "healthy" {
		t.Errorf("expected healthy, got %q", state.Keyword.ValueString())
	}
}

func TestMapUptimeMonitorToState_ProbeRegionIDsPinnedEmpty(t *testing.T) {
	// Sending [] is only half of clearable; the read side has to preserve it too,
	// or the plan re-proposes [] and every apply fails.
	state := &uptimeMonitorModel{ProbeRegionIDs: types.ListValueMust(types.Int64Type, []attr.Value{})}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid"}, state)
	if state.ProbeRegionIDs.IsNull() {
		t.Error("an explicitly empty probe_region_ids must stay [], not become null")
	}

	state = &uptimeMonitorModel{ProbeRegionIDs: types.ListNull(types.Int64Type)}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", ProbeRegionIDs: []int64{1, 2}}, state)
	if len(state.ProbeRegionIDs.Elements()) != 2 {
		t.Errorf("expected 2 regions, got %v", state.ProbeRegionIDs.Elements())
	}
}
