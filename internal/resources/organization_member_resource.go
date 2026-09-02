package resources

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                = &organizationMemberResource{}
	_ resource.ResourceWithImportState = &organizationMemberResource{}
	_ resource.ResourceWithModifyPlan  = &organizationMemberResource{}
)

type organizationMemberResource struct {
	client *client.Client
}

type organizationMemberModel struct {
	ID               types.Int64  `tfsdk:"id"`
	UserID           types.Int64  `tfsdk:"user_id"`
	Email            types.String `tfsdk:"email"`
	Role             types.String `tfsdk:"role"`
	TwoFactorEnabled types.Bool   `tfsdk:"two_factor_enabled"`
	JoinedAt         types.String `tfsdk:"joined_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

func NewOrganizationMemberResource() resource.Resource {
	return &organizationMemberResource{}
}

func (r *organizationMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_member"
}

func (r *organizationMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the role of an existing organization member, and offboards them on destroy.\n\n" +
			"Members cannot be created through the API — people join by accepting an invitation. " +
			"Applying this resource for an address that is already a member **adopts** that membership " +
			"and brings its role under Terraform; for an address that is not, it fails and points you at " +
			"`fivenines_organization_invitation`.\n\n" +
			"**Destroying this resource offboards the person.** The API removes the membership and deletes " +
			"the user account in one transaction, which also destroys every API token that user owned. " +
			"Terraform validates the removal with a dry-run request during `plan`, so refusals (removing " +
			"yourself, removing the owner, a read-only key) surface before anything is applied.\n\n" +
			"Requires the `admin` or `owner` role on a plan that includes team features.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Membership ID. Not the user ID — a membership row disappears and reappears when someone leaves and rejoins.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"email": schema.StringAttribute{
				Description: "Email address of the member. Changing it offboards the old address and adopts the new one.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					requiresReplaceOnDifferentAddress(),
				},
			},
			"role": schema.StringAttribute{
				Description: "Role of the member: `admin` or `member`. `owner` is readable through the API but never assignable — ownership transfer is not an API operation, so an owner cannot be managed here.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf(roleAdmin, roleMember),
				},
			},
			"user_id": schema.Int64Attribute{
				Description: "User ID — the durable identity to reconcile against your own directory.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					// Immutable for a membership row: a role change cannot move
					// it, so replanning it as unknown is noise in every diff.
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"two_factor_enabled": schema.BoolAttribute{
				Description: "Whether this person has enrolled a second factor.",
				Computed:    true,
			},
			"joined_at": schema.StringAttribute{
				Description: "When the member joined.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp of the membership. Moves on join and role change, not when the person behind it changes.",
				Computed:    true,
			},
		},
	}
}

func (r *organizationMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan validates destructive changes with X-Dry-Run before they are
// applied. The API refuses removing yourself and removing an owner; without
// this the refusal only lands halfway through an apply.
func (r *organizationMemberResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// No client during `terraform validate`, and nothing to check on create.
	if r.client == nil || r.client.SkipPlanValidation || req.State.Raw.IsNull() {
		return
	}

	var state organizationMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueInt64()

	if req.Plan.Raw.IsNull() {
		tflog.Debug(ctx, "Dry-run validating member removal", map[string]interface{}{"id": id})
		err := r.client.DeleteOrganizationMember(client.WithDryRun(ctx), id)
		reportDryRun(err, "removal", state.Email.ValueString(), &resp.Diagnostics)
		return
	}

	var plan organizationMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A different address replaces the resource. Terraform destroys before it
	// creates, so the removal offboards the incumbent — irreversibly, tokens and
	// all — and only THEN does adoption look the successor up. If the successor
	// has not accepted an invitation yet, which is the normal state of affairs,
	// that apply deletes somebody's account and then fails having achieved
	// nothing. Both halves are therefore validated before either runs.
	if !strings.EqualFold(plan.Email.ValueString(), state.Email.ValueString()) {
		tflog.Debug(ctx, "Dry-run validating member removal for replacement", map[string]interface{}{"id": id})
		err := r.client.DeleteOrganizationMember(client.WithDryRun(ctx), id)
		reportDryRun(err, "removal", state.Email.ValueString(), &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}

		successor := plan.Email.ValueString()
		member, err := findMemberByEmail(ctx, r.client, successor)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Could not pre-validate the replacement member",
				fmt.Sprintf("The roster could not be read to confirm %s is adoptable: %s\n\n"+
					"The plan continues, but note the destroy half of this replacement offboards %s first.",
					successor, err, state.Email.ValueString()),
			)
			return
		}
		if member == nil {
			resp.Diagnostics.AddError(
				"Replacement member has not joined yet",
				fmt.Sprintf("Changing this resource's address offboards %s and then adopts %s — but no member of "+
					"this organization has the address %s, so the adoption would fail AFTER the offboarding had "+
					"already deleted %s's account and every API token they owned.\n\nInvite %s with a "+
					"fivenines_organization_invitation and wait for them to accept before repointing this resource.",
					state.Email.ValueString(), successor, successor, state.Email.ValueString(), successor),
			)
			return
		}
		if member.Role == roleOwner {
			resp.Diagnostics.AddError(
				"Cannot manage the organization owner",
				fmt.Sprintf("%s is the organization owner, so the adoption half of this replacement would be "+
					"refused — after the destroy half had already offboarded %s.", successor, state.Email.ValueString()),
			)
		}
		return
	}

	if plan.Role.IsUnknown() || plan.Role.ValueString() == state.Role.ValueString() {
		return
	}

	tflog.Debug(ctx, "Dry-run validating member role change", map[string]interface{}{"id": id})
	_, err := r.client.UpdateOrganizationMember(client.WithDryRun(ctx), id, client.UpdateOrganizationMemberInput{
		Role: plan.Role.ValueString(),
	})
	reportDryRun(err, "role change", state.Email.ValueString(), &resp.Diagnostics)
}

// reportDryRun turns a dry-run result into a plan diagnostic. An API refusal is
// an error — that is the whole point of the pre-flight. A transport failure is
// only a warning, so a network blip cannot block an unrelated plan.
func reportDryRun(err error, action, email string, diags *diag.Diagnostics) {
	if err == nil {
		return
	}
	apiErr := client.AsAPIError(err)
	if apiErr == nil {
		diags.AddWarning(
			fmt.Sprintf("Could not pre-validate member %s", action),
			fmt.Sprintf("The dry-run request for %s failed to reach the API: %s\n\n"+
				"The plan continues; the %s will be attempted at apply time.", email, err, action),
		)
		return
	}
	// Already gone. Delete tolerates this too, so the plan is still valid.
	if client.IsNotFound(err) {
		return
	}
	// A 5xx is the API being unwell, not a policy answer. Rendering it as "the
	// API refuses this" would point the operator at skip_plan_validation as the
	// remedy for a transient outage — turning off the interlock on the most
	// destructive resource in the provider because a proxy returned 502.
	if apiErr.StatusCode >= 500 || apiErr.StatusCode == http.StatusTooManyRequests {
		diags.AddWarning(
			fmt.Sprintf("Could not pre-validate member %s", action),
			fmt.Sprintf("The dry-run request for %s was answered with %s\n\n"+
				"That is a server-side failure rather than a refusal, so the plan continues; "+
				"the %s will be attempted at apply time.", email, apiErr, action),
		)
		return
	}
	// The 403 covers both "this key cannot write" and "nobody may do this to
	// this member" — the API does not separate them — so the escape hatch is
	// offered as the one case it helps, not as the way past the refusal. Skipping
	// the pre-flight when the refusal is self-or-owner just moves the same
	// failure into the apply.
	diags.AddError(
		fmt.Sprintf("Member %s would be refused", action),
		fmt.Sprintf("A dry-run of the %s for %s was refused by the API: %s\n\n"+
			"The API refuses changing or removing your own membership and the organization owner's, "+
			"and requires an admin or owner key with write scope on a plan that includes team features. "+
			"If that is the case here, the apply would be refused too — skipping this pre-flight will not "+
			"change the outcome.\n\n"+
			"Only if you plan with a DIFFERENT key than you apply with (a read-only key cannot dry-run a "+
			"write it would be allowed to perform) set `skip_plan_validation = true` on the provider.",
			action, email, apiErr),
	)
}

// Create adopts an existing membership. The API has no endpoint that creates a
// member: people join by accepting an invitation.
func (r *organizationMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	email := plan.Email.ValueString()
	member, err := findMemberByEmail(ctx, r.client, email)
	if err != nil {
		resp.Diagnostics.AddError("Error listing organization members", err.Error())
		return
	}
	if member == nil {
		resp.Diagnostics.AddError(
			"No such organization member",
			fmt.Sprintf("No member of this organization has the address %q.\n\n"+
				"Members cannot be created through the API — they join by accepting an invitation. "+
				"Invite them with a fivenines_organization_invitation resource, and manage their role here "+
				"once they have accepted.", email),
		)
		return
	}
	if member.Role == roleOwner {
		resp.Diagnostics.AddError(
			"Cannot manage the organization owner",
			fmt.Sprintf("%s is the organization owner. The API refuses to change or remove an owner, and "+
				"ownership transfer is not an API operation, so this membership cannot be managed by Terraform.", email),
		)
		return
	}

	if member.Role != plan.Role.ValueString() {
		tflog.Debug(ctx, "Adopting member and setting role", map[string]interface{}{"id": member.ID, "role": plan.Role.ValueString()})
		member, err = r.client.UpdateOrganizationMember(ctx, member.ID, client.UpdateOrganizationMemberInput{
			Role: plan.Role.ValueString(),
		})
		if err != nil {
			resp.Diagnostics.AddError("Error setting member role", err.Error())
			return
		}
	}

	mapMemberToState(member, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *organizationMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API exposes no per-member GET, so the membership is resolved out of
	// the roster by id.
	members, err := r.client.ListOrganizationMembers(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing organization members", err.Error())
		return
	}

	id := state.ID.ValueInt64()
	for i := range members {
		if members[i].ID == id {
			mapMemberToState(&members[i], &state)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	tflog.Debug(ctx, "Membership no longer present, removing from state", map[string]interface{}{"id": id})
	resp.State.RemoveResource(ctx)
}

func (r *organizationMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan organizationMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state organizationMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A case-only edit to email reaches Update with the role unchanged. The API
	// is idempotent about it, but there is nothing to write.
	if plan.Role.ValueString() == state.Role.ValueString() {
		state.Email = plan.Email
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	member, err := r.client.UpdateOrganizationMember(ctx, state.ID.ValueInt64(), client.UpdateOrganizationMemberInput{
		Role: plan.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error updating member role", err.Error())
		return
	}

	mapMemberToState(member, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete offboards the member: the API removes the membership and deletes the
// user account, destroying that user's API tokens along with it.
func (r *organizationMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Removing organization member", map[string]interface{}{"id": state.ID.ValueInt64()})

	err := r.client.DeleteOrganizationMember(ctx, state.ID.ValueInt64())
	if err == nil {
		return
	}
	if !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error removing organization member", err.Error())
		return
	}

	// The membership id is not durable — it changes when somebody leaves and
	// rejoins — so a 404 here means "no membership with THAT id", not "this
	// person is gone". Reporting a completed offboarding on that alone would
	// tell an operator someone lost their access while they still hold it.
	email := state.Email.ValueString()
	member, lookupErr := findMemberByEmail(ctx, r.client, email)
	if lookupErr != nil {
		resp.Diagnostics.AddWarning(
			"Could not confirm the member was removed",
			fmt.Sprintf("Membership %d was already gone, and the roster could not be read to confirm %s no longer "+
				"has access: %s\n\nTerraform has dropped the resource from state — verify in the dashboard.",
				state.ID.ValueInt64(), email, lookupErr),
		)
		return
	}
	if member != nil {
		resp.Diagnostics.AddError(
			"Member still has access",
			fmt.Sprintf("Membership %d no longer exists, but %s is still a member of this organization under "+
				"membership %d — they most likely left and rejoined, which mints a new membership id.\n\n"+
				"Terraform has NOT offboarded them. Re-run after a refresh, or import membership %d, so the "+
				"removal addresses the membership they actually hold.",
				state.ID.ValueInt64(), email, member.ID, member.ID),
		)
	}
}

// ImportState accepts either a membership id or the member's email address.
func (r *organizationMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if id, err := strconv.ParseInt(req.ID, 10, 64); err == nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
		return
	}

	member, err := findMemberByEmail(ctx, r.client, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error listing organization members", err.Error())
		return
	}
	if member != nil && member.Role == roleOwner {
		resp.Diagnostics.AddError(
			"Cannot manage the organization owner",
			fmt.Sprintf("%s is the organization owner. Importing them would produce a resource whose every "+
				"subsequent plan is refused, since `role` accepts only admin and member and the API refuses to "+
				"change or remove an owner.", req.ID),
		)
		return
	}
	if member == nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("%q is neither a membership id nor the address of a member of this organization.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(member.ID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("email"), types.StringValue(member.Email))...)
}

// findMemberByEmail resolves one membership out of the roster. The API exposes
// no per-member GET and no address filter, so the whole list is the only way in.
// Shared with the invitation resource, which asks the same question to tell an
// accepted invitation from a revoked one.
func findMemberByEmail(ctx context.Context, c *client.Client, email string) (*client.OrganizationMember, error) {
	// An empty needle would EqualFold-match a roster row with no address, which
	// is how an import with no email in state could record a bogus match.
	if email == "" {
		return nil, nil
	}
	members, err := c.ListOrganizationMembers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range members {
		if strings.EqualFold(members[i].Email, email) {
			return &members[i], nil
		}
	}
	return nil, nil
}

// mapMemberToState copies the API row onto state.
func mapMemberToState(m *client.OrganizationMember, state *organizationMemberModel) {
	state.ID = types.Int64Value(m.ID)
	state.UserID = types.Int64Value(m.UserID)
	state.Email = keepEmailCasing(state.Email, m.Email)
	state.Role = types.StringValue(m.Role)
	state.TwoFactorEnabled = types.BoolValue(m.TwoFactorEnabled)
	state.JoinedAt = types.StringValue(m.JoinedAt)
	state.UpdatedAt = types.StringValue(m.UpdatedAt)
}
