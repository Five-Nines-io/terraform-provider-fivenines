package resources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                = &hostGroupResource{}
	_ resource.ResourceWithImportState = &hostGroupResource{}
)

type hostGroupResource struct {
	client *client.Client
}

type hostGroupModel struct {
	ID        types.Int64  `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Position  types.Int64  `tfsdk:"position"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

// isNotFound reports whether an error is the API's 404. errors.As rather than a
// bare type assertion: the client already wraps errors elsewhere, and a future
// wrap would silently turn "the group vanished" into a hard apply failure.
func isNotFound(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func vanishedHostGroupDetail(id int64) string {
	return fmt.Sprintf("Host group %d was removed outside of Terraform — the API deletes a group once its "+
		"last instance leaves it. Re-run the apply to recreate it; if the plan was saved or refreshing is "+
		"disabled, refresh first so Terraform can see it is gone.", id)
}

// settledPosition decides which position an apply records: the one Terraform
// planned, or the one the API reports.
//
// Terraform requires the applied value to equal a KNOWN planned value, and it
// enforces that on both ends — a provider may not plan unknown over a configured
// position (Terraform rejects the plan itself), and it may not apply a value that
// differs from one (Terraform rejects the result). Two things routinely make the
// API disagree: it clamps a position beyond the number of groups, and it
// renumbers a group whenever a NEIGHBOUR moves, so even an update that never
// touched position can come back carrying a different one.
//
// So a known plan wins, and Read reports the server's truth on the next refresh —
// a diff the practitioner can see and act on, rather than a dead apply. A plan
// that is unknown has nothing to keep, which is the case where the practitioner
// left position out and the API's assignment is the only answer there is.
func settledPosition(planned, fromAPI types.Int64) types.Int64 {
	if planned.IsUnknown() || planned.IsNull() {
		return fromAPI
	}
	return planned
}

func NewHostGroupResource() resource.Resource {
	return &hostGroupResource{}
}

func (r *hostGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host_group"
}

func (r *hostGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines host group. Note that the API deletes a group automatically " +
			"once its last instance leaves it; Terraform treats the vanished group as removed and " +
			"recreates it on the next apply.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the host group. Up to 50 characters, unique per organization (case-insensitive).",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthBetween(1, 50),
				},
			},
			"position": schema.Int64Attribute{
				Description: "1-based position in the group list. Setting it slots the group in and renumbers " +
					"the others, so repositioning one group shifts the position of every other group in the " +
					"organisation. Omit it to let new groups land on top, which is the right choice unless " +
					"you intend to manage the whole ordering. A configured position is the position you are " +
					"asking for: the API clamps anything beyond the number of existing groups without " +
					"reporting an error, so a position past the end settles at the last slot and every " +
					"later plan shows a diff that can never converge. Keep configured positions within the " +
					"number of groups you manage.",
				Optional: true,
				Computed: true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp. Position changes do not bump it.",
				Computed:    true,
			},
		},
	}
}

func (r *hostGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *hostGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateHostGroupInput{
		Name: plan.Name.ValueString(),
	}
	// Unknown means the configuration left the position out, and the API reads an
	// omitted position as "put this group on top". There is no zero value that
	// says the same thing, so the key has to be absent rather than empty.
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() {
		v := plan.Position.ValueInt64()
		input.Position = &v
	}

	tflog.Debug(ctx, "Creating host group", map[string]interface{}{"name": input.Name})

	group, err := r.client.CreateHostGroup(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating host group", err.Error())
		return
	}

	plannedPosition := plan.Position
	mapHostGroupToState(group, &plan)
	plan.Position = settledPosition(plannedPosition, plan.Position)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *hostGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, _, err := r.client.GetHostGroup(ctx, state.ID.ValueInt64())
	if err != nil {
		// The API drops a group as soon as its last instance leaves it, so a 404 here
		// is an expected outcome rather than a hard failure.
		if isNotFound(err) {
			tflog.Debug(ctx, "Host group no longer exists, removing from state", map[string]interface{}{"id": state.ID.ValueInt64()})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading host group", err.Error())
		return
	}

	mapHostGroupToState(group, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *hostGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan hostGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state hostGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var config hostGroupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()

	name := plan.Name.ValueString()

	// ETag retry loop. The ETag is harvested from a GET immediately before the
	// PATCH, so a 412 means something moved in between — which for host groups is
	// routine, since any group's move invalidates every other group's ETag.
	var group *client.HostGroup
	for attempt := 0; attempt < 3; attempt++ {
		current, etag, err := r.client.GetHostGroup(ctx, id)
		if err != nil {
			if isNotFound(err) {
				resp.Diagnostics.AddError("Host group no longer exists", vanishedHostGroupDetail(id))
				return
			}
			resp.Diagnostics.AddError("Error reading host group for update", err.Error())
			return
		}
		// Decide the move against what the server holds RIGHT NOW, not against
		// Terraform's last refresh. A sibling's move renumbers this group, so
		// state.Position can already be stale by the time we get here: comparing
		// to it can skip a move the practitioner asked for (config matches the
		// stale number, so nothing is sent, and the group silently stays where the
		// sibling left it) or send one nobody needs (the server already reached
		// the target, and re-sending it renumbers every other group). This GET is
		// the same round trip the ETag needs, so the fresher answer is free — and
		// it is recomputed on every retry, which is exactly when it has changed.
		//
		// Only a real move is sent: omitting the key means "leave it where it is",
		// and so does a configured position the group already occupies.
		input := client.UpdateHostGroupInput{Name: &name}
		if !config.Position.IsNull() && !config.Position.IsUnknown() &&
			config.Position.ValueInt64() != current.Position {
			v := config.Position.ValueInt64()
			input.Position = &v
		}

		group, err = r.client.UpdateHostGroup(ctx, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on host group update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			// The group can also vanish in the GET-to-PATCH window.
			if isNotFound(err) {
				resp.Diagnostics.AddError("Host group no longer exists", vanishedHostGroupDetail(id))
				return
			}
			if client.IsPreconditionFailed(err) {
				resp.Diagnostics.AddError(
					"Host group was modified concurrently",
					fmt.Sprintf("Host group %d changed between reading its ETag and updating it, three times "+
						"running. Any group's move renumbers the others, so a large reordering applied in "+
						"parallel can collide this way. Re-run the apply.", id),
				)
				return
			}
			resp.Diagnostics.AddError("Error updating host group", err.Error())
			return
		}
		break
	}

	// Unreachable while the loop bound and the retry guard agree, but decoupling
	// them would otherwise fall through into a nil dereference below.
	if group == nil {
		resp.Diagnostics.AddError("Error updating host group", "the update retry loop produced no result")
		return
	}

	plannedPosition := plan.Position
	mapHostGroupToState(group, &plan)
	plan.Position = settledPosition(plannedPosition, plan.Position)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *hostGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting host group", map[string]interface{}{"id": state.ID.ValueInt64()})

	// Deleting a group only ungroups its instances; the instances themselves survive.
	err := r.client.DeleteHostGroup(ctx, state.ID.ValueInt64())
	if err != nil {
		if isNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting host group", err.Error())
	}
}

func (r *hostGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse %q as int64: %s", req.ID, err))
		return
	}
	if id < 1 {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Host group ids start at 1, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

func mapHostGroupToState(g *client.HostGroup, state *hostGroupModel) {
	state.ID = types.Int64Value(g.ID)
	state.Name = types.StringValue(g.Name)
	state.Position = types.Int64Value(g.Position)
	state.CreatedAt = types.StringValue(g.CreatedAt)
	state.UpdatedAt = types.StringValue(g.UpdatedAt)
}
