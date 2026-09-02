package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- Schemas ---

func TestDashboardSchemas_ValidateImplementation(t *testing.T) {
	tests := map[string]resource.Resource{
		"fivenines_dashboard":               NewDashboardResource(),
		"fivenines_dashboard_section":       NewDashboardSectionResource(),
		"fivenines_dashboard_visualization": NewDashboardVisualizationResource(),
	}

	for name, r := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			resp := &resource.SchemaResponse{}
			r.Schema(ctx, resource.SchemaRequest{}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("schema errors: %v", resp.Diagnostics)
			}
			if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
				t.Errorf("invalid schema implementation: %v", diags)
			}
		})
	}
}

// --- mapDashboardToState ---

func TestMapDashboardToState(t *testing.T) {
	name := "Fleet health"
	shareURL := "https://fivenines.io/share/dashboard/abc"
	dashboard := &client.Dashboard{
		ID:                 12,
		Name:               &name,
		Description:        nil,
		Shared:             true,
		ShareURL:           &shareURL,
		SectionCount:       3,
		VisualizationCount: 14,
		CreatedAt:          "2026-01-15T10:00:00Z",
		UpdatedAt:          "2026-01-15T10:00:00Z",
	}

	state := &dashboardModel{TemplateSlug: types.StringValue("postgresql")}
	mapDashboardToState(dashboard, state)

	if state.ID.ValueInt64() != 12 {
		t.Errorf("expected id 12, got %d", state.ID.ValueInt64())
	}
	if state.Name.ValueString() != "Fleet health" {
		t.Errorf("expected name %q, got %q", "Fleet health", state.Name.ValueString())
	}
	if !state.Description.IsNull() {
		t.Error("expected a null description to stay null rather than normalize to an empty string")
	}
	if state.ShareURL.ValueString() != shareURL {
		t.Errorf("expected the share url, got %q", state.ShareURL.ValueString())
	}
	if state.VisualizationCount.ValueInt64() != 14 {
		t.Errorf("expected 14 panels, got %d", state.VisualizationCount.ValueInt64())
	}
	// template_slug is a create-time recipe with no server-side counterpart, so
	// a read must never blank it.
	if state.TemplateSlug.ValueString() != "postgresql" {
		t.Errorf("expected template_slug preserved, got %v", state.TemplateSlug)
	}
}

func TestMapDashboardToState_LegacyNullName(t *testing.T) {
	state := &dashboardModel{}
	mapDashboardToState(&client.Dashboard{ID: 12}, state)

	if !state.Name.IsNull() {
		t.Error("expected a legacy null name to stay null")
	}
	if !state.ShareURL.IsNull() {
		t.Error("expected an unshared dashboard to have a null share url")
	}
}

// --- mapDashboardSectionToState ---

func TestMapDashboardSectionToState_TakesServerPositionWhenUnplanned(t *testing.T) {
	section := &client.DashboardSection{ID: 41, Name: "Compute", Position: 2, Collapsed: true}

	state := &dashboardSectionModel{Position: types.Int64Null()}
	mapDashboardSectionToState(section, 12, state, true)

	if state.Position.ValueInt64() != 2 {
		t.Errorf("expected the server position 2, got %d", state.Position.ValueInt64())
	}
	if state.DashboardID.ValueInt64() != 12 {
		t.Errorf("expected dashboard_id 12, got %d", state.DashboardID.ValueInt64())
	}
	if !state.Collapsed.ValueBool() {
		t.Error("expected collapsed=true")
	}
}

func TestMapDashboardSectionToState_KeepsPlannedPosition(t *testing.T) {
	// Creating several sections at once makes the server resequence siblings, so
	// the position it reports back can differ from the one that was planned.
	// Terraform rejects an apply whose result differs from its plan, so the
	// planned value wins here and the next refresh surfaces the difference.
	section := &client.DashboardSection{ID: 41, Name: "Compute", Position: 1}

	state := &dashboardSectionModel{Position: types.Int64Value(2)}
	mapDashboardSectionToState(section, 12, state, true)

	if state.Position.ValueInt64() != 2 {
		t.Errorf("expected the planned position 2 to be kept, got %d", state.Position.ValueInt64())
	}
}

// --- parseCompositeInt64ID, via the dashboard import forms ---

func TestParseNestedDashboardID(t *testing.T) {
	dashboardID, id, err := parseCompositeInt64ID("12:88", "dashboard_id", "visualization_id")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if dashboardID != 12 || id != 88 {
		t.Errorf("expected 12/88, got %d/%d", dashboardID, id)
	}
}

func TestParseNestedDashboardID_Invalid(t *testing.T) {
	for _, importID := range []string{"12", "12:88:99", "", "abc:88", "12:abc", ":88", "12:"} {
		if _, _, err := parseCompositeInt64ID(importID, "dashboard_id", "section_id"); err == nil {
			t.Errorf("expected %q to be refused", importID)
		}
	}
}

// --- mapVisualizationToState ---

func newTestVisualization() *client.Visualization {
	title := "CPU usage"
	section := "Compute"
	reducer := "avg"
	stacked := false
	x, y, w, h := int64(0), int64(0), int64(12), int64(6)

	return &client.Visualization{
		ID:         88,
		Title:      &title,
		Metric:     "cpu_usage",
		TargetKind: "hosts",
		ChartType:  "line",
		Section:    &section,
		Layout:     client.VisualizationLayout{X: &x, Y: &y, W: &w, H: &h},
		Targets: client.VisualizationTargets{
			Hosts:          []string{"host-uuid-1"},
			UptimeMonitors: []string{},
			Tasks:          []string{},
			NetworkDevices: []string{},
			CephClusters:   []string{},
		},
		Options:        client.VisualizationOptions{Reducer: &reducer, Stacked: &stacked},
		QueryResources: []string{"cpu_usage"},
		CreatedAt:      "2026-01-15T10:00:00Z",
		UpdatedAt:      "2026-01-15T10:00:00Z",
	}
}

func TestMapVisualizationToState(t *testing.T) {
	state := &dashboardVisualizationModel{}
	mapVisualizationToState(newTestVisualization(), 12, state)

	if state.ID.ValueInt64() != 88 || state.DashboardID.ValueInt64() != 12 {
		t.Errorf("unexpected ids: %v/%v", state.ID, state.DashboardID)
	}
	if state.Section.ValueString() != "Compute" {
		t.Errorf("expected section Compute, got %q", state.Section.ValueString())
	}
	if !state.Description.IsNull() {
		t.Error("expected a null description to stay null")
	}
	if state.TargetKind.ValueString() != "hosts" {
		t.Errorf("expected target_kind hosts, got %q", state.TargetKind.ValueString())
	}
	if state.ChartType.ValueString() != "line" {
		t.Errorf("expected chart_type line, got %q", state.ChartType.ValueString())
	}
	if state.Metric.ValueString() != "cpu_usage" {
		t.Errorf("expected metric cpu_usage, got %q", state.Metric.ValueString())
	}
	if state.Title.ValueString() != "CPU usage" {
		t.Errorf("expected title, got %v", state.Title)
	}

	layout := state.Layout.Attributes()
	if layout["w"].(types.Int64).ValueInt64() != 12 || layout["h"].(types.Int64).ValueInt64() != 6 {
		t.Errorf("unexpected layout: %v", layout)
	}

	targets := state.Targets.Attributes()
	hosts := targets["hosts"].(types.List)
	if len(hosts.Elements()) != 1 {
		t.Errorf("expected one host, got %v", hosts)
	}
	// The API always sends all five kinds; an unused one is empty, not absent.
	monitors := targets["uptime_monitors"].(types.List)
	if monitors.IsNull() || len(monitors.Elements()) != 0 {
		t.Errorf("expected an empty uptime_monitors list, got %v", monitors)
	}

	if state.QueryResources.IsNull() || len(state.QueryResources.Elements()) != 1 {
		t.Errorf("unexpected query_resources: %v", state.QueryResources)
	}
}

func TestMapVisualizationToState_OptionsRenderWhenStored(t *testing.T) {
	state := &dashboardVisualizationModel{}
	mapVisualizationToState(newTestVisualization(), 12, state)

	if state.Options.IsNull() {
		t.Fatal("expected an options block for a panel that stores settings")
	}
	options := state.Options.Attributes()
	if options["reducer"].(types.String).ValueString() != "avg" {
		t.Errorf("expected reducer avg, got %v", options["reducer"])
	}
	if options["stacked"].(types.Bool).ValueBool() {
		t.Error("expected stacked=false to survive as a real false")
	}
	if !options["group_by"].(types.String).IsNull() {
		t.Error("expected an unstored option to read back as null")
	}
	if !options["dimensions"].(types.List).IsNull() {
		t.Error("expected unstored dimensions to read back as null, not an empty list")
	}
}

func TestMapVisualizationToState_OptionsNullWhenUndeclaredAndUnset(t *testing.T) {
	panel := newTestVisualization()
	panel.Options = client.VisualizationOptions{}

	state := &dashboardVisualizationModel{}
	mapVisualizationToState(panel, 12, state)

	if !state.Options.IsNull() {
		t.Errorf("expected no options block for a panel storing nothing, got %v", state.Options)
	}
}

func TestMapVisualizationToState_OptionsKeepShapeWhenDeclared(t *testing.T) {
	// A configuration carrying an empty `options = {}` plans an object, and an
	// apply that returned null there would be rejected as inconsistent.
	declared, diags := types.ObjectValue(visualizationOptionsAttrTypes, map[string]attr.Value{
		"reducer":          types.StringNull(),
		"group_by":         types.StringNull(),
		"dimensions":       types.ListNull(types.StringType),
		"limit":            types.Int64Null(),
		"stacked":          types.BoolNull(),
		"incident_overlay": types.BoolNull(),
		"sparkline":        types.BoolNull(),
		"max":              types.Float64Null(),
	})
	if diags.HasError() {
		t.Fatalf("building the declared object: %v", diags)
	}

	panel := newTestVisualization()
	panel.Options = client.VisualizationOptions{}

	state := &dashboardVisualizationModel{Options: declared}
	mapVisualizationToState(panel, 12, state)

	if state.Options.IsNull() {
		t.Error("expected a declared options block to stay an object")
	}
}

// --- visualizationInputFromPlan ---

func TestVisualizationInputFromPlan_AlwaysSendsOptions(t *testing.T) {
	plan := &dashboardVisualizationModel{
		Metric:    types.StringValue("cpu_usage"),
		ChartType: types.StringValue("line"),
	}

	input := visualizationInputFromPlan(plan, plan)

	// An absent options block must clear the panel's settings rather than leave
	// them behind, which means sending the block with explicit nulls.
	if input.Options == nil {
		t.Fatal("expected options to be sent even when the block is absent")
	}
	if input.Options.Reducer != nil || input.Options.Dimensions != nil {
		t.Errorf("expected every option to be null, got %+v", input.Options)
	}
	if input.Layout != nil {
		t.Error("expected an absent layout to be omitted so the dashboard places the panel")
	}
	if input.Targets != nil {
		t.Error("expected absent targets to be omitted")
	}
	if input.Title != nil || input.Section != nil {
		t.Error("expected absent title and section to be sent as null")
	}
}

func TestVisualizationInputFromPlan_TargetsAndLayout(t *testing.T) {
	targets, diags := types.ObjectValue(visualizationTargetsAttrTypes, map[string]attr.Value{
		"hosts":           stringListValue([]string{"host-uuid-1"}),
		"uptime_monitors": stringListValue(nil),
		"tasks":           types.ListNull(types.StringType),
		"network_devices": types.ListNull(types.StringType),
		"ceph_clusters":   types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("building targets: %v", diags)
	}

	layout, diags := types.ObjectValue(visualizationLayoutAttrTypes, map[string]attr.Value{
		"x": types.Int64Value(0),
		"y": types.Int64Null(),
		"w": types.Int64Value(24),
		"h": types.Int64Unknown(),
	})
	if diags.HasError() {
		t.Fatalf("building layout: %v", diags)
	}

	plan := &dashboardVisualizationModel{
		Metric:  types.StringValue("cpu_usage"),
		Targets: targets,
		Layout:  layout,
	}

	input := visualizationInputFromPlan(plan, plan)

	if input.Targets.Hosts == nil || len(*input.Targets.Hosts) != 1 {
		t.Errorf("expected one host, got %v", input.Targets.Hosts)
	}
	// An explicitly emptied kind is sent as an empty array; the API replaces
	// what it is given and leaves out what it is not.
	if input.Targets.UptimeMonitors == nil || len(*input.Targets.UptimeMonitors) != 0 {
		t.Errorf("expected an explicitly empty uptime_monitors, got %v", input.Targets.UptimeMonitors)
	}
	if input.Targets.Tasks != nil {
		t.Error("expected an unset target kind to be omitted")
	}

	if input.Layout.X == nil || *input.Layout.X != 0 {
		t.Errorf("expected x=0 to be sent, got %v", input.Layout.X)
	}
	if input.Layout.Y != nil || input.Layout.H != nil {
		t.Error("expected null and unknown layout fields to be omitted so the server's defaults apply")
	}
}

func TestOptionsFromPlanRoundTrip(t *testing.T) {
	options, diags := types.ObjectValue(visualizationOptionsAttrTypes, map[string]attr.Value{
		"reducer":          types.StringValue("max"),
		"group_by":         types.StringValue("instance_device"),
		"dimensions":       stringListValue([]string{"recv", "sent"}),
		"limit":            types.Int64Value(10),
		"stacked":          types.BoolValue(true),
		"incident_overlay": types.BoolNull(),
		"sparkline":        types.BoolValue(false),
		"max":              types.Float64Value(100),
	})
	if diags.HasError() {
		t.Fatalf("building options: %v", diags)
	}

	got := optionsFromPlan(options)

	if got.Reducer == nil || *got.Reducer != "max" {
		t.Errorf("unexpected reducer %v", got.Reducer)
	}
	if len(got.Dimensions) != 2 {
		t.Errorf("unexpected dimensions %v", got.Dimensions)
	}
	if got.Limit == nil || *got.Limit != 10 {
		t.Errorf("unexpected limit %v", got.Limit)
	}
	if got.Sparkline == nil || *got.Sparkline {
		t.Errorf("expected sparkline=false to be sent as false, got %v", got.Sparkline)
	}
	if got.IncidentOverlay != nil {
		t.Error("expected a null option to stay null so the server clears it")
	}
	if got.Max == nil || *got.Max != 100 {
		t.Errorf("unexpected max %v", got.Max)
	}
}

func TestMapDashboardSectionToState_RefreshTakesServerPosition(t *testing.T) {
	// A refresh has no plan to honour, so it must report what the dashboard
	// actually looks like — otherwise a section reordered by hand, or by a
	// sibling's resequencing, would never show up as drift.
	section := &client.DashboardSection{ID: 41, Name: "Compute", Position: 1}

	state := &dashboardSectionModel{Position: types.Int64Value(2)}
	mapDashboardSectionToState(section, 12, state, false)

	if state.Position.ValueInt64() != 1 {
		t.Errorf("expected the server position 1 on refresh, got %d", state.Position.ValueInt64())
	}
}

// --- trimmedStringValidator ---

func TestTrimmedStringValidator(t *testing.T) {
	tests := map[string]bool{
		"Compute":            true,
		"a":                  true,
		"Two words":          true,
		"Line one\nline two": true,
		" Compute":           false,
		"Compute ":           false,
		"":                   false,
		"   ":                false,
		"\nCompute":          false,
		"Compute\n":          false,
	}

	for value, want := range tests {
		req := validator.StringRequest{
			Path:           path.Root("name"),
			ConfigValue:    types.StringValue(value),
			PathExpression: path.MatchRoot("name"),
		}
		resp := &validator.StringResponse{}
		trimmedStringValidator().ValidateString(context.Background(), req, resp)

		if got := !resp.Diagnostics.HasError(); got != want {
			t.Errorf("trimmedStringValidator(%q) accepted=%v, want %v", value, got, want)
		}
	}
}

func TestTrimmedStringValidator_IgnoresNull(t *testing.T) {
	req := validator.StringRequest{
		Path:           path.Root("title"),
		ConfigValue:    types.StringNull(),
		PathExpression: path.MatchRoot("title"),
	}
	resp := &validator.StringResponse{}
	trimmedStringValidator().ValidateString(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("expected an unset optional attribute to pass, got %v", resp.Diagnostics)
	}
}

func TestMapVisualizationToState_ChartTypeComesFromTheAPI(t *testing.T) {
	// A panel whose chart_type always read back as the schema default would
	// plan an in-place update forever on every gauge, stat and table on the
	// dashboard, and the round-trip test above cannot see it because its
	// fixture happens to be a line chart.
	panel := newTestVisualization()
	panel.ChartType = "gauge"

	state := &dashboardVisualizationModel{}
	mapVisualizationToState(panel, 12, state)

	if state.ChartType.ValueString() != "gauge" {
		t.Errorf("expected chart_type gauge, got %q", state.ChartType.ValueString())
	}
}

func TestMapVisualizationToState_EmptyQueryResourcesIsNotNull(t *testing.T) {
	// An empty query_resources is a real answer, not an absent one: the
	// Postgres-backed metrics (uptime, SSL expiry, org incident and CVE stats)
	// query no time series at all.
	//
	// The nil case is the one that bites. The API always sends the key, so a
	// decoded [] is a non-nil empty slice and any mapper handles it; a response
	// that omits the key decodes to nil, and mapping THAT to a null list makes a
	// Computed attribute flip between null and [] across reads, which plans as a
	// diff on a panel nobody touched. Absent and empty are the same answer here.
	for name, resources := range map[string][]string{
		"empty array": {},
		"key omitted": nil,
	} {
		t.Run(name, func(t *testing.T) {
			panel := newTestVisualization()
			panel.Metric = "monitor_uptime"
			panel.QueryResources = resources

			state := &dashboardVisualizationModel{}
			mapVisualizationToState(panel, 12, state)

			if state.QueryResources.IsNull() {
				t.Fatal("expected an empty list, got null")
			}
			if n := len(state.QueryResources.Elements()); n != 0 {
				t.Errorf("expected an empty list, got %d elements", n)
			}
		})
	}
}

func TestMapDashboardSectionToState_NameAndCollapsedComeFromTheAPI(t *testing.T) {
	// Renaming a section in the dashboard UI has to show up as drift; a mapper
	// that echoed the plan back instead would hide it.
	section := &client.DashboardSection{ID: 41, Name: "Renamed in the UI", Position: 0, Collapsed: true}

	state := &dashboardSectionModel{
		Name:      types.StringValue("Compute"),
		Collapsed: types.BoolValue(false),
		Position:  types.Int64Null(),
	}
	mapDashboardSectionToState(section, 12, state, false)

	if state.Name.ValueString() != "Renamed in the UI" {
		t.Errorf("expected the server name, got %q", state.Name.ValueString())
	}
	if !state.Collapsed.ValueBool() {
		t.Error("expected the server collapsed value")
	}
}

func TestMapDashboardToState_CountsAndTimestamps(t *testing.T) {
	// section_count and visualization_count are the only signal a
	// template-built dashboard has drifted, since its panels are not managed.
	name := "Fleet"
	state := &dashboardModel{}
	mapDashboardToState(&client.Dashboard{
		ID: 12, Name: &name, SectionCount: 3, VisualizationCount: 14,
		CreatedAt: "2026-01-15T10:00:00Z", UpdatedAt: "2026-02-20T08:30:00Z",
	}, state)

	if state.SectionCount.ValueInt64() != 3 {
		t.Errorf("expected 3 sections, got %d", state.SectionCount.ValueInt64())
	}
	if state.VisualizationCount.ValueInt64() != 14 {
		t.Errorf("expected 14 panels, got %d", state.VisualizationCount.ValueInt64())
	}
	if state.CreatedAt.ValueString() != "2026-01-15T10:00:00Z" {
		t.Errorf("unexpected created_at %q", state.CreatedAt.ValueString())
	}
	if state.UpdatedAt.ValueString() != "2026-02-20T08:30:00Z" {
		t.Errorf("unexpected updated_at %q", state.UpdatedAt.ValueString())
	}
	if !state.Shared.Equal(types.BoolValue(false)) {
		t.Errorf("expected shared=false, got %v", state.Shared)
	}
}

func TestVisualizationInputFromPlan_SendsMetricAndChartType(t *testing.T) {
	plan := &dashboardVisualizationModel{
		Metric:    types.StringValue("memory_usage"),
		ChartType: types.StringValue("gauge"),
		Section:   types.StringValue("Compute"),
	}

	input := visualizationInputFromPlan(plan, plan)

	if input.Metric != "memory_usage" {
		t.Errorf("expected metric memory_usage, got %q", input.Metric)
	}
	if input.ChartType != "gauge" {
		t.Errorf("expected chart_type gauge, got %q", input.ChartType)
	}
	if input.Section == nil || *input.Section != "Compute" {
		t.Errorf("expected section Compute, got %v", input.Section)
	}
}

func TestVisualizationInputFromPlan_UnknownTargetKindIsOmitted(t *testing.T) {
	// On create, a target kind the configuration never mentions plans as
	// UNKNOWN, not null. Unknown is not a value: sending it as an explicit []
	// would tell the API to replace that kind with nothing, which on a PATCH is
	// a real detach rather than the no-op it looks like.
	targets, diags := types.ObjectValue(visualizationTargetsAttrTypes, map[string]attr.Value{
		"hosts":           stringListValue([]string{"host-uuid-1"}),
		"uptime_monitors": types.ListUnknown(types.StringType),
		"tasks":           types.ListUnknown(types.StringType),
		"network_devices": types.ListUnknown(types.StringType),
		"ceph_clusters":   types.ListUnknown(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("building targets: %v", diags)
	}

	declared := &dashboardVisualizationModel{
		Metric:  types.StringValue("cpu_usage"),
		Targets: targets,
	}
	input := visualizationInputFromPlan(declared, declared)

	if input.Targets.Hosts == nil || len(*input.Targets.Hosts) != 1 {
		t.Errorf("expected the configured hosts to be sent, got %v", input.Targets.Hosts)
	}
	for name, got := range map[string]*[]string{
		"uptime_monitors": input.Targets.UptimeMonitors,
		"tasks":           input.Targets.Tasks,
		"network_devices": input.Targets.NetworkDevices,
		"ceph_clusters":   input.Targets.CephClusters,
	} {
		if got != nil {
			t.Errorf("expected unknown %s to be omitted, got %v", name, *got)
		}
	}
}

// --- templateSkipWarning ---

func TestTemplateSkipWarning(t *testing.T) {
	summary, detail, ok := templateSkipWarning("postgresql", &client.DashboardTemplateResult{
		CreatedCount: 9,
		Skipped: []client.DashboardTemplateSkip{
			{Title: "Replication lag", Reason: "no instance runs PostgreSQL yet"},
			{Title: "Locks", Reason: "no instance runs PostgreSQL yet"},
			{Title: "Cache hit ratio", Reason: "no instance runs PostgreSQL yet"},
		},
		SkipSummary: []string{"3 panels skipped, no instance runs PostgreSQL yet"},
	})

	if !ok {
		t.Fatal("expected a warning when panels were skipped")
	}
	// 9 built + 3 skipped = the 12 the template declared. Reporting only the
	// built count would let "built 4 and dropped 13" read as a full success.
	if summary != `Dashboard template "postgresql" built 9 of 12 panels` {
		t.Errorf("unexpected summary %q", summary)
	}
	if !strings.Contains(detail, "3 panels skipped, no instance runs PostgreSQL yet") {
		t.Errorf("expected the reason in the detail, got %q", detail)
	}
}

func TestTemplateSkipWarning_SilentWhenNothingSkipped(t *testing.T) {
	if _, _, ok := templateSkipWarning("postgresql", &client.DashboardTemplateResult{CreatedCount: 12}); ok {
		t.Error("expected no warning when every panel was built")
	}
	if _, _, ok := templateSkipWarning("postgresql", nil); ok {
		t.Error("expected no warning for a nil result")
	}
}

func TestVisualizationInputFromPlan_UndeclaredTargetsAreNotSent(t *testing.T) {
	// The plan for an undeclared Optional+Computed block carries the prior
	// state, so `plan` here holds a full set of target kinds while `config`
	// holds none. Sending them would make an unrelated title edit replace the
	// panel's entities with Terraform's last known set.
	planned, diags := types.ObjectValue(visualizationTargetsAttrTypes, map[string]attr.Value{
		"hosts":           stringListValue([]string{"host-uuid-1"}),
		"uptime_monitors": stringListValue(nil),
		"tasks":           stringListValue(nil),
		"network_devices": stringListValue(nil),
		"ceph_clusters":   stringListValue(nil),
	})
	if diags.HasError() {
		t.Fatalf("building targets: %v", diags)
	}
	layout, diags := types.ObjectValue(visualizationLayoutAttrTypes, map[string]attr.Value{
		"x": types.Int64Value(0), "y": types.Int64Value(6),
		"w": types.Int64Value(12), "h": types.Int64Value(6),
	})
	if diags.HasError() {
		t.Fatalf("building layout: %v", diags)
	}

	plan := &dashboardVisualizationModel{
		Metric: types.StringValue("cpu_usage"), Targets: planned, Layout: layout,
		Title: types.StringValue("Renamed"),
	}
	config := &dashboardVisualizationModel{
		Metric: types.StringValue("cpu_usage"), Title: types.StringValue("Renamed"),
		Targets: types.ObjectNull(visualizationTargetsAttrTypes),
		Layout:  types.ObjectNull(visualizationLayoutAttrTypes),
	}

	input := visualizationInputFromPlan(plan, config)

	if input.Targets != nil {
		t.Errorf("expected undeclared targets to be omitted, got %+v", input.Targets)
	}
	// Same reasoning for the grid: a panel dragged in the dashboard must not be
	// shoved back to Terraform's last known coordinates by a title change.
	if input.Layout != nil {
		t.Errorf("expected an undeclared layout to be omitted, got %+v", input.Layout)
	}
	// The attribute the configuration DOES own still goes out.
	if input.Title == nil || *input.Title != "Renamed" {
		t.Errorf("expected the configured title to be sent, got %v", input.Title)
	}
}
