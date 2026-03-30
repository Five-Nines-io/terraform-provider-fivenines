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

// --- mapStatusPageToState ---

func TestMapStatusPageToState_AllFields(t *testing.T) {
	page := &client.StatusPage{
		ID:                      42,
		Name:                    "Public Status",
		Slug:                    "pub1",
		Description:             "Our status page",
		Public:                  true,
		Uptime:                  true,
		CustomDomain:            "status.example.com",
		CustomDomainEnabled:     true,
		CustomFooter:            "<p>Footer</p>",
		CustomFooterEnabled:     true,
		IncidentsHistoryEnabled: true,
		ThemeVariant:            "dark",
		Items: []client.StatusPageItem{
			{ItemType: "UptimeMonitor", ItemID: "mon-uuid", Position: 0},
			{ItemType: "Host", ItemID: "host-uuid", Position: 1},
		},
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-02T00:00:00Z",
	}

	state := &statusPageModel{}
	mapStatusPageToState(page, state)

	if state.ID.ValueInt64() != 42 {
		t.Errorf("expected ID 42, got %d", state.ID.ValueInt64())
	}
	if state.Name.ValueString() != "Public Status" {
		t.Errorf("expected name 'Public Status', got %s", state.Name.ValueString())
	}
	if state.Slug.ValueString() != "pub1" {
		t.Errorf("expected slug 'pub1', got %s", state.Slug.ValueString())
	}
	if state.Description.ValueString() != "Our status page" {
		t.Errorf("expected description 'Our status page', got %s", state.Description.ValueString())
	}
	if !state.Public.ValueBool() {
		t.Error("expected public=true")
	}
	if !state.Uptime.ValueBool() {
		t.Error("expected uptime=true")
	}
	if state.CustomDomain.ValueString() != "status.example.com" {
		t.Errorf("expected custom_domain 'status.example.com', got %s", state.CustomDomain.ValueString())
	}
	if !state.CustomDomainEnabled.ValueBool() {
		t.Error("expected custom_domain_enabled=true")
	}
	if state.CustomFooter.ValueString() != "<p>Footer</p>" {
		t.Errorf("expected custom_footer '<p>Footer</p>', got %s", state.CustomFooter.ValueString())
	}
	if !state.CustomFooterEnabled.ValueBool() {
		t.Error("expected custom_footer_enabled=true")
	}
	if !state.IncidentsHistoryEnabled.ValueBool() {
		t.Error("expected incidents_history_enabled=true")
	}
	if state.ThemeVariant.ValueString() != "dark" {
		t.Errorf("expected theme_variant 'dark', got %s", state.ThemeVariant.ValueString())
	}
	if state.CreatedAt.ValueString() != "2026-01-01T00:00:00Z" {
		t.Errorf("expected created_at '2026-01-01T00:00:00Z', got %s", state.CreatedAt.ValueString())
	}
	if state.UpdatedAt.ValueString() != "2026-01-02T00:00:00Z" {
		t.Errorf("expected updated_at '2026-01-02T00:00:00Z', got %s", state.UpdatedAt.ValueString())
	}
	// Items
	if state.Items.IsNull() {
		t.Fatal("expected items to be non-null")
	}
	elements := state.Items.Elements()
	if len(elements) != 2 {
		t.Fatalf("expected 2 items, got %d", len(elements))
	}
	item0 := elements[0].(types.Object)
	if item0.Attributes()["item_type"].(types.String).ValueString() != "UptimeMonitor" {
		t.Errorf("expected first item_type 'UptimeMonitor', got %s", item0.Attributes()["item_type"].(types.String).ValueString())
	}
	if item0.Attributes()["item_id"].(types.String).ValueString() != "mon-uuid" {
		t.Errorf("expected first item_id 'mon-uuid', got %s", item0.Attributes()["item_id"].(types.String).ValueString())
	}
}

func TestMapStatusPageToState_EmptyItems(t *testing.T) {
	page := &client.StatusPage{
		ID:           1,
		Name:         "Empty Page",
		Slug:         "empty",
		ThemeVariant: "system",
		Items:        []client.StatusPageItem{},
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}

	state := &statusPageModel{}
	mapStatusPageToState(page, state)

	if !state.Items.IsNull() {
		t.Error("expected items to be null when API returns empty items")
	}
}

func TestMapStatusPageToState_NilItems(t *testing.T) {
	page := &client.StatusPage{
		ID:           1,
		Name:         "No Items Page",
		Slug:         "none",
		ThemeVariant: "system",
		Items:        nil,
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}

	state := &statusPageModel{}
	mapStatusPageToState(page, state)

	if !state.Items.IsNull() {
		t.Error("expected items to be null when API returns nil items")
	}
}

func TestPlanItemsToClient(t *testing.T) {
	itemObjs := []client.StatusPageItem{
		{ItemType: "Host", ItemID: "host-1", Position: 0},
		{ItemType: "UptimeMonitor", ItemID: "mon-1", Position: 1},
		{ItemType: "Task", ItemID: "task-1", Position: 2},
	}

	// Build a types.List from these items (simulating what Terraform would provide)
	page := &client.StatusPage{
		ID:           1,
		Name:         "Test",
		Slug:         "test",
		ThemeVariant: "system",
		Items:        itemObjs,
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}
	state := &statusPageModel{}
	mapStatusPageToState(page, state)

	// Now convert back using planItemsToClient
	result := planItemsToClient(state.Items)

	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0].ItemType != "Host" || result[0].ItemID != "host-1" || result[0].Position != 0 {
		t.Errorf("item 0 mismatch: %+v", result[0])
	}
	if result[1].ItemType != "UptimeMonitor" || result[1].ItemID != "mon-1" || result[1].Position != 1 {
		t.Errorf("item 1 mismatch: %+v", result[1])
	}
	if result[2].ItemType != "Task" || result[2].ItemID != "task-1" || result[2].Position != 2 {
		t.Errorf("item 2 mismatch: %+v", result[2])
	}
}

// Verify types.String null behavior (framework contract test)
func TestTypesStringNull(t *testing.T) {
	s := types.StringNull()
	if !s.IsNull() {
		t.Error("expected IsNull() to be true")
	}
}
