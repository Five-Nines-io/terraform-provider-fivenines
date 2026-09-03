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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// The organization vocabulary, in one place. The API accepts admin and member
// on every write; owner is readable on a membership but never assignable, and
// ownership transfer is not an API operation at all.
const (
	roleOwner  = "owner"
	roleAdmin  = "admin"
	roleMember = "member"

	// statusAccepted is the status this provider records once an invitation has
	// left the invitation list for the member roster.
	statusAccepted = "accepted"
)

var (
	_ resource.Resource                = &organizationInvitationResource{}
	_ resource.ResourceWithImportState = &organizationInvitationResource{}
	_ resource.ResourceWithModifyPlan  = &organizationInvitationResource{}
)

type organizationInvitationResource struct {
	client *client.Client
}

type organizationInvitationModel struct {
	ID         types.Int64  `tfsdk:"id"`
	Email      types.String `tfsdk:"email"`
	Role       types.String `tfsdk:"role"`
	Status     types.String `tfsdk:"status"`
	InvitedBy  types.String `tfsdk:"invited_by"`
	ExpiresAt  types.String `tfsdk:"expires_at"`
	AcceptedAt types.String `tfsdk:"accepted_at"`
	CreatedAt  types.String `tfsdk:"created_at"`
	UpdatedAt  types.String `tfsdk:"updated_at"`
}

func NewOrganizationInvitationResource() resource.Resource {
	return &organizationInvitationResource{}
}

func (r *organizationInvitationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_invitation"
}

func (r *organizationInvitationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Invites somebody to the organization. Creating the resource sends the invitation email and " +
			"holds a seat; destroying it revokes the invitation and invalidates the acceptance link.\n\n" +
			"**An invitation is consumed once it is accepted.** The person becomes a member and the invitation " +
			"leaves the API's invitation list, so Terraform records `status = \"accepted\"` and stops managing it: " +
			"a later destroy is a no-op, and offboarding is the job of `fivenines_organization_member`. An " +
			"invitation that disappears without being accepted was revoked outside Terraform, and the next apply " +
			"re-sends it.\n\n" +
			"Invitations last 7 days, after which `status` reads `expired`. Re-sending is an action rather than a " +
			"state, so it is not modelled here — run `terraform apply -replace` on the resource to issue a fresh " +
			"invitation and a fresh link.\n\n" +
			"Requires the `admin` or `owner` role on a plan that includes team features.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Invitation ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"email": schema.StringAttribute{
				Description: "Email address to invite.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					requiresReplaceOnDifferentAddress(),
				},
			},
			"role": schema.StringAttribute{
				Description: "Role to grant on acceptance: `admin` or `member`. Ownership cannot be invited.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(roleMember),
				Validators: []validator.String{
					stringvalidator.OneOf(roleAdmin, roleMember),
				},
			},
			"status": schema.StringAttribute{
				Description: "`pending`, `expired`, or `accepted` once the invitation has been taken up.",
				Computed:    true,
			},
			"invited_by": schema.StringAttribute{
				Description: "Email of whoever last sent the invitation.",
				Computed:    true,
			},
			"expires_at": schema.StringAttribute{
				Description: "When the invitation lapses. Invitations last 7 days.",
				Computed:    true,
			},
			"accepted_at": schema.StringAttribute{
				Description: "When the invitation was accepted, if it was.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					// A resend moves expires_at and updated_at, never this.
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp. Moves on invite and on resend, not when an invitation lapses.",
				Computed:    true,
			},
		},
	}
}

// ModifyPlan refuses a role change on an accepted invitation at PLAN time. The
// same refusal in Update lands mid-apply, and because it is not self-clearing —
// the resource stays accepted, the configuration keeps the new role — every
// later plan and apply fails identically until somebody hand-edits the config.
// Deciding it here needs no API request: the state already holds the answer.
func (r *organizationInvitationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var state, plan organizationInvitationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.Status.ValueString() != statusAccepted {
		return
	}
	// A changed address REPLACES the resource: the role travels with the new
	// invitation being created, not with the spent one. Only an in-place role
	// change on the accepted invitation is the thing to refuse.
	if plan.Email.IsUnknown() || !strings.EqualFold(plan.Email.ValueString(), state.Email.ValueString()) {
		return
	}
	if plan.Role.IsUnknown() || plan.Role.ValueString() == state.Role.ValueString() {
		return
	}

	resp.Diagnostics.AddError(
		"Invitation has already been accepted",
		fmt.Sprintf("%s accepted this invitation and is now a member, so their role can no longer be changed "+
			"through it.\n\nManage the role with a fivenines_organization_member resource instead, and leave this "+
			"invitation's role as %q (destroying the invitation is safe — it is already spent).",
			state.Email.ValueString(), state.Role.ValueString()),
	)
}

func (r *organizationInvitationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *organizationInvitationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationInvitationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Creating organization invitation", map[string]interface{}{"email": plan.Email.ValueString()})

	invitation, err := r.client.CreateOrganizationInvitation(ctx, client.CreateOrganizationInvitationInput{
		Email: plan.Email.ValueString(),
		Role:  plan.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization invitation", err.Error())
		return
	}

	mapInvitationToState(invitation, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *organizationInvitationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationInvitationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()
	email := state.Email.ValueString()

	// An invitation recorded as accepted is TERMINAL: it was consumed the moment
	// somebody joined, and nothing that happens to the roster afterwards
	// un-consumes it. Re-deriving the verdict here would ask "is this person
	// still a member", and answering no by dropping the resource makes the next
	// apply re-invite somebody who was deliberately offboarded — mailing a live
	// acceptance link to the person the previous apply removed. It also costs
	// no API call, which matters because a long-lived configuration keeps
	// accepted invitations around by design.
	if state.Status.ValueString() == statusAccepted {
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	// The API exposes no per-invitation GET, so the row is resolved out of the
	// list by id. That list carries pending and expired rows only.
	invitations, err := r.client.ListOrganizationInvitations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing organization invitations", err.Error())
		return
	}
	for i := range invitations {
		if invitations[i].ID == id {
			mapInvitationToState(&invitations[i], &state)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	// Not pending: either it was accepted — in which case the address is now on
	// the member roster and the invitation did its job — or it was revoked
	// outside Terraform and should be re-sent on the next apply. The invitation
	// endpoints alone cannot tell these apart.
	member, err := findMemberByEmail(ctx, r.client, email)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error listing organization members",
			fmt.Sprintf("Invitation %d for %s is no longer pending, and the member roster could not be read to "+
				"tell whether it was accepted or revoked: %s", id, email, err),
		)
		return
	}
	if member != nil {
		tflog.Debug(ctx, "Invitation was accepted", map[string]interface{}{"id": id, "email": email})
		state.Status = types.StringValue(statusAccepted)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	tflog.Debug(ctx, "Invitation revoked outside Terraform, removing from state", map[string]interface{}{"id": id})
	resp.State.RemoveResource(ctx)
}

// Update re-sends the invitation with the new role. The create endpoint is an
// upsert, so this refreshes the existing row rather than colliding with it —
// which does mean a new email and a new acceptance link.
func (r *organizationInvitationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan organizationInvitationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state organizationInvitationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Nothing to send: only the role can change without replacing the resource,
	// so an unchanged role means this is a cosmetic edit. This has to precede the
	// accepted refusal below, or re-casing the address of an accepted invitation
	// fails forever.
	if plan.Role.ValueString() == state.Role.ValueString() {
		state.Email = plan.Email
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	if state.Status.ValueString() == statusAccepted {
		resp.Diagnostics.AddError(
			"Invitation has already been accepted",
			fmt.Sprintf("%s accepted this invitation and is now a member, so their role can no longer be changed "+
				"through it.\n\nManage the role with a fivenines_organization_member resource instead, and remove "+
				"this invitation from the configuration (destroying it is a no-op).", state.Email.ValueString()),
		)
		return
	}

	// Re-issuing sends a new email and invalidates the outstanding acceptance
	// link, which is why the cosmetic-edit branch above returns before reaching
	// this point.
	tflog.Debug(ctx, "Re-issuing organization invitation", map[string]interface{}{
		"email": plan.Email.ValueString(),
		"role":  plan.Role.ValueString(),
	})

	invitation, err := r.client.CreateOrganizationInvitation(ctx, client.CreateOrganizationInvitationInput{
		Email: plan.Email.ValueString(),
		Role:  plan.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error updating organization invitation", err.Error())
		return
	}

	mapInvitationToState(invitation, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete revokes the invitation. A 404 means it was already accepted or revoked
// — there is nothing left to cancel either way.
func (r *organizationInvitationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationInvitationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The revoke is attempted even for an invitation recorded as accepted: if
	// it really was accepted the row is gone and the API answers 404, which is
	// tolerated below, and if the acceptance verdict was ever wrong this is what
	// retires the live acceptance link and frees the seat. Skipping the call on
	// a guess is the only version of this that can leave one live.
	tflog.Debug(ctx, "Revoking organization invitation", map[string]interface{}{"id": state.ID.ValueInt64()})

	err := r.client.DeleteOrganizationInvitation(ctx, state.ID.ValueInt64())
	if err == nil {
		return
	}
	if !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error revoking organization invitation", err.Error())
		return
	}

	// The invitation is gone, which is what a revoke wants — unless it went
	// because somebody ACCEPTED it first. Acceptance consumes the row, so the
	// race and the success return the same 404, and reporting "revoked" while
	// the person is now a member would tell an operator that access was
	// withdrawn when it was in fact granted. A warning rather than an error:
	// the destroy must still complete, or the resource is stranded in state.
	if state.Status.ValueString() == statusAccepted {
		return
	}
	member, lookupErr := findMemberByEmail(ctx, r.client, state.Email.ValueString())
	if lookupErr != nil {
		resp.Diagnostics.AddWarning(
			"Could not confirm the invitation was revoked rather than accepted",
			fmt.Sprintf("Invitation %d for %s was already gone, and the member roster could not be read to tell "+
				"a revoke apart from an acceptance that beat it: %s\n\nIf they accepted, they are now a member "+
				"and revoking did not withdraw that access — check before assuming it did.",
				state.ID.ValueInt64(), state.Email.ValueString(), lookupErr),
		)
		return
	}
	if member == nil {
		return
	}
	resp.Diagnostics.AddWarning(
		"Invitation was accepted before it could be revoked",
		fmt.Sprintf("%s accepted this invitation before the revoke reached the API, so they are now a member "+
			"of this organization (membership %d, role %s) — revoking the invitation did not withdraw that "+
			"access.\n\nRemove them with a fivenines_organization_member resource if that was the intent.",
			member.Email, member.ID, member.Role),
	)
}

func (r *organizationInvitationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse %q as int64: %s", req.ID, err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

// mapInvitationToState copies the API row onto state.
func mapInvitationToState(i *client.OrganizationInvitation, state *organizationInvitationModel) {
	state.ID = types.Int64Value(i.ID)
	state.Email = keepEmailCasing(state.Email, i.Email)
	state.Role = types.StringValue(i.Role)
	state.Status = types.StringValue(i.Status)
	state.InvitedBy = optionalString(i.InvitedBy)
	state.ExpiresAt = types.StringValue(i.ExpiresAt)
	state.AcceptedAt = optionalString(i.AcceptedAt)
	state.CreatedAt = types.StringValue(i.CreatedAt)
	state.UpdatedAt = types.StringValue(i.UpdatedAt)
}
