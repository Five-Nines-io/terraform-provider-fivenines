package resources

import (
	"context"
	"fmt"
	"strconv"

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
	_ resource.Resource                = &enrollmentTokenResource{}
	_ resource.ResourceWithImportState = &enrollmentTokenResource{}
)

type enrollmentTokenResource struct {
	client *client.Client
}

type enrollmentTokenModel struct {
	ID                   types.Int64  `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	Token                types.String `tfsdk:"token"`
	InstallCommand       types.String `tfsdk:"install_command"`
	HostsRegisteredCount types.Int64  `tfsdk:"hosts_registered_count"`
	LastUsedAt           types.String `tfsdk:"last_used_at"`
	CreatedAt            types.String `tfsdk:"created_at"`
	UpdatedAt            types.String `tfsdk:"updated_at"`
}

func NewEnrollmentTokenResource() resource.Resource {
	return &enrollmentTokenResource{}
}

func (r *enrollmentTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enrollment_token"
}

func (r *enrollmentTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines enrollment token: the credential an agent presents to self-enroll a host, so " +
			"cloud-init, Ansible or an image build can onboard a machine without a human copying a secret out of the " +
			"dashboard.\n\n" +
			"The value is returned by the API exactly once, in the create response, and is stored in Terraform state from " +
			"there — treat your state as a secret. Nothing can fetch it back, so an imported token has no value and " +
			"never will.\n\n" +
			"Revoking a token outside Terraform drops it from state and the next plan mints a replacement, the same " +
			"rule `fivenines_api_token` follows: a revoked token enrolls nothing and cannot be reactivated, so " +
			"recreating it is the only heal. Hosts it already enrolled keep reporting and stay attributed to it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the enrollment token. The API can create, revoke and delete tokens but not edit " +
					"them, so changing this replaces the token and issues a new value.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"token": schema.StringAttribute{
				Description: "The value an agent presents to enroll a host. Returned by the API only when the token is " +
					"created, so it is null on an imported token.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"install_command": schema.StringAttribute{
				Description: "Ready-to-run one-liner that installs the agent and enrolls the host with this token. It " +
					"embeds the value, so it is equally sensitive and equally null on an imported token.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"hosts_registered_count": schema.Int64Attribute{
				Description: "Number of hosts that have enrolled through this token. Once this is above zero the API " +
					"refuses to delete the token, so destroying the resource revokes it instead.",
				Computed: true,
			},
			"last_used_at": schema.StringAttribute{
				Description: "Last time an agent enrolled through this token.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp. Moves on create and on revoke only — an enrollment writes " +
					"last_used_at and hosts_registered_count without touching it.",
				Computed: true,
			},
		},
	}
}

func (r *enrollmentTokenResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected *client.Client, got unexpected type.")
		return
	}
	r.client = c
}

func (r *enrollmentTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enrollmentTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Creating enrollment token", map[string]interface{}{"name": plan.Name.ValueString()})

	token, err := r.client.CreateEnrollmentToken(ctx, client.CreateEnrollmentTokenInput{
		Name: plan.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating enrollment token", err.Error())
		return
	}

	mapEnrollmentTokenToState(token, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *enrollmentTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enrollmentTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	token, err := r.client.GetEnrollmentToken(ctx, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading enrollment token", err.Error())
		return
	}

	// A revoked token is gone as far as Terraform is concerned. The row survives
	// server-side so the hosts it enrolled keep their attribution, but it enrolls
	// nothing and cannot be un-revoked, so dropping it here is what makes the next
	// apply mint a replacement — the same rule, and the same reasoning, as
	// api_token_resource.go.
	//
	// api_token has to separate revocation from expiry there, because recreating
	// an expired token would re-send a past expires_at and wedge every future
	// plan. Enrollment tokens have no expiry at all: the API carries `active` and
	// no `expires_at`, so inactive can only mean revoked and the exception has
	// nothing to apply to.
	if !token.Active {
		tflog.Debug(ctx, "Enrollment token has been revoked, removing from state", map[string]interface{}{
			"id": token.ID,
		})
		resp.State.RemoveResource(ctx)
		return
	}

	mapEnrollmentTokenToState(token, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *enrollmentTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state enrollmentTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// name is the only configurable attribute and it forces replacement, so
	// nothing can reach this. Mirrors api_token: an in-place update here would
	// silently report success for a change the API cannot make.
	resp.Diagnostics.AddError(
		"Enrollment tokens cannot be updated in place",
		"The FiveNines API can create, revoke and delete enrollment tokens but not edit them, so this change has to "+
			"replace the token. Reaching this error means a plan modifier is missing — please report it.",
	)
}

func (r *enrollmentTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state enrollmentTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()
	tflog.Debug(ctx, "Deleting enrollment token", map[string]interface{}{"id": id})

	err := r.client.DeleteEnrollmentToken(ctx, id)
	if err == nil {
		return
	}
	if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
		return
	}

	// The API refuses to delete a token that has enrolled hosts, because the
	// hosts would lose their attribution, and points at revoke instead. Take it:
	// a destroy that stops halfway leaves a live enrollment credential in the
	// fleet, which is the outcome destroying this resource exists to produce.
	// The row stays behind, so say so rather than reporting a clean delete.
	if !client.IsTokenHasRegisteredHosts(err) {
		resp.Diagnostics.AddError("Error deleting enrollment token", err.Error())
		return
	}

	if _, revokeErr := r.client.RevokeEnrollmentToken(ctx, id); revokeErr != nil {
		resp.Diagnostics.AddError("Error deleting enrollment token",
			fmt.Sprintf("Enrollment token %d has registered hosts, so the API refused to delete it, and revoking it "+
				"instead also failed. The token can still enroll hosts.\n\nDelete: %s\n\nRevoke: %s",
				id, err.Error(), revokeErr.Error()))
		return
	}

	// The count comes from state, which an import has not refreshed yet — say
	// "hosts" rather than "0 host(s)", which would read as a contradiction of the
	// refusal being reported.
	enrolled := "hosts"
	if !state.HostsRegisteredCount.IsNull() && !state.HostsRegisteredCount.IsUnknown() {
		enrolled = fmt.Sprintf("%d host(s)", state.HostsRegisteredCount.ValueInt64())
	}
	resp.Diagnostics.AddWarning("Enrollment token revoked instead of deleted",
		fmt.Sprintf("Enrollment token %d has enrolled %s, which the API will not let you delete — that would "+
			"orphan them. It has been revoked instead, so it can no longer enroll anything, and removed from Terraform "+
			"state. The token record remains in FiveNines, and the hosts it enrolled are unaffected.",
			id, enrolled))
}

func (r *enrollmentTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse %q as int64: %s", req.ID, err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
	resp.Diagnostics.AddWarning("Imported enrollment token has no value",
		"The API returns an enrollment token's value only when it is created, so `token` and `install_command` are "+
			"null for an imported token and stay null. Anything consuming them needs a token Terraform created.")
}

// mapEnrollmentTokenToState copies API fields onto Terraform state.
//
// token and install_command are written only when the response actually carries
// them, which is the create response and nothing else. Copying them
// unconditionally would blank the one copy of the value that exists, the next
// time a read or a revoke maps a metadata-only row over it — emptying
// `terraform output` without changing a single plan.
//
// The unknown case is the create path and only the create path: a Computed
// attribute is unknown until something resolves it, and an unknown that survives
// an apply is a hard framework error that discards the state Terraform was about
// to write — leaking a live token the provider can never read back. So a create
// response missing either field resolves it to null rather than leaving it
// unknown. Same shape as mapAPITokenToState.
func mapEnrollmentTokenToState(t *client.EnrollmentToken, state *enrollmentTokenModel) {
	state.ID = types.Int64Value(t.ID)
	state.Name = types.StringValue(t.Name)
	state.HostsRegisteredCount = types.Int64Value(t.HostsRegisteredCount)
	state.LastUsedAt = optionalString(t.LastUsedAt)
	state.CreatedAt = types.StringValue(t.CreatedAt)
	state.UpdatedAt = types.StringValue(t.UpdatedAt)
	if t.Token != "" {
		state.Token = types.StringValue(t.Token)
	} else if state.Token.IsUnknown() {
		state.Token = types.StringNull()
	}
	if t.InstallCommand != "" {
		state.InstallCommand = types.StringValue(t.InstallCommand)
	} else if state.InstallCommand.IsUnknown() {
		state.InstallCommand = types.StringNull()
	}
}
