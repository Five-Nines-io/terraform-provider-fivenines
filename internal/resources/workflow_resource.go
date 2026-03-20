package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &workflowResource{}
	_ resource.ResourceWithImportState = &workflowResource{}
)

type workflowResource struct {
	client *client.Client
}

type workflowModel struct {
	ID                 types.Int64  `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	IntervalSeconds    types.Int64  `tfsdk:"interval_seconds"`
	Status             types.String `tfsdk:"status"`
	TriggerType        types.String `tfsdk:"trigger_type"`
	TriggerTypeLabel   types.String `tfsdk:"trigger_type_label"`
	PublishedVersionID types.Int64  `tfsdk:"published_version_id"`
	NextEvaluationAt   types.String `tfsdk:"next_evaluation_at"`
	LastEvaluationAt   types.String `tfsdk:"last_evaluation_at"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func NewWorkflowResource() resource.Resource {
	return &workflowResource{}
}

func (r *workflowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow"
}

func (r *workflowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines workflow (automation definition).",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the workflow.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the workflow.",
				Optional:    true,
				Computed:    true,
			},
			"interval_seconds": schema.Int64Attribute{
				Description: "Evaluation interval in seconds.",
				Optional:    true,
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "Current status (draft, active, paused, archived).",
				Computed:    true,
			},
			"trigger_type": schema.StringAttribute{
				Description: "Type of trigger.",
				Computed:    true,
			},
			"trigger_type_label": schema.StringAttribute{
				Description: "Human-readable trigger type.",
				Computed:    true,
			},
			"published_version_id": schema.Int64Attribute{
				Description: "ID of the currently published version.",
				Computed:    true,
			},
			"next_evaluation_at": schema.StringAttribute{
				Description: "Next scheduled evaluation time.",
				Computed:    true,
			},
			"last_evaluation_at": schema.StringAttribute{
				Description: "Last evaluation time.",
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

func (r *workflowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *workflowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workflowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateWorkflowInput{
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		input.Description = plan.Description.ValueString()
	}
	if !plan.IntervalSeconds.IsNull() && !plan.IntervalSeconds.IsUnknown() {
		v := plan.IntervalSeconds.ValueInt64()
		input.IntervalSeconds = &v
	}

	tflog.Debug(ctx, "Creating workflow", map[string]interface{}{"name": input.Name})

	workflow, err := r.client.CreateWorkflow(input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workflow", err.Error())
		return
	}

	mapWorkflowToState(workflow, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workflowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workflow, _, err := r.client.GetWorkflow(state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading workflow", err.Error())
		return
	}

	mapWorkflowToState(workflow, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workflowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workflowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()
	_, etag, err := r.client.GetWorkflow(id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workflow for update", err.Error())
		return
	}

	name := plan.Name.ValueString()
	input := client.UpdateWorkflowInput{
		Name: &name,
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		input.Description = &v
	}
	if !plan.IntervalSeconds.IsNull() && !plan.IntervalSeconds.IsUnknown() {
		v := plan.IntervalSeconds.ValueInt64()
		input.IntervalSeconds = &v
	}

	workflow, err := r.client.UpdateWorkflow(id, etag, input)
	if err != nil {
		resp.Diagnostics.AddError("Error updating workflow", err.Error())
		return
	}

	mapWorkflowToState(workflow, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workflowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting workflow", map[string]interface{}{"id": state.ID.ValueInt64()})

	err := r.client.DeleteWorkflow(state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting workflow", err.Error())
	}
}

func (r *workflowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse %q as int64: %s", req.ID, err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

func mapWorkflowToState(w *client.Workflow, state *workflowModel) {
	state.ID = types.Int64Value(w.ID)
	state.Name = types.StringValue(w.Name)
	state.Description = types.StringValue(w.Description)
	if w.IntervalSeconds != nil {
		state.IntervalSeconds = types.Int64Value(*w.IntervalSeconds)
	} else {
		state.IntervalSeconds = types.Int64Null()
	}
	state.Status = types.StringValue(w.Status)
	state.TriggerType = types.StringValue(w.TriggerType)
	state.TriggerTypeLabel = types.StringValue(w.TriggerTypeLabel)
	if w.PublishedVersionID != nil {
		state.PublishedVersionID = types.Int64Value(*w.PublishedVersionID)
	} else {
		state.PublishedVersionID = types.Int64Null()
	}
	state.NextEvaluationAt = optionalString(w.NextEvaluationAt)
	state.LastEvaluationAt = optionalString(w.LastEvaluationAt)
	state.CreatedAt = types.StringValue(w.CreatedAt)
	state.UpdatedAt = types.StringValue(w.UpdatedAt)
}
