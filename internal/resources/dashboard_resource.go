package resources

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &dashboardResource{}
	_ resource.ResourceWithImportState = &dashboardResource{}
)

type dashboardResource struct {
	client *client.Client
}

type dashboardModel struct {
	ID                 types.Int64  `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	TemplateSlug       types.String `tfsdk:"template_slug"`
	Shared             types.Bool   `tfsdk:"shared"`
	ShareURL           types.String `tfsdk:"share_url"`
	SectionCount       types.Int64  `tfsdk:"section_count"`
	VisualizationCount types.Int64  `tfsdk:"visualization_count"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func NewDashboardResource() resource.Resource {
	return &dashboardResource{}
}

func (r *dashboardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard"
}

func (r *dashboardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines dashboard. Sections and panels are separate resources " +
			"(`fivenines_dashboard_section` and `fivenines_dashboard_visualization`) because the API " +
			"refuses to reconcile them from the dashboard endpoint: a rename must never be able to " +
			"silently delete a section it did not mention.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the dashboard. At most 255 characters, stored stripped of surrounding whitespace.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					trimmedStringValidator(),
				},
			},
			"description": schema.StringAttribute{
				Description: "Description of the dashboard. At most 2000 characters. Removing it clears the value.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(2000),
				},
			},
			"template_slug": schema.StringAttribute{
				Description: "Build the dashboard from a template in the gallery instead of creating it empty. " +
					"A slug from the `fivenines_dashboard_templates` data source. Create-only: changing it " +
					"replaces the dashboard. The panels and sections a template builds are NOT managed by " +
					"Terraform — panels the organization cannot feed are dropped at build time and reported " +
					"as a warning, and later drift in them is invisible beyond `section_count` and " +
					"`visualization_count`. Leave unset and declare panels as resources when you want them managed.",
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"shared": schema.BoolAttribute{
				Description: "Whether a public share link exists. Sharing is an action, not a managed field: " +
					"use the dashboard's Share button or the API. Reconcile this across tenants to audit " +
					"which dashboards are readable without signing in.",
				Computed: true,
			},
			"share_url": schema.StringAttribute{
				Description: "The public link, or null when the dashboard is not shared. The URL contains the " +
					"share slug, which is the credential.",
				Computed:  true,
				Sensitive: true,
			},
			"section_count": schema.Int64Attribute{
				Description: "How many collapsible sections the dashboard groups its panels into.",
				Computed:    true,
			},
			"visualization_count": schema.Int64Attribute{
				Description: "How many panels the dashboard holds, ungrouped ones included.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp. The dashboard row's own — editing a panel does not move it.",
				Computed:    true,
			},
		},
	}
}

func (r *dashboardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *dashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dashboardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.TemplateSlug.IsNull() && !plan.TemplateSlug.IsUnknown() {
		r.createFromTemplate(ctx, plan, resp)
		return
	}

	input := client.CreateDashboardInput{
		Name:        plan.Name.ValueString(),
		Description: nullableString(plan.Description),
	}

	tflog.Debug(ctx, "Creating dashboard", map[string]interface{}{"name": input.Name})

	dashboard, err := r.client.CreateDashboard(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating dashboard", err.Error())
		return
	}

	mapDashboardToState(dashboard, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// createFromTemplate builds the dashboard from the template gallery, then
// PATCHes its own fields.
//
// The second call is not optional. The template endpoint takes no description,
// and it DEDUPLICATES the name it is given ("PostgreSQL" becomes "PostgreSQL (2)"
// when the organization already has one) - so a dashboard built from a template
// can come back named something other than what was declared, which Terraform
// rejects as an inconsistent apply. The dashboard's own PATCH sets the name
// verbatim, which is what makes the declared name the one that sticks.
//
// State is saved before the PATCH runs, so a failure there leaves a dashboard
// Terraform still owns rather than an orphan.
func (r *dashboardResource) createFromTemplate(ctx context.Context, plan dashboardModel, resp *resource.CreateResponse) {
	slug := plan.TemplateSlug.ValueString()
	name := plan.Name.ValueString()

	tflog.Debug(ctx, "Instantiating dashboard template", map[string]interface{}{"slug": slug})

	result, err := r.client.InstantiateDashboardTemplate(ctx, client.InstantiateDashboardTemplateInput{
		Slug: slug,
		Name: name,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error instantiating dashboard template", err.Error())
		return
	}

	if len(result.SkipSummary) > 0 {
		declared := result.CreatedCount + int64(len(result.Skipped))
		resp.Diagnostics.AddWarning(
			fmt.Sprintf("Dashboard template %q built %d of %d panels", slug, result.CreatedCount, declared),
			"Panels the organization cannot feed are dropped rather than created blank:\n  "+
				strings.Join(result.SkipSummary, "\n  "),
		)
	}

	dashboard := &result.Dashboard
	mapDashboardToState(dashboard, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateDashboard(ctx, dashboard.ID, "", client.UpdateDashboardInput{
		Name:        name,
		Description: nullableString(plan.Description),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error applying name and description to the template-built dashboard",
			fmt.Sprintf("The dashboard was created (id %d) but its own fields could not be set: %s",
				dashboard.ID, err),
		)
		return
	}

	mapDashboardToState(updated, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dashboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dashboardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboard, _, err := r.client.GetDashboard(ctx, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading dashboard", err.Error())
		return
	}

	mapDashboardToState(dashboard, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dashboardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state dashboardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()
	input := client.UpdateDashboardInput{
		Name:        plan.Name.ValueString(),
		Description: nullableString(plan.Description),
	}

	var dashboard *client.Dashboard
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetDashboard(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading dashboard for update", err.Error())
			return
		}
		dashboard, err = r.client.UpdateDashboard(ctx, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on dashboard update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating dashboard", err.Error())
			return
		}
		break
	}

	mapDashboardToState(dashboard, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dashboardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()
	tflog.Debug(ctx, "Deleting dashboard", map[string]interface{}{"id": id})

	err := r.client.DeleteDashboard(ctx, id)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok && apiErr.StatusCode == 404 {
			return
		}
		if ok && apiErr.StatusCode == 422 {
			resp.Diagnostics.AddError(
				"Error deleting dashboard",
				fmt.Sprintf("%s\n\nThe API refuses to delete an organization's last dashboard, because the "+
					"dashboard navigation resolves to the first one and an organization with none has no "+
					"dashboard page at all. Create another dashboard first.", err),
			)
			return
		}
		resp.Diagnostics.AddError("Error deleting dashboard", err.Error())
	}
}

func (r *dashboardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse %q as int64: %s", req.ID, err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

// mapDashboardToState copies the API's view onto state. template_slug is
// deliberately untouched: it is a create-time recipe with no server-side
// counterpart to read back.
func mapDashboardToState(d *client.Dashboard, state *dashboardModel) {
	state.ID = types.Int64Value(d.ID)
	state.Name = optionalString(d.Name)
	state.Description = optionalString(d.Description)
	state.Shared = types.BoolValue(d.Shared)
	state.ShareURL = optionalString(d.ShareURL)
	state.SectionCount = types.Int64Value(d.SectionCount)
	state.VisualizationCount = types.Int64Value(d.VisualizationCount)
	state.CreatedAt = types.StringValue(d.CreatedAt)
	state.UpdatedAt = types.StringValue(d.UpdatedAt)
}

// nullableString turns a Terraform string into the pointer the dashboards API
// expects, where an explicit null clears the field and a missing key would
// leave it alone. Unknown is treated as null: nothing else can be sent.
func nullableString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// trimmedPattern matches a string with no leading or trailing whitespace and at
// least one non-whitespace character. `(?s:...)` so a multi-line description is
// judged as a whole rather than line by line.
var trimmedPattern = regexp.MustCompile(`\A\S(?s:.*\S)?\z`)

// trimmedStringValidator rejects a value the API would store differently from
// what was written. The dashboard endpoints strip these fields (and a panel's
// title and description also collapse blank to null), so an unstripped value
// reads back changed — which Terraform reports as the provider producing an
// inconsistent result, several steps away from the padding that caused it.
func trimmedStringValidator() validator.String {
	return stringvalidator.RegexMatches(
		trimmedPattern,
		"must not be empty and must not start or end with whitespace: the API stores it stripped, "+
			"so the value would read back changed",
	)
}
