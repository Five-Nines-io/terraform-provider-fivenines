package resources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &dashboardVisualizationResource{}
	_ resource.ResourceWithImportState = &dashboardVisualizationResource{}
)

// chartTypes is the panel library. The uptime family only renders availability
// metrics and gated metrics only render their own allowlist, so the API refuses
// a mismatch with the list of what fits — this validator only catches typos.
var chartTypes = []string{
	"line", "area", "bar", "stat", "gauge", "top_n", "pie", "table",
	"state_timeline", "heatmap_uptime",
}

var visualizationLayoutAttrTypes = map[string]attr.Type{
	"x": types.Int64Type,
	"y": types.Int64Type,
	"w": types.Int64Type,
	"h": types.Int64Type,
}

var visualizationTargetsAttrTypes = map[string]attr.Type{
	"hosts":           types.ListType{ElemType: types.StringType},
	"uptime_monitors": types.ListType{ElemType: types.StringType},
	"tasks":           types.ListType{ElemType: types.StringType},
	"network_devices": types.ListType{ElemType: types.StringType},
	"ceph_clusters":   types.ListType{ElemType: types.StringType},
}

var visualizationOptionsAttrTypes = map[string]attr.Type{
	"reducer":          types.StringType,
	"group_by":         types.StringType,
	"dimensions":       types.ListType{ElemType: types.StringType},
	"limit":            types.Int64Type,
	"stacked":          types.BoolType,
	"incident_overlay": types.BoolType,
	"sparkline":        types.BoolType,
	"max":              types.Float64Type,
}

type dashboardVisualizationResource struct {
	client *client.Client
}

type dashboardVisualizationModel struct {
	ID             types.Int64  `tfsdk:"id"`
	DashboardID    types.Int64  `tfsdk:"dashboard_id"`
	Title          types.String `tfsdk:"title"`
	Description    types.String `tfsdk:"description"`
	Metric         types.String `tfsdk:"metric"`
	ChartType      types.String `tfsdk:"chart_type"`
	Section        types.String `tfsdk:"section"`
	Layout         types.Object `tfsdk:"layout"`
	Targets        types.Object `tfsdk:"targets"`
	Options        types.Object `tfsdk:"options"`
	TargetKind     types.String `tfsdk:"target_kind"`
	QueryResources types.List   `tfsdk:"query_resources"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func NewDashboardVisualizationResource() resource.Resource {
	return &dashboardVisualizationResource{}
}

func (r *dashboardVisualizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard_visualization"
}

func (r *dashboardVisualizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages one panel on a FiveNines dashboard. Whether the metric exists, whether the " +
			"chart type can render it, whether the reducer applies and whether the right entity kind is " +
			"attached are all decided server-side by the same validations the dashboard's own form obeys, " +
			"so these fail at apply time rather than at plan time.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"dashboard_id": schema.Int64Attribute{
				Description: "ID of the dashboard this panel belongs to. Changing it replaces the panel.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"title": schema.StringAttribute{
				Description: "Panel title. Stored stripped of surrounding whitespace, and a blank one is " +
					"stored as no title at all — so write the trimmed value or leave it unset.",
				Optional:   true,
				Validators: []validator.String{trimmedStringValidator()},
			},
			"description": schema.StringAttribute{
				Description: "Panel description. Stored stripped of surrounding whitespace, and a blank one " +
					"is stored as no description at all.",
				Optional:   true,
				Validators: []validator.String{trimmedStringValidator()},
			},
			"metric": schema.StringAttribute{
				Description: "The metric this panel charts, e.g. `cpu_usage`. Must be one in the catalog; " +
					"the metric also decides which `targets` list the panel binds.",
				Required: true,
			},
			"chart_type": schema.StringAttribute{
				Description: "The panel type. Must be one the metric can render.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("line"),
				Validators: []validator.String{
					stringvalidator.OneOf(chartTypes...),
				},
			},
			"section": schema.StringAttribute{
				Description: "The NAME of a section on this dashboard, or unset for the ungrouped grid at " +
					"the top. A name that does not exist is an error — create the " +
					"`fivenines_dashboard_section` first and reference its `name`, which also gives " +
					"Terraform the dependency it needs to order the two.",
				Optional:   true,
				Validators: []validator.String{trimmedStringValidator()},
			},
			"layout": schema.SingleNestedAttribute{
				Description: "Grid geometry in 24-column gridstack space, relative to the panel's own " +
					"section. Omit it and the dashboard places the panel; `x + w` may not exceed 24.",
				Optional: true,
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"x": schema.Int64Attribute{
						Description: "Column, 0-23.",
						Optional:    true,
						Computed:    true,
						Validators:  []validator.Int64{int64validator.Between(0, 23)},
					},
					"y": schema.Int64Attribute{
						Description: "Row, 0-10000.",
						Optional:    true,
						Computed:    true,
						Validators:  []validator.Int64{int64validator.Between(0, 10000)},
					},
					"w": schema.Int64Attribute{
						Description: "Width in columns, 1-24. Defaults to 12 (half width).",
						Optional:    true,
						Computed:    true,
						Validators:  []validator.Int64{int64validator.Between(1, 24)},
					},
					"h": schema.Int64Attribute{
						Description: "Height in rows, 1-500. Defaults to 6.",
						Optional:    true,
						Computed:    true,
						Validators:  []validator.Int64{int64validator.Between(1, 500)},
					},
				},
			},
			"targets": schema.SingleNestedAttribute{
				Description: "The entities this panel charts, by kind. Send only the kind the metric binds " +
					"— `target_kind` says which that is, and attaching the wrong kind is rejected. " +
					"Org-wide metrics take no entities at all.",
				Optional: true,
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"hosts":           targetListAttribute("Instance (host) UUIDs."),
					"uptime_monitors": targetListAttribute("Uptime monitor UUIDs."),
					"tasks":           targetListAttribute("Task UUIDs."),
					"network_devices": targetListAttribute("Network device UUIDs."),
					"ceph_clusters":   targetListAttribute("Ceph cluster UUIDs."),
				},
			},
			"options": schema.SingleNestedAttribute{
				Description: "Panel settings. Which ones apply depends on the panel type; an inapplicable " +
					"one is stored and ignored. Leaving a setting unset means the panel renders the " +
					"metric's own default, and removing one from the configuration clears it back to that " +
					"default rather than leaving the old value behind.",
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"reducer": schema.StringAttribute{
						Description: "How a single-value or collection panel collapses a series. `sum` is " +
							"rejected for percentage metrics, and the availability metrics accept only " +
							"`avg`, `min` and `max`.",
						Optional: true,
						Validators: []validator.String{
							stringvalidator.OneOf("last", "avg", "max", "min", "sum"),
						},
					},
					"group_by": schema.StringAttribute{
						Description: "Which label breaks the metric into series (`instance`, " +
							"`instance_device`, `instance_container`, `if_index`, ...). Unset uses the " +
							"metric's default.",
						Optional: true,
					},
					"dimensions": schema.ListAttribute{
						Description: "For a multi-dimensional metric (`network_bytes` = recv + sent), which " +
							"dimensions to chart. Unset selects them all. A stat or gauge panel needs " +
							"exactly one.",
						Optional:    true,
						ElementType: types.StringType,
						Validators:  []validator.List{listvalidator.SizeAtMost(50)},
					},
					"limit": schema.Int64Attribute{
						Description: "Series cap for `top_n` and `pie` panels. Defaults to 10.",
						Optional:    true,
						Validators:  []validator.Int64{int64validator.Between(1, 1000)},
					},
					"stacked": schema.BoolAttribute{
						Description: "Stack the series on a time-series panel.",
						Optional:    true,
					},
					"incident_overlay": schema.BoolAttribute{
						Description: "Shade incident windows over a time-series panel.",
						Optional:    true,
					},
					"sparkline": schema.BoolAttribute{
						Description: "Draw a trend line under a `stat` or `gauge` panel. Needs a real time " +
							"series, so it does not apply to the availability metrics.",
						Optional: true,
					},
					"max": schema.Float64Attribute{
						Description: "Full-scale value for a `gauge` panel.",
						Optional:    true,
					},
				},
			},
			"target_kind": schema.StringAttribute{
				Description: "Which of the `targets` lists this metric binds, derived from the metric: " +
					"`organization`, `hosts`, `uptime_monitors`, `tasks`, `network_devices` or " +
					"`ceph_clusters`. `organization` means the metric is org-wide and takes no entities.",
				Computed: true,
			},
			"query_resources": schema.ListAttribute{
				Description: "What this panel reads from the metrics query API. Empty is meaningful: the " +
					"Postgres-backed metrics (uptime, SSL expiry, org incident and CVE stats) query no " +
					"time series at all.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp.",
				Computed:    true,
			},
		},
	}
}

func targetListAttribute(description string) schema.ListAttribute {
	return schema.ListAttribute{
		Description: description,
		Optional:    true,
		Computed:    true,
		ElementType: types.StringType,
		Validators:  []validator.List{listvalidator.SizeAtMost(2000)},
	}
}

func (r *dashboardVisualizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *client.Client.")
		return
	}
	r.client = c
}

func (r *dashboardVisualizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dashboardVisualizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboardID := plan.DashboardID.ValueInt64()
	input := visualizationInputFromPlan(&plan)

	tflog.Debug(ctx, "Creating dashboard visualization", map[string]interface{}{
		"dashboard_id": dashboardID,
		"metric":       input.Metric,
	})

	panel, err := r.client.CreateVisualization(ctx, dashboardID, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating dashboard visualization", err.Error())
		return
	}

	mapVisualizationToState(panel, dashboardID, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dashboardVisualizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dashboardVisualizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboardID := state.DashboardID.ValueInt64()
	panel, _, err := r.client.GetVisualization(ctx, dashboardID, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading dashboard visualization", err.Error())
		return
	}

	mapVisualizationToState(panel, dashboardID, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dashboardVisualizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dashboardVisualizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state dashboardVisualizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboardID := plan.DashboardID.ValueInt64()
	id := state.ID.ValueInt64()
	input := visualizationInputFromPlan(&plan)

	var panel *client.Visualization
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetVisualization(ctx, dashboardID, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading dashboard visualization for update", err.Error())
			return
		}
		panel, err = r.client.UpdateVisualization(ctx, dashboardID, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on visualization update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating dashboard visualization", err.Error())
			return
		}
		break
	}

	mapVisualizationToState(panel, dashboardID, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dashboardVisualizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dashboardVisualizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting dashboard visualization", map[string]interface{}{
		"dashboard_id": state.DashboardID.ValueInt64(),
		"id":           state.ID.ValueInt64(),
	})

	err := r.client.DeleteVisualization(ctx, state.DashboardID.ValueInt64(), state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting dashboard visualization", err.Error())
	}
}

func (r *dashboardVisualizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	dashboardID, id, err := parseCompositeInt64ID(req.ID, "dashboard_id", "visualization_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dashboard_id"), types.Int64Value(dashboardID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

func visualizationInputFromPlan(plan *dashboardVisualizationModel) client.VisualizationInput {
	return client.VisualizationInput{
		Title:       stringPtr(plan.Title),
		Description: stringPtr(plan.Description),
		Metric:      plan.Metric.ValueString(),
		ChartType:   plan.ChartType.ValueString(),
		Section:     stringPtr(plan.Section),
		Layout:      layoutFromPlan(plan.Layout),
		Targets:     targetsFromPlan(plan.Targets),
		Options:     optionsFromPlan(plan.Options),
	}
}

func layoutFromPlan(o types.Object) *client.VisualizationLayout {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	attrs := o.Attributes()
	return &client.VisualizationLayout{
		X: objectInt64(attrs, "x"),
		Y: objectInt64(attrs, "y"),
		W: objectInt64(attrs, "w"),
		H: objectInt64(attrs, "h"),
	}
}

func targetsFromPlan(o types.Object) *client.VisualizationTargetsInput {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	attrs := o.Attributes()
	return &client.VisualizationTargetsInput{
		Hosts:          objectStringSlicePtr(attrs, "hosts"),
		UptimeMonitors: objectStringSlicePtr(attrs, "uptime_monitors"),
		Tasks:          objectStringSlicePtr(attrs, "tasks"),
		NetworkDevices: objectStringSlicePtr(attrs, "network_devices"),
		CephClusters:   objectStringSlicePtr(attrs, "ceph_clusters"),
	}
}

// optionsFromPlan always returns a struct, never nil. Every field marshals even
// when nil, and an explicit null is how the API clears a setting back to the
// metric's default — so a removed `options` block clears the whole block rather
// than leaving stale settings on the panel.
func optionsFromPlan(o types.Object) *client.VisualizationOptions {
	options := &client.VisualizationOptions{}
	if o.IsNull() || o.IsUnknown() {
		return options
	}
	attrs := o.Attributes()
	options.Reducer = objectString(attrs, "reducer")
	options.GroupBy = objectString(attrs, "group_by")
	options.Dimensions = objectStringSlice(attrs, "dimensions")
	options.Limit = objectInt64(attrs, "limit")
	options.Stacked = objectBool(attrs, "stacked")
	options.IncidentOverlay = objectBool(attrs, "incident_overlay")
	options.Sparkline = objectBool(attrs, "sparkline")
	options.Max = objectFloat64(attrs, "max")
	return options
}

func mapVisualizationToState(v *client.Visualization, dashboardID int64, state *dashboardVisualizationModel) {
	// Read before overwriting: whether the configuration declares an `options`
	// block decides whether an all-null read maps to an empty block or to no
	// block at all, and only one of the two matches the plan.
	optionsDeclared := !state.Options.IsNull()

	state.ID = types.Int64Value(v.ID)
	state.DashboardID = types.Int64Value(dashboardID)
	state.Title = optionalString(v.Title)
	state.Description = optionalString(v.Description)
	state.Metric = types.StringValue(v.Metric)
	state.ChartType = types.StringValue(v.ChartType)
	state.Section = optionalString(v.Section)
	state.TargetKind = types.StringValue(v.TargetKind)
	state.Layout = layoutToObject(v.Layout)
	state.Targets = targetsToObject(v.Targets)
	state.Options = optionsToObject(v.Options, optionsDeclared)
	state.QueryResources = stringListValue(v.QueryResources)
	state.CreatedAt = types.StringValue(v.CreatedAt)
	state.UpdatedAt = types.StringValue(v.UpdatedAt)
}

func layoutToObject(l client.VisualizationLayout) types.Object {
	obj, _ := types.ObjectValue(visualizationLayoutAttrTypes, map[string]attr.Value{
		"x": optionalInt64(l.X),
		"y": optionalInt64(l.Y),
		"w": optionalInt64(l.W),
		"h": optionalInt64(l.H),
	})
	return obj
}

func targetsToObject(t client.VisualizationTargets) types.Object {
	obj, _ := types.ObjectValue(visualizationTargetsAttrTypes, map[string]attr.Value{
		"hosts":           stringListValue(t.Hosts),
		"uptime_monitors": stringListValue(t.UptimeMonitors),
		"tasks":           stringListValue(t.Tasks),
		"network_devices": stringListValue(t.NetworkDevices),
		"ceph_clusters":   stringListValue(t.CephClusters),
	})
	return obj
}

// optionsToObject maps the API's all-keys-present options object onto state. An
// option the panel does not store reads back as null, so a panel with no
// settings at all is indistinguishable from one whose block was never declared
// — declared decides which of the two this is.
func optionsToObject(o client.VisualizationOptions, declared bool) types.Object {
	values := map[string]attr.Value{
		"reducer":          optionalString(o.Reducer),
		"group_by":         optionalString(o.GroupBy),
		"dimensions":       optionalStringListValue(o.Dimensions),
		"limit":            optionalInt64(o.Limit),
		"stacked":          optionalBool(o.Stacked),
		"incident_overlay": optionalBool(o.IncidentOverlay),
		"sparkline":        optionalBool(o.Sparkline),
		"max":              optionalFloat64(o.Max),
	}

	if !declared {
		empty := true
		for _, v := range values {
			if !v.IsNull() {
				empty = false
				break
			}
		}
		if empty {
			return types.ObjectNull(visualizationOptionsAttrTypes)
		}
	}

	obj, _ := types.ObjectValue(visualizationOptionsAttrTypes, values)
	return obj
}

// --- types.Object accessors ---

func objectString(attrs map[string]attr.Value, key string) *string {
	v, ok := attrs[key].(types.String)
	if !ok || v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func objectInt64(attrs map[string]attr.Value, key string) *int64 {
	v, ok := attrs[key].(types.Int64)
	if !ok || v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

func objectBool(attrs map[string]attr.Value, key string) *bool {
	v, ok := attrs[key].(types.Bool)
	if !ok || v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func objectFloat64(attrs map[string]attr.Value, key string) *float64 {
	v, ok := attrs[key].(types.Float64)
	if !ok || v.IsNull() || v.IsUnknown() {
		return nil
	}
	f := v.ValueFloat64()
	return &f
}

func objectStringSlice(attrs map[string]attr.Value, key string) []string {
	v, ok := attrs[key].(types.List)
	if !ok || v.IsNull() || v.IsUnknown() {
		return nil
	}
	elements := v.Elements()
	out := make([]string, 0, len(elements))
	for _, e := range elements {
		s, ok := e.(types.String)
		if !ok || s.IsNull() || s.IsUnknown() {
			continue
		}
		out = append(out, s.ValueString())
	}
	return out
}

// objectStringSlicePtr distinguishes "not mentioned" from "explicitly empty":
// on PATCH a target kind that is omitted is left alone and one that is sent is
// replaced, so clearing a kind means sending an empty array, not dropping it.
func objectStringSlicePtr(attrs map[string]attr.Value, key string) *[]string {
	v, ok := attrs[key].(types.List)
	if !ok || v.IsNull() || v.IsUnknown() {
		return nil
	}
	out := objectStringSlice(attrs, key)
	if out == nil {
		out = []string{}
	}
	return &out
}
