package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &statusPageResource{}
	_ resource.ResourceWithImportState = &statusPageResource{}
)

type statusPageResource struct {
	client *client.Client
}

type statusPageModel struct {
	ID                      types.Int64  `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Slug                    types.String `tfsdk:"slug"`
	Description             types.String `tfsdk:"description"`
	Public                  types.Bool   `tfsdk:"public"`
	Uptime                  types.Bool   `tfsdk:"uptime"`
	CustomDomain            types.String `tfsdk:"custom_domain"`
	CustomDomainEnabled     types.Bool   `tfsdk:"custom_domain_enabled"`
	CustomFooter            types.String `tfsdk:"custom_footer"`
	CustomFooterEnabled     types.Bool   `tfsdk:"custom_footer_enabled"`
	IncidentsHistoryEnabled types.Bool   `tfsdk:"incidents_history_enabled"`
	ThemeVariant            types.String `tfsdk:"theme_variant"`
	Items                   types.List   `tfsdk:"items"`
	CreatedAt               types.String `tfsdk:"created_at"`
	UpdatedAt               types.String `tfsdk:"updated_at"`
}

var statusPageItemAttrTypes = map[string]attr.Type{
	"item_type": types.StringType,
	"item_id":   types.StringType,
}

func NewStatusPageResource() resource.Resource {
	return &statusPageResource{}
}

func (r *statusPageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *statusPageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines status page.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the status page.",
				Required:    true,
			},
			"slug": schema.StringAttribute{
				Description: "URL slug (auto-generated if not provided).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				Description: "Description of the status page.",
				Optional:    true,
				Computed:    true,
			},
			"public": schema.BoolAttribute{
				Description: "Whether the status page is publicly accessible.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"uptime": schema.BoolAttribute{
				Description: "Whether to show uptime percentages.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"custom_domain": schema.StringAttribute{
				Description: "Custom domain for the status page.",
				Optional:    true,
				Computed:    true,
			},
			"custom_domain_enabled": schema.BoolAttribute{
				Description: "Whether custom domain is enabled (requires plan upgrade).",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"custom_footer": schema.StringAttribute{
				Description: "Custom footer HTML.",
				Optional:    true,
				Computed:    true,
			},
			"custom_footer_enabled": schema.BoolAttribute{
				Description: "Whether custom footer is enabled (requires plan upgrade).",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"incidents_history_enabled": schema.BoolAttribute{
				Description: "Whether to show incidents history.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"theme_variant": schema.StringAttribute{
				Description: "Theme variant (system, dark, light).",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("system"),
				Validators: []validator.String{
					stringvalidator.OneOf("system", "dark", "light"),
				},
			},
			"items": schema.ListNestedAttribute{
				Description: "Items displayed on the status page, in order.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"item_type": schema.StringAttribute{
							Description: "Type of item (Host, UptimeMonitor, Task).",
							Required:    true,
							Validators: []validator.String{
								stringvalidator.OneOf("Host", "UptimeMonitor", "Task"),
							},
						},
						"item_id": schema.StringAttribute{
							Description: "UUID of the item.",
							Required:    true,
						},
					},
				},
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

func (r *statusPageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *statusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan statusPageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateStatusPageInput{
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		input.Description = plan.Description.ValueString()
	}
	if !plan.Public.IsNull() {
		v := plan.Public.ValueBool()
		input.Public = &v
	}
	if !plan.Uptime.IsNull() {
		v := plan.Uptime.ValueBool()
		input.Uptime = &v
	}
	if !plan.CustomDomain.IsNull() && !plan.CustomDomain.IsUnknown() {
		input.CustomDomain = plan.CustomDomain.ValueString()
	}
	if !plan.CustomDomainEnabled.IsNull() {
		v := plan.CustomDomainEnabled.ValueBool()
		input.CustomDomainEnabled = &v
	}
	if !plan.CustomFooter.IsNull() && !plan.CustomFooter.IsUnknown() {
		input.CustomFooter = plan.CustomFooter.ValueString()
	}
	if !plan.CustomFooterEnabled.IsNull() {
		v := plan.CustomFooterEnabled.ValueBool()
		input.CustomFooterEnabled = &v
	}
	if !plan.IncidentsHistoryEnabled.IsNull() {
		v := plan.IncidentsHistoryEnabled.ValueBool()
		input.IncidentsHistoryEnabled = &v
	}
	if !plan.ThemeVariant.IsNull() && !plan.ThemeVariant.IsUnknown() {
		input.ThemeVariant = plan.ThemeVariant.ValueString()
	}
	if !plan.Items.IsNull() && !plan.Items.IsUnknown() {
		input.Items = planItemsToClient(plan.Items)
	}

	tflog.Debug(ctx, "Creating status page", map[string]interface{}{"name": input.Name})

	page, err := r.client.CreateStatusPage(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating status page", err.Error())
		return
	}

	mapStatusPageToState(page, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	page, _, err := r.client.GetStatusPage(ctx, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading status page", err.Error())
		return
	}

	mapStatusPageToState(page, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *statusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan statusPageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()

	name := plan.Name.ValueString()
	input := client.UpdateStatusPageInput{
		Name: &name,
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		input.Description = &v
	}
	if !plan.Public.IsNull() {
		v := plan.Public.ValueBool()
		input.Public = &v
	}
	if !plan.Uptime.IsNull() {
		v := plan.Uptime.ValueBool()
		input.Uptime = &v
	}
	if !plan.CustomDomain.IsNull() && !plan.CustomDomain.IsUnknown() {
		v := plan.CustomDomain.ValueString()
		input.CustomDomain = &v
	}
	if !plan.CustomDomainEnabled.IsNull() {
		v := plan.CustomDomainEnabled.ValueBool()
		input.CustomDomainEnabled = &v
	}
	if !plan.CustomFooter.IsNull() && !plan.CustomFooter.IsUnknown() {
		v := plan.CustomFooter.ValueString()
		input.CustomFooter = &v
	}
	if !plan.CustomFooterEnabled.IsNull() {
		v := plan.CustomFooterEnabled.ValueBool()
		input.CustomFooterEnabled = &v
	}
	if !plan.IncidentsHistoryEnabled.IsNull() {
		v := plan.IncidentsHistoryEnabled.ValueBool()
		input.IncidentsHistoryEnabled = &v
	}
	if !plan.ThemeVariant.IsNull() && !plan.ThemeVariant.IsUnknown() {
		v := plan.ThemeVariant.ValueString()
		input.ThemeVariant = &v
	}
	if !plan.Items.IsNull() && !plan.Items.IsUnknown() {
		input.Items = planItemsToClient(plan.Items)
	}

	// ETag retry loop
	var page *client.StatusPage
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetStatusPage(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading status page for update", err.Error())
			return
		}
		page, err = r.client.UpdateStatusPage(ctx, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on status page update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating status page", err.Error())
			return
		}
		break
	}

	mapStatusPageToState(page, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting status page", map[string]interface{}{"id": state.ID.ValueInt64()})

	err := r.client.DeleteStatusPage(ctx, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting status page", err.Error())
	}
}

func (r *statusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse %q as int64: %s", req.ID, err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

func mapStatusPageToState(p *client.StatusPage, state *statusPageModel) {
	state.ID = types.Int64Value(p.ID)
	state.Name = types.StringValue(p.Name)
	state.Slug = types.StringValue(p.Slug)
	state.Description = types.StringValue(p.Description)
	state.Public = types.BoolValue(p.Public)
	state.Uptime = types.BoolValue(p.Uptime)
	state.CustomDomain = types.StringValue(p.CustomDomain)
	state.CustomDomainEnabled = types.BoolValue(p.CustomDomainEnabled)
	state.CustomFooter = types.StringValue(p.CustomFooter)
	state.CustomFooterEnabled = types.BoolValue(p.CustomFooterEnabled)
	state.IncidentsHistoryEnabled = types.BoolValue(p.IncidentsHistoryEnabled)
	state.ThemeVariant = types.StringValue(p.ThemeVariant)
	state.CreatedAt = types.StringValue(p.CreatedAt)
	state.UpdatedAt = types.StringValue(p.UpdatedAt)

	if len(p.Items) > 0 {
		items := make([]attr.Value, len(p.Items))
		for i, item := range p.Items {
			items[i], _ = types.ObjectValue(statusPageItemAttrTypes, map[string]attr.Value{
				"item_type": types.StringValue(item.ItemType),
				"item_id":   types.StringValue(item.ItemID),
			})
		}
		state.Items, _ = types.ListValue(types.ObjectType{AttrTypes: statusPageItemAttrTypes}, items)
	} else {
		state.Items = types.ListNull(types.ObjectType{AttrTypes: statusPageItemAttrTypes})
	}
}

func planItemsToClient(itemsList types.List) []client.StatusPageItem {
	elements := itemsList.Elements()
	result := make([]client.StatusPageItem, len(elements))
	for i, elem := range elements {
		obj := elem.(types.Object)
		attrs := obj.Attributes()
		result[i] = client.StatusPageItem{
			ItemType: attrs["item_type"].(types.String).ValueString(),
			ItemID:   attrs["item_id"].(types.String).ValueString(),
			Position: i,
		}
	}
	return result
}
