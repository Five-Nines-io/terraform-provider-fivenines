package resources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &dashboardSectionResource{}
	_ resource.ResourceWithImportState = &dashboardSectionResource{}
)

type dashboardSectionResource struct {
	client *client.Client
}

type dashboardSectionModel struct {
	ID          types.Int64  `tfsdk:"id"`
	DashboardID types.Int64  `tfsdk:"dashboard_id"`
	Name        types.String `tfsdk:"name"`
	Collapsed   types.Bool   `tfsdk:"collapsed"`
	Position    types.Int64  `tfsdk:"position"`
}

func NewDashboardSectionResource() resource.Resource {
	return &dashboardSectionResource{}
}

func (r *dashboardSectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard_section"
}

func (r *dashboardSectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a collapsible section (a Grafana-style row) on a FiveNines dashboard. " +
			"Panels reference their section by name, not by id, which is what makes a dashboard " +
			"definition portable between organizations. Deleting a section never deletes its panels: " +
			"they return to the ungrouped grid at the top of the dashboard.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"dashboard_id": schema.Int64Attribute{
				Description: "ID of the dashboard this section belongs to. Changing it replaces the section.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the section, at most 100 characters. This is the handle a panel's " +
					"`section` attribute references, and it must be unique within the dashboard. " +
					"Stored stripped of surrounding whitespace.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 100),
					trimmedStringValidator(),
				},
			},
			"collapsed": schema.BoolAttribute{
				Description: "Whether the section renders collapsed by default.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"position": schema.Int64Attribute{
				Description: "Zero-based render order. Leave unset to append the section at the bottom. " +
					"The API treats this as the index the section should end up at (not a relative move), " +
					"and it renumbers every sibling to match — so declaring positions on some sections of a " +
					"dashboard and not others, or creating several at once, can take a second apply to settle.",
				Optional: true,
				Computed: true,
			},
		},
	}
}

func (r *dashboardSectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *dashboardSectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dashboardSectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The CONFIG, not the plan: position is Optional+Computed, so an unconfigured
	// one plans to whatever the server last reported. Sending that back would
	// reorder the dashboard on every apply.
	var configPosition types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("position"), &configPosition)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboardID := plan.DashboardID.ValueInt64()
	input := client.DashboardSectionInput{
		Name:      plan.Name.ValueString(),
		Collapsed: nullableBool(plan.Collapsed),
		Position:  nullableInt64(configPosition),
	}

	tflog.Debug(ctx, "Creating dashboard section", map[string]interface{}{
		"dashboard_id": dashboardID,
		"name":         input.Name,
	})

	section, err := r.client.CreateDashboardSection(ctx, dashboardID, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating dashboard section", err.Error())
		return
	}

	mapDashboardSectionToState(section, dashboardID, &plan, true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dashboardSectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dashboardSectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboardID := state.DashboardID.ValueInt64()

	// Sections have no GET of their own — the dashboard definition lists them.
	dashboard, _, err := r.client.GetDashboard(ctx, dashboardID)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading dashboard section", err.Error())
		return
	}

	id := state.ID.ValueInt64()
	for i := range dashboard.Sections {
		if dashboard.Sections[i].ID == id {
			mapDashboardSectionToState(&dashboard.Sections[i], dashboardID, &state, false)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *dashboardSectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dashboardSectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state dashboardSectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var configPosition types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("position"), &configPosition)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboardID := plan.DashboardID.ValueInt64()
	id := state.ID.ValueInt64()
	input := client.DashboardSectionInput{
		Name:      plan.Name.ValueString(),
		Collapsed: nullableBool(plan.Collapsed),
		Position:  nullableInt64(configPosition),
	}

	section, err := r.client.UpdateDashboardSection(ctx, dashboardID, id, input)
	if err != nil {
		resp.Diagnostics.AddError("Error updating dashboard section", err.Error())
		return
	}

	mapDashboardSectionToState(section, dashboardID, &plan, true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dashboardSectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dashboardSectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting dashboard section", map[string]interface{}{
		"dashboard_id": state.DashboardID.ValueInt64(),
		"id":           state.ID.ValueInt64(),
	})

	err := r.client.DeleteDashboardSection(ctx, state.DashboardID.ValueInt64(), state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting dashboard section", err.Error())
	}
}

func (r *dashboardSectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	dashboardID, id, err := parseNestedDashboardID(req.ID, "section")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dashboard_id"), types.Int64Value(dashboardID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

// mapDashboardSectionToState copies the API's view onto state.
//
// keepPlannedPosition is what makes an apply survive the server's resequencing.
// Creating several sections at once means each one is inserted and clamped
// against however many existed at that moment, so the position that comes back
// can differ from the one that was planned - and Terraform rejects an apply
// whose result differs from its plan. On the write paths the planned value
// wins; a refresh takes the server's number, so the difference shows up as
// ordinary drift and the next apply reconciles it.
func mapDashboardSectionToState(s *client.DashboardSection, dashboardID int64, state *dashboardSectionModel, keepPlannedPosition bool) {
	state.ID = types.Int64Value(s.ID)
	state.DashboardID = types.Int64Value(dashboardID)
	state.Name = types.StringValue(s.Name)
	state.Collapsed = types.BoolValue(s.Collapsed)
	if !keepPlannedPosition || state.Position.IsNull() || state.Position.IsUnknown() {
		state.Position = types.Int64Value(s.Position)
	}
}

// parseNestedDashboardID splits the "<dashboard_id>:<id>" form the nested
// dashboard resources import with.
func parseNestedDashboardID(importID, kind string) (int64, int64, error) {
	parts := strings.Split(importID, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected %q in the form \"<dashboard_id>:<%s_id>\", got %q", importID, kind, importID)
	}
	dashboardID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse dashboard id %q as int64: %s", parts[0], err)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse %s id %q as int64: %s", kind, parts[1], err)
	}
	return dashboardID, id, nil
}

func nullableBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func nullableInt64(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}
