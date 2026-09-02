package resources

import (
	"context"
	"testing"
	"time"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
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

// Verify types.String null behavior (framework contract test)
func TestTypesStringNull(t *testing.T) {
	s := types.StringNull()
	if !s.IsNull() {
		t.Error("expected IsNull() to be true")
	}
}

// --- parseWindowTime ---

func TestParseWindowTime(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("loading Europe/Paris: %v", err)
	}

	tests := []struct {
		name      string
		input     string
		wantUTC   string
		wantZoned bool
		wantOK    bool
	}{
		{"utc", "2026-09-01T22:00:00Z", "2026-09-01T22:00:00Z", true, true},
		{"offset", "2026-09-02T00:00:00+02:00", "2026-09-01T22:00:00Z", true, true},
		{"offset without colon", "2026-09-02T00:00:00+0200", "2026-09-01T22:00:00Z", true, true},
		{"fractional seconds", "2026-09-01T22:00:00.500Z", "2026-09-01T22:00:00Z", true, true},
		{"minute precision", "2026-09-01T22:00Z", "2026-09-01T22:00:00Z", true, true},
		{"space separator", "2026-09-01 22:00:00Z", "2026-09-01T22:00:00Z", true, true},
		{"bare local read in page zone", "2026-09-02T00:00:00", "2026-09-01T22:00:00Z", false, true},
		{"bare local minute precision", "2026-09-02T00:00", "2026-09-01T22:00:00Z", false, true},
		{"impossible date", "2026-02-30T00:00:00Z", "", false, false},
		{"not a timestamp", "tomorrow", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, zoned, ok := parseWindowTime(tt.input, paris)
			if ok != tt.wantOK {
				t.Fatalf("parseWindowTime(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if zoned != tt.wantZoned {
				t.Errorf("parseWindowTime(%q) zoned = %v, want %v", tt.input, zoned, tt.wantZoned)
			}
			if utc := got.UTC().Format(time.RFC3339); utc != tt.wantUTC {
				t.Errorf("parseWindowTime(%q) = %s, want %s", tt.input, utc, tt.wantUTC)
			}
		})
	}
}

// --- preserveTimestamp ---

func TestPreserveTimestamp_SameInstantKeepsConfiguredFormat(t *testing.T) {
	// The API normalizes into the page timezone; the practitioner wrote UTC.
	got := preserveTimestamp(types.StringValue("2026-09-01T22:00:00Z"), "2026-09-02T00:00:00+02:00", "Europe/Paris")
	if got.ValueString() != "2026-09-01T22:00:00Z" {
		t.Errorf("expected the configured value to be kept, got %q", got.ValueString())
	}
}

func TestPreserveTimestamp_BareLocalResolvedInPageZone(t *testing.T) {
	got := preserveTimestamp(types.StringValue("2026-09-02T00:00:00"), "2026-09-02T00:00:00+02:00", "Europe/Paris")
	if got.ValueString() != "2026-09-02T00:00:00" {
		t.Errorf("expected the configured value to be kept, got %q", got.ValueString())
	}
}

func TestPreserveTimestamp_DifferentInstantTakesAPIValue(t *testing.T) {
	got := preserveTimestamp(types.StringValue("2026-09-01T22:00:00Z"), "2026-09-05T00:00:00+02:00", "Europe/Paris")
	if got.ValueString() != "2026-09-05T00:00:00+02:00" {
		t.Errorf("expected drift to surface the API value, got %q", got.ValueString())
	}
}

func TestPreserveTimestamp_NullPriorTakesAPIValue(t *testing.T) {
	got := preserveTimestamp(types.StringNull(), "2026-09-02T00:00:00+02:00", "Europe/Paris")
	if got.ValueString() != "2026-09-02T00:00:00+02:00" {
		t.Errorf("expected the API value, got %q", got.ValueString())
	}
}

func TestPreserveTimestamp_UnparseableFallsBackToAPIValue(t *testing.T) {
	got := preserveTimestamp(types.StringValue("whenever"), "2026-09-02T00:00:00+02:00", "")
	if got.ValueString() != "2026-09-02T00:00:00+02:00" {
		t.Errorf("expected the API value, got %q", got.ValueString())
	}
}

// --- preserveBlankString ---

func TestPreserveBlankString(t *testing.T) {
	blank := ""
	text := "hello"

	if got := preserveBlankString(nil, types.StringNull()); !got.IsNull() {
		t.Errorf("null API + null config should stay null, got %q", got.ValueString())
	}
	if got := preserveBlankString(&blank, types.StringNull()); !got.IsNull() {
		t.Errorf("empty API + null config should stay null, got %q", got.ValueString())
	}
	if got := preserveBlankString(nil, types.StringValue("")); got.IsNull() || got.ValueString() != "" {
		t.Errorf(`null API + "" config should stay "", got %v`, got)
	}
	if got := preserveBlankString(&text, types.StringNull()); got.ValueString() != "hello" {
		t.Errorf("expected 'hello', got %q", got.ValueString())
	}
	// Body cleared outside Terraform must surface as drift, not be masked.
	if got := preserveBlankString(nil, types.StringValue("hello")); !got.IsNull() {
		t.Errorf("expected null to surface out-of-band clearing, got %q", got.ValueString())
	}
}

// --- affectedItemsToState / planAffectedItemsToClient ---

func maintenanceWindowItemList(t *testing.T, items ...client.MaintenanceWindowAffectedItem) types.List {
	t.Helper()
	objType := types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}
	values := make([]attr.Value, len(items))
	for i, item := range items {
		values[i] = types.ObjectValueMust(maintenanceWindowItemAttrTypes, map[string]attr.Value{
			"item_type": types.StringValue(item.ItemType),
			"item_id":   types.StringValue(item.ItemID),
		})
	}
	return types.ListValueMust(objType, values)
}

func TestAffectedItemsToState_Populated(t *testing.T) {
	got := affectedItemsToState(
		[]client.MaintenanceWindowAffectedItem{{ItemType: "Host", ItemID: "host-uuid"}},
		types.ListNull(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}),
	)
	if len(got.Elements()) != 1 {
		t.Fatalf("expected 1 element, got %d", len(got.Elements()))
	}
	attrs := got.Elements()[0].(types.Object).Attributes()
	if attrs["item_type"].(types.String).ValueString() != "Host" {
		t.Errorf("unexpected item_type: %v", attrs["item_type"])
	}
	if attrs["item_id"].(types.String).ValueString() != "host-uuid" {
		t.Errorf("unexpected item_id: %v", attrs["item_id"])
	}
}

func TestAffectedItemsToState_EmptyKeepsNullVersusEmptyChoice(t *testing.T) {
	nullList := types.ListNull(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes})
	if got := affectedItemsToState(nil, nullList); !got.IsNull() {
		t.Error("expected an unconfigured list to stay null")
	}

	emptyList := maintenanceWindowItemList(t)
	got := affectedItemsToState(nil, emptyList)
	if got.IsNull() || len(got.Elements()) != 0 {
		t.Errorf("expected an explicitly empty list to stay empty, got %v", got)
	}
}

func TestAffectedItemsToState_ClearedOutOfBandSurfacesAsDrift(t *testing.T) {
	prior := maintenanceWindowItemList(t, client.MaintenanceWindowAffectedItem{ItemType: "Host", ItemID: "host-uuid"})
	if got := affectedItemsToState(nil, prior); !got.IsNull() {
		t.Errorf("expected null so the removal shows up in the plan, got %v", got)
	}
}

func TestPlanAffectedItemsToClient(t *testing.T) {
	nullList := types.ListNull(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes})
	// An unconfigured list must send [] so removing the block clears the items.
	if got := planAffectedItemsToClient(nullList); got == nil || len(got) != 0 {
		t.Errorf("expected an empty non-nil slice, got %#v", got)
	}

	list := maintenanceWindowItemList(t,
		client.MaintenanceWindowAffectedItem{ItemType: "UptimeMonitor", ItemID: "monitor-uuid"},
		client.MaintenanceWindowAffectedItem{ItemType: "Task", ItemID: "task-uuid"},
	)
	got := planAffectedItemsToClient(list)
	if len(got) != 2 || got[0].ItemType != "UptimeMonitor" || got[1].ItemID != "task-uuid" {
		t.Errorf("unexpected conversion: %#v", got)
	}
}

// --- parseMaintenanceWindowImportID ---

func TestParseMaintenanceWindowImportID(t *testing.T) {
	statusPageID, id, err := parseMaintenanceWindowImportID("12:77")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusPageID != 12 || id != 77 {
		t.Errorf("expected 12:77, got %d:%d", statusPageID, id)
	}

	for _, bad := range []string{"77", "12:77:3", "", ":77", "12:", "abc:77", "12:abc"} {
		if _, _, err := parseMaintenanceWindowImportID(bad); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}
}

// --- mapMaintenanceWindowToState ---

func TestMapMaintenanceWindowToState(t *testing.T) {
	body := "Read-only mode for two hours."
	window := &client.StatusPageMaintenanceWindow{
		ID:            77,
		StatusPageID:  12,
		Title:         "Database upgrade",
		Body:          &body,
		StartsAt:      "2026-09-02T00:00:00+02:00",
		EndsAt:        "2026-09-02T02:00:00+02:00",
		TimeZone:      "Europe/Paris",
		Status:        "scheduled",
		State:         "in_progress",
		AffectedItems: []client.MaintenanceWindowAffectedItem{{ItemType: "Host", ItemID: "host-uuid"}},
		CreatedAt:     "2026-09-01T00:00:00Z",
		UpdatedAt:     "2026-09-01T00:00:00Z",
	}

	// The model arrives holding the plan, as it does during Create and Update.
	state := &statusPageMaintenanceWindowModel{
		StatusPageID:  types.Int64Value(12),
		StartsAt:      types.StringValue("2026-09-01T22:00:00Z"),
		EndsAt:        types.StringValue("2026-09-02T00:00:00Z"),
		Body:          types.StringValue(body),
		AffectedItems: maintenanceWindowItemList(t, client.MaintenanceWindowAffectedItem{ItemType: "Host", ItemID: "host-uuid"}),
	}
	mapMaintenanceWindowToState(window, state)

	if state.ID.ValueInt64() != 77 {
		t.Errorf("expected id 77, got %d", state.ID.ValueInt64())
	}
	if state.StatusPageID.ValueInt64() != 12 {
		t.Errorf("expected status_page_id 12, got %d", state.StatusPageID.ValueInt64())
	}
	if state.Title.ValueString() != "Database upgrade" {
		t.Errorf("unexpected title: %q", state.Title.ValueString())
	}
	// Equivalent instants keep the practitioner's own formatting.
	if state.StartsAt.ValueString() != "2026-09-01T22:00:00Z" {
		t.Errorf("expected starts_at to keep the configured format, got %q", state.StartsAt.ValueString())
	}
	if state.EndsAt.ValueString() != "2026-09-02T00:00:00Z" {
		t.Errorf("expected ends_at to keep the configured format, got %q", state.EndsAt.ValueString())
	}
	if state.TimeZone.ValueString() != "Europe/Paris" {
		t.Errorf("unexpected time_zone: %q", state.TimeZone.ValueString())
	}
	if state.Status.ValueString() != "scheduled" || state.State.ValueString() != "in_progress" {
		t.Errorf("unexpected status/state: %q/%q", state.Status.ValueString(), state.State.ValueString())
	}
	if len(state.AffectedItems.Elements()) != 1 {
		t.Errorf("expected 1 affected item, got %d", len(state.AffectedItems.Elements()))
	}
}

func TestMapMaintenanceWindowToState_NullBodyAndItems(t *testing.T) {
	window := &client.StatusPageMaintenanceWindow{
		ID:        78,
		Title:     "Quick restart",
		StartsAt:  "2026-09-02T00:00:00Z",
		EndsAt:    "2026-09-02T00:30:00Z",
		TimeZone:  "UTC",
		Status:    "scheduled",
		State:     "scheduled",
		CreatedAt: "2026-09-01T00:00:00Z",
		UpdatedAt: "2026-09-01T00:00:00Z",
	}

	state := &statusPageMaintenanceWindowModel{
		StatusPageID:  types.Int64Value(12),
		StartsAt:      types.StringValue("2026-09-02T00:00:00Z"),
		EndsAt:        types.StringValue("2026-09-02T00:30:00Z"),
		Body:          types.StringNull(),
		AffectedItems: types.ListNull(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}),
	}
	mapMaintenanceWindowToState(window, state)

	if !state.Body.IsNull() {
		t.Errorf("expected body to stay null, got %q", state.Body.ValueString())
	}
	if !state.AffectedItems.IsNull() {
		t.Errorf("expected affected_items to stay null, got %v", state.AffectedItems)
	}
}

// --- schema & ValidateConfig ---

func TestStatusPageMaintenanceWindowResource_SchemaImplementation(t *testing.T) {
	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %+v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema implementation diagnostics: %+v", diags)
	}
}

func statusPageMaintenanceWindowConfig(t *testing.T, startsAt, endsAt string) tfsdk.Config {
	t.Helper()

	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %+v", resp.Diagnostics)
	}

	// Start from an all-null object so the helper keeps working as attributes
	// are added, then fill in only what the validation looks at.
	objType := resp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	values["status_page_id"] = tftypes.NewValue(tftypes.Number, int64(12))
	values["title"] = tftypes.NewValue(tftypes.String, "Database upgrade")
	values["starts_at"] = tftypes.NewValue(tftypes.String, startsAt)
	values["ends_at"] = tftypes.NewValue(tftypes.String, endsAt)

	return tfsdk.Config{Schema: resp.Schema, Raw: tftypes.NewValue(objType, values)}
}

func TestStatusPageMaintenanceWindowResource_ValidateConfig(t *testing.T) {
	tests := []struct {
		name             string
		startsAt, endsAt string
		wantError        bool
	}{
		{"ordered utc", "2026-09-01T22:00:00Z", "2026-09-02T00:00:00Z", false},
		{"ordered bare local", "2026-09-02T00:00:00", "2026-09-02T02:00:00", false},
		{"inverted utc", "2026-09-02T00:00:00Z", "2026-09-01T22:00:00Z", true},
		{"inverted bare local", "2026-09-02T02:00:00", "2026-09-02T00:00:00", true},
		{"equal", "2026-09-01T22:00:00Z", "2026-09-01T22:00:00Z", true},
		// The page timezone decides the ordering, so mixed forms are left to the API.
		{"mixed offset information", "2026-09-02T00:00:00", "2026-09-01T22:00:00Z", false},
		// An offset that actually resolves the apparent inversion.
		{"inverted wall clock, ordered instants", "2026-09-02T00:00:00+02:00", "2026-09-01T23:00:00Z", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewStatusPageMaintenanceWindowResource().(interface {
				ValidateConfig(context.Context, fwresource.ValidateConfigRequest, *fwresource.ValidateConfigResponse)
			})
			resp := &fwresource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), fwresource.ValidateConfigRequest{
				Config: statusPageMaintenanceWindowConfig(t, tt.startsAt, tt.endsAt),
			}, resp)

			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("HasError() = %v, want %v (diagnostics: %+v)", got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}

func TestStatusPageMaintenanceWindowResource_ValidateConfig_NullConfig(t *testing.T) {
	ctx := context.Background()
	schemaResp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, schemaResp)

	r := NewStatusPageMaintenanceWindowResource().(interface {
		ValidateConfig(context.Context, fwresource.ValidateConfigRequest, *fwresource.ValidateConfigResponse)
	})
	resp := &fwresource.ValidateConfigResponse{}
	r.ValidateConfig(ctx, fwresource.ValidateConfigRequest{
		Config: tfsdk.Config{
			Schema: schemaResp.Schema,
			Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
		},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("a null config must not error: %+v", resp.Diagnostics)
	}
}
