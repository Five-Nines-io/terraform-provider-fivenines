package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                     = &workflowResource{}
	_ resource.ResourceWithImportState      = &workflowResource{}
	_ resource.ResourceWithConfigValidators = &workflowResource{}
)

type workflowResource struct {
	client *client.Client
}

type workflowModel struct {
	ID                 types.Int64  `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	IntervalSeconds    types.Int64  `tfsdk:"interval_seconds"`
	ExecutionGraphJSON types.String `tfsdk:"execution_graph_json"`
	CanvasDataJSON     types.String `tfsdk:"canvas_data_json"`
	TemplateSlug       types.String `tfsdk:"template_slug"`
	Active             types.Bool   `tfsdk:"active"`
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
			"execution_graph_json": schema.StringAttribute{
				Description: "JSON-encoded execution graph (nodes and edges). When changed, a new version is created and published automatically. Use jsonencode() or file() to provide the value. When left unset, it is read back from the published version. Formatting differences (key order, whitespace) are ignored.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					jsonSemanticEquality{},
				},
			},
			"canvas_data_json": schema.StringAttribute{
				Description: "JSON-encoded React Flow canvas layout published alongside the execution graph. Leave unset to let the API generate a layout for the graph. Formatting differences (key order, whitespace) are ignored.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					jsonSemanticEquality{},
				},
			},
			"template_slug": schema.StringAttribute{
				Description: "Slug of a workflow template to instantiate. Only used at creation time — changing it replaces the workflow. Conflicts with execution_graph_json and canvas_data_json.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"active": schema.BoolAttribute{
				Description: "Whether the workflow is active. Set to true to activate, false to pause. Requires a published version.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"status": schema.StringAttribute{
				Description: "Current status (draft, active, paused, archived).",
				Computed:    true,
			},
			"trigger_type": schema.StringAttribute{
				Description: "Type of trigger (derived from the execution graph).",
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

// ConfigValidators enforces that a workflow is bootstrapped either from a
// template or from an inline graph, never both.
func (r *workflowResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.Conflicting(
			path.MatchRoot("template_slug"),
			path.MatchRoot("execution_graph_json"),
		),
		resourcevalidator.Conflicting(
			path.MatchRoot("template_slug"),
			path.MatchRoot("canvas_data_json"),
		),
	}
}

func (r *workflowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workflowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var workflow *client.Workflow
	var err error

	if !plan.TemplateSlug.IsNull() && !plan.TemplateSlug.IsUnknown() {
		slug := plan.TemplateSlug.ValueString()
		tflog.Debug(ctx, "Creating workflow from template", map[string]interface{}{"slug": slug})

		workflow, err = r.client.CreateWorkflowFromTemplate(ctx, slug)
		if err != nil {
			resp.Diagnostics.AddError("Error creating workflow from template", err.Error())
			return
		}

		// The template supplies its own name, description and interval, so patch
		// the configured values over them.
		workflow, err = r.client.UpdateWorkflow(ctx, workflow.ID, r.updateInput(plan))
		if err != nil {
			resp.Diagnostics.AddError("Error applying configuration to templated workflow", err.Error())
			return
		}
	} else {
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

		workflow, err = r.client.CreateWorkflow(ctx, input)
		if err != nil {
			resp.Diagnostics.AddError("Error creating workflow", err.Error())
			return
		}
	}

	// If execution_graph_json is provided, create a version and publish it.
	if !plan.ExecutionGraphJSON.IsNull() && !plan.ExecutionGraphJSON.IsUnknown() {
		if err := r.publishGraph(ctx, workflow.ID, plan.ExecutionGraphJSON, plan.CanvasDataJSON); err != nil {
			resp.Diagnostics.AddError("Error publishing workflow version", err.Error())
			return
		}
	}

	// Activation is independent of how the graph got there: a template or a
	// version published out-of-band is enough for the API to accept it.
	if plan.Active.ValueBool() {
		if err := r.client.ActivateWorkflow(ctx, workflow.ID); err != nil {
			resp.Diagnostics.AddError("Error activating workflow", activationErrorDetail(err))
			return
		}
	}

	// Re-read: publishing, templating and activating all move the workflow on.
	workflow, _, err = r.client.GetWorkflow(ctx, workflow.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workflow after create", err.Error())
		return
	}

	mapWorkflowToState(workflow, &plan)
	if err := r.fillUnknownGraph(ctx, workflow, &plan); err != nil {
		resp.Diagnostics.AddError("Error reading published workflow version", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workflowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workflow, _, err := r.client.GetWorkflow(ctx, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading workflow", err.Error())
		return
	}

	mapWorkflowToState(workflow, &state)

	// Read the published graph back so imports are complete and edits made
	// outside Terraform show up as drift.
	graph, canvas, err := r.readPublishedGraph(ctx, workflow)
	if err != nil {
		resp.Diagnostics.AddError("Error reading published workflow version", err.Error())
		return
	}
	state.ExecutionGraphJSON = preserveJSONIfEqual(state.ExecutionGraphJSON, graph)
	if !state.CanvasDataJSON.IsNull() {
		state.CanvasDataJSON = preserveJSONIfEqual(state.CanvasDataJSON, canvas)
	}

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

	// Workflow endpoints do not support If-Match, so there is no optimistic
	// locking to retry here.
	workflow, err := r.client.UpdateWorkflow(ctx, id, r.updateInput(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating workflow", err.Error())
		return
	}

	// Publish a new version when the graph or the canvas layout changed.
	if !plan.ExecutionGraphJSON.IsNull() && !plan.ExecutionGraphJSON.IsUnknown() {
		graphChanged := !jsonAttrEqual(plan.ExecutionGraphJSON, state.ExecutionGraphJSON)
		canvasChanged := !jsonAttrEqual(plan.CanvasDataJSON, state.CanvasDataJSON)

		if graphChanged || canvasChanged {
			if err := r.publishGraph(ctx, id, plan.ExecutionGraphJSON, plan.CanvasDataJSON); err != nil {
				resp.Diagnostics.AddError("Error publishing workflow version", err.Error())
				return
			}
		}
	}

	// Handle activate/pause transitions
	wantActive := plan.Active.ValueBool()
	isActive := workflow.Status == "active"
	if wantActive && !isActive {
		if err := r.client.ActivateWorkflow(ctx, id); err != nil {
			resp.Diagnostics.AddError("Error activating workflow", activationErrorDetail(err))
			return
		}
	} else if !wantActive && isActive {
		if err := r.client.PauseWorkflow(ctx, id); err != nil {
			resp.Diagnostics.AddError("Error pausing workflow", err.Error())
			return
		}
	}

	// Re-read final state
	workflow, _, err = r.client.GetWorkflow(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workflow after update", err.Error())
		return
	}

	mapWorkflowToState(workflow, &plan)
	if err := r.fillUnknownGraph(ctx, workflow, &plan); err != nil {
		resp.Diagnostics.AddError("Error reading published workflow version", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workflowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting workflow", map[string]interface{}{"id": state.ID.ValueInt64()})

	err := r.client.DeleteWorkflow(ctx, state.ID.ValueInt64())
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

// updateInput builds the metadata patch body from a plan.
func (r *workflowResource) updateInput(plan workflowModel) client.UpdateWorkflowInput {
	name := plan.Name.ValueString()
	input := client.UpdateWorkflowInput{Name: &name}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		input.Description = &v
	}
	if !plan.IntervalSeconds.IsNull() && !plan.IntervalSeconds.IsUnknown() {
		v := plan.IntervalSeconds.ValueInt64()
		input.IntervalSeconds = &v
	}
	return input
}

// publishGraph creates a new workflow version from JSON and publishes it. A null
// canvas is left to the API, which generates a layout for the graph.
func (r *workflowResource) publishGraph(ctx context.Context, workflowID int64, graphJSON, canvasJSON types.String) error {
	var graph map[string]interface{}
	if err := json.Unmarshal([]byte(graphJSON.ValueString()), &graph); err != nil {
		return fmt.Errorf("invalid execution_graph_json: %w", err)
	}

	input := client.CreateWorkflowVersionInput{ExecutionGraph: graph}
	if !canvasJSON.IsNull() && !canvasJSON.IsUnknown() {
		var canvas map[string]interface{}
		if err := json.Unmarshal([]byte(canvasJSON.ValueString()), &canvas); err != nil {
			return fmt.Errorf("invalid canvas_data_json: %w", err)
		}
		input.CanvasData = canvas
	}

	version, err := r.client.CreateWorkflowVersion(ctx, workflowID, input)
	if err != nil {
		return fmt.Errorf("creating version: %w", err)
	}

	if err := r.client.PublishWorkflowVersion(ctx, workflowID, version.ID); err != nil {
		return fmt.Errorf("publishing version %d: %w", version.ID, err)
	}

	return nil
}

// readPublishedGraph returns the execution graph and canvas layout of the
// workflow's published version, both empty when nothing is published yet.
func (r *workflowResource) readPublishedGraph(ctx context.Context, w *client.Workflow) (string, string, error) {
	if w.PublishedVersionID == nil {
		return "", "", nil
	}

	version, err := r.client.GetWorkflowVersion(ctx, w.ID, *w.PublishedVersionID)
	if err != nil {
		return "", "", fmt.Errorf("reading version %d: %w", *w.PublishedVersionID, err)
	}

	graph, err := marshalGraph(version.ExecutionGraph)
	if err != nil {
		return "", "", fmt.Errorf("encoding execution_graph: %w", err)
	}
	canvas, err := marshalGraph(version.CanvasData)
	if err != nil {
		return "", "", fmt.Errorf("encoding canvas_data: %w", err)
	}
	return graph, canvas, nil
}

// fillUnknownGraph resolves execution_graph_json when the configuration left it
// unset — after a template instantiation or an import, the published version is
// the only source for it. A configured graph keeps its planned value: writing
// back the API's rendering of the same graph would break the plan contract.
func (r *workflowResource) fillUnknownGraph(ctx context.Context, w *client.Workflow, m *workflowModel) error {
	if !m.ExecutionGraphJSON.IsUnknown() {
		return nil
	}

	graph, _, err := r.readPublishedGraph(ctx, w)
	if err != nil {
		return err
	}
	m.ExecutionGraphJSON = stringOrNull(graph)
	return nil
}

// activationErrorDetail explains the 422 the API returns for a workflow that
// has no published version to activate.
func activationErrorDetail(err error) string {
	if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 422 {
		return fmt.Sprintf("%s\n\nA workflow needs a published version before it can be activated. "+
			"Set execution_graph_json or template_slug, or publish a version outside Terraform first.", err.Error())
	}
	return err.Error()
}

// marshalGraph encodes an API graph payload, returning "" for an absent one.
// Go sorts object keys, which gives the state a stable rendering to compare.
func marshalGraph(graph map[string]interface{}) (string, error) {
	if graph == nil {
		return "", nil
	}
	encoded, err := json.Marshal(graph)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// jsonAttrEqual compares two optional JSON attributes, treating null as absent.
func jsonAttrEqual(a, b types.String) bool {
	if a.IsNull() || a.IsUnknown() || b.IsNull() || b.IsUnknown() {
		return a.IsNull() == b.IsNull() && a.IsUnknown() == b.IsUnknown()
	}
	return jsonEqual(a.ValueString(), b.ValueString())
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
	state.Active = types.BoolValue(w.Status == "active")
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

	// execution_graph_json and canvas_data_json live on the published version,
	// not on the workflow object — callers resolve them separately.
}
