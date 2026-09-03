package resources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &organizationResource{}
	_ resource.ResourceWithImportState = &organizationResource{}
)

type organizationResource struct {
	client *client.Client
}

type organizationModel struct {
	ID                      types.Int64  `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Slug                    types.String `tfsdk:"slug"`
	DisplayName             types.String `tfsdk:"display_name"`
	Plan                    types.String `tfsdk:"plan"`
	Trialing                types.Bool   `tfsdk:"trialing"`
	SeatsUsed               types.Int64  `tfsdk:"seats_used"`
	SeatsTotal              types.Int64  `tfsdk:"seats_total"`
	SeatsRemaining          types.Int64  `tfsdk:"seats_remaining"`
	MembersCount            types.Int64  `tfsdk:"members_count"`
	PendingInvitationsCount types.Int64  `tfsdk:"pending_invitations_count"`
	CreatedAt               types.String `tfsdk:"created_at"`
	UpdatedAt               types.String `tfsdk:"updated_at"`
}

func NewOrganizationResource() resource.Resource {
	return &organizationResource{}
}

func (r *organizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *organizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the settings of the organization the API key belongs to. " +
			"An API key resolves exactly one organization, so this is a singleton: there is no " +
			"create and no delete, and only `name` is writable. Applying it renames the organization; " +
			"destroying it removes the resource from Terraform state and leaves the organization untouched. " +
			"Requires the `admin` or `owner` role on a plan that includes team features.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Organization ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the organization. The only writable field.",
				Required:    true,
			},
			"slug": schema.StringAttribute{
				Description: "Stable URL identifier. Read-only.",
				Computed:    true,
			},
			"display_name": schema.StringAttribute{
				Description: "`name` when set, otherwise `slug`. What the dashboard renders.",
				Computed:    true,
			},
			"plan": schema.StringAttribute{
				Description: "Effective plan — a trialing organization reads as the plan it is trialing.",
				Computed:    true,
			},
			"trialing": schema.BoolAttribute{
				Description: "Whether the organization is in a trial.",
				Computed:    true,
			},
			"seats_used": schema.Int64Attribute{
				Description: "Members plus pending invitations — an unaccepted invite holds a seat.",
				Computed:    true,
			},
			"seats_total": schema.Int64Attribute{
				Description: "Total seats on the plan. Null when the plan is unmetered.",
				Computed:    true,
			},
			"seats_remaining": schema.Int64Attribute{
				Description: "Seats still available. Check this before inviting rather than parsing a 422.",
				Computed:    true,
			},
			"members_count": schema.Int64Attribute{
				Description: "Number of members.",
				Computed:    true,
			},
			"pending_invitations_count": schema.Int64Attribute{
				Description: "Number of invitations sent but not yet accepted.",
				Computed:    true,
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

func (r *organizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create adopts the organization the API key already resolves to and applies
// the configured name. The API has no create endpoint — an organization always
// exists — so this is a PATCH, not a POST.
func (r *organizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Adopting organization", map[string]interface{}{"name": plan.Name.ValueString()})

	org := r.updateName(ctx, plan.Name.ValueString(), &resp.Diagnostics)
	if org == nil {
		return
	}

	mapOrganizationToState(org, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *organizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, _, err := r.client.GetOrganization(ctx)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading organization", err.Error())
		return
	}

	mapOrganizationToState(org, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *organizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan organizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org := r.updateName(ctx, plan.Name.ValueString(), &resp.Diagnostics)
	if org == nil {
		return
	}

	mapOrganizationToState(org, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op: an organization cannot be deleted through the API, and the
// resource only ever owned its name. Terraform drops it from state.
func (r *organizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Debug(ctx, "Removing organization from state; the organization itself is not deleted")
}

// ImportState ignores the import ID — an API key resolves exactly one
// organization, so there is nothing to address.
func (r *organizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	org, _, err := r.client.GetOrganization(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error importing organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(org.ID))...)
}

// updateName PATCHes the organization name, refreshing the ETag between
// attempts when a concurrent write invalidates it.
func (r *organizationResource) updateName(ctx context.Context, name string, diags *diag.Diagnostics) *client.Organization {
	input := client.UpdateOrganizationInput{Name: &name}

	for attempt := 0; attempt < 3; attempt++ {
		current, etag, err := r.client.GetOrganization(ctx)
		if err != nil {
			diags.AddError("Error reading organization for update", err.Error())
			return nil
		}
		// Adoption of an already-correctly-named organization is the common case
		// on a first apply, and it needs no write.
		if current.Name != nil && *current.Name == name {
			return current
		}
		org, err := r.client.UpdateOrganization(ctx, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on organization update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			diags.AddError("Error updating organization", err.Error())
			return nil
		}
		return org
	}
	// Unreachable while the loop bound and the retry guard agree; a diagnostic
	// here costs nothing and stops a future edit to either from returning from
	// an apply with no state and no error.
	diags.AddError(
		"Error updating organization",
		"The organization was still being written concurrently after three attempts. Re-run the apply.",
	)
	return nil
}

func mapOrganizationToState(o *client.Organization, state *organizationModel) {
	state.ID = types.Int64Value(o.ID)
	state.Name = optionalString(o.Name)
	state.Slug = types.StringValue(o.Slug)
	state.DisplayName = types.StringValue(o.DisplayName)
	state.Plan = types.StringValue(o.Plan)
	state.Trialing = types.BoolValue(o.Trialing)
	state.SeatsUsed = types.Int64Value(o.SeatsUsed)
	state.SeatsTotal = optionalInt64(o.SeatsTotal)
	state.SeatsRemaining = types.Int64Value(o.SeatsRemaining)
	state.MembersCount = types.Int64Value(o.MembersCount)
	state.PendingInvitationsCount = types.Int64Value(o.PendingInvitationsCount)
	state.CreatedAt = types.StringValue(o.CreatedAt)
	state.UpdatedAt = types.StringValue(o.UpdatedAt)
}
