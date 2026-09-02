package resources

import (
	"context"
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

	input := visualizationInputFromPlan(plan)

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

	input := visualizationInputFromPlan(plan)

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
