package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	ID                 types.Int64          `tfsdk:"id"`
	Name               types.String         `tfsdk:"name"`
	Description        types.String         `tfsdk:"description"`
	IntervalSeconds    types.Int64          `tfsdk:"interval_seconds"`
	ExecutionGraphJSON jsontypes.Normalized `tfsdk:"execution_graph_json"`
	CanvasDataJSON     jsontypes.Normalized `tfsdk:"canvas_data_json"`
	TemplateSlug       types.String         `tfsdk:"template_slug"`
	Active             types.Bool           `tfsdk:"active"`
	Status             types.String         `tfsdk:"status"`
	TriggerType        types.String         `tfsdk:"trigger_type"`
	TriggerTypeLabel   types.String         `tfsdk:"trigger_type_label"`
	PublishedVersionID types.Int64          `tfsdk:"published_version_id"`
	NextEvaluationAt   types.String         `tfsdk:"next_evaluation_at"`
	LastEvaluationAt   types.String         `tfsdk:"last_evaluation_at"`
	CreatedAt          types.String         `tfsdk:"created_at"`
	UpdatedAt          types.String         `tfsdk:"updated_at"`
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
			// Computed as well as Optional so the published graph can be read
			// back: that is what makes an import complete, and what turns a graph
			// edited outside Terraform into drift instead of silence.
			"execution_graph_json": schema.StringAttribute{
				Description: "JSON-encoded execution graph (nodes and edges). When changed, a new version is created and published automatically. Use jsonencode() or file() to provide the value. Left unset, it is read back from the workflow's published version. Compared semantically, so reformatting the graph (whitespace, key ordering) does not publish a new workflow version.",
				Optional:    true,
				Computed:    true,
				CustomType:  jsontypes.NormalizedType{},
			},
			// Optional-only on purpose. Computed, the API's auto-generated layout
			// would land in state, and every republish would then owe that
			// generated value an update the practitioner never asked for.
			"canvas_data_json": schema.StringAttribute{
				Description: "JSON-encoded React Flow canvas layout, published alongside the execution graph. Omit it and the API lays the graph out itself; the generated layout is not tracked here. Removing it from a configuration that had one stops Terraform managing the layout, it does not restore the generated one. Requires execution_graph_json, since a layout is only ever published as part of a workflow version. Compared semantically, so reformatting it does not publish a new version.",
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
			},
			"template_slug": schema.StringAttribute{
				Description: "Slug of a workflow template to instantiate, as listed by the fivenines_workflow_templates data source. The template arrives with its graph already published. Only read at creation time, so changing it replaces the workflow. Conflicts with execution_graph_json and canvas_data_json.",
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

// ConfigValidators keeps the two ways of getting a graph onto a workflow apart.
// A template arrives with its own published graph and canvas, so pinning either
// alongside it would lose to whichever the resource wrote last.
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

	resp.Diagnostics.Append(unresolvedInputDiagnostics(plan)...)
	if d := canvasNeedsGraphDiagnostic(plan.ExecutionGraphJSON, plan.CanvasDataJSON); d != nil {
		resp.Diagnostics.Append(d)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var workflow *client.Workflow
	var err error

	if !plan.TemplateSlug.IsNull() {
		slug := plan.TemplateSlug.ValueString()
		tflog.Debug(ctx, "Creating workflow from template", map[string]interface{}{"slug": slug})

		workflow, err = r.client.CreateWorkflowFromTemplate(ctx, slug)
		if err != nil {
			resp.Diagnostics.AddError("Error creating workflow from template", err.Error())
			return
		}

		// The template names the workflow and picks its own interval, so the
		// configured values have to be patched over them.
		workflow, err = r.client.UpdateWorkflow(ctx, workflow.ID, workflowUpdateInput(plan))
		if err != nil {
			resp.Diagnostics.AddError("Error applying configuration to templated workflow", err.Error())
			return
		}
	} else {
		input := client.CreateWorkflowInput{
			Name:            plan.Name.ValueString(),
			IntervalSeconds: int64Ptr(plan.IntervalSeconds),
		}
		if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
			input.Description = plan.Description.ValueString()
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

	// Activation does not care how the graph got there. Nesting it inside the
	// block above meant `active = true` on a workflow whose version came from a
	// template, or from the UI, silently did nothing.
	if plan.Active.ValueBool() {
		if err := r.client.ActivateWorkflow(ctx, workflow.ID); err != nil {
			resp.Diagnostics.AddError("Error activating workflow", activationErrorDetail(err))
			return
		}
	}

	// Publishing, templating and activating each move the workflow on, so the
	// authoritative state is what comes back from a fresh read.
	workflow, _, err = r.client.GetWorkflow(ctx, workflow.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workflow after create", err.Error())
		return
	}

	mapWorkflowToState(workflow, &plan)
	if err := r.resolveUnknownGraph(ctx, workflow, &plan); err != nil {
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

	// The graph lives on the published version, not on the workflow. Reading it
	// back is what completes an import and what surfaces a graph edited in the
	// UI as drift. jsontypes.Normalized collapses a pure re-serialisation back
	// onto the stored value, so this does not diff on formatting.
	graph, canvas, err := r.readPublishedGraph(ctx, workflow)
	if err != nil {
		resp.Diagnostics.AddError("Error reading published workflow version", err.Error())
		return
	}
	state.ExecutionGraphJSON = graph
	// canvas_data_json is Optional-only, so a layout the configuration never
	// pinned must stay out of state: Terraform rejects an apply that turns a
	// null Optional attribute into a value.
	if !state.CanvasDataJSON.IsNull() {
		state.CanvasDataJSON = canvas
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

	resp.Diagnostics.Append(unresolvedInputDiagnostics(plan)...)
	if d := canvasNeedsGraphDiagnostic(plan.ExecutionGraphJSON, plan.CanvasDataJSON); d != nil {
		resp.Diagnostics.Append(d)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()

	// The workflow endpoints do not document ETag/If-Match, so there is no
	// optimistic locking to send and no 412 to retry.
	workflow, err := r.client.UpdateWorkflow(ctx, id, workflowUpdateInput(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating workflow", err.Error())
		return
	}

	// A new version is cut when either payload changed. The canvas alone counts:
	// republishing the same graph under a new layout is how a pinned layout gets
	// updated at all.
	graphChanged, d := shouldPublishGraph(ctx, plan.ExecutionGraphJSON, state.ExecutionGraphJSON)
	resp.Diagnostics.Append(d...)
	canvasChanged, d := shouldRepublishCanvas(ctx, plan.CanvasDataJSON, state.CanvasDataJSON)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A canvas can be dropped from a workflow that has no graph either — there is
	// simply no version to cut in that case.
	hasGraph := !plan.ExecutionGraphJSON.IsNull() && !plan.ExecutionGraphJSON.IsUnknown()
	if hasGraph && (graphChanged || canvasChanged) {
		if err := r.publishGraph(ctx, id, plan.ExecutionGraphJSON, plan.CanvasDataJSON); err != nil {
			resp.Diagnostics.AddError("Error publishing workflow version", err.Error())
			return
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
	if err := r.resolveUnknownGraph(ctx, workflow, &plan); err != nil {
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

// workflowUpdateInput builds the metadata patch. Every field goes through the
// same stringPtr/int64Ptr call site; whether a nil clears or preserves is a
// property of the json tag on UpdateWorkflowInput, not of this call.
func workflowUpdateInput(plan workflowModel) client.UpdateWorkflowInput {
	return client.UpdateWorkflowInput{
		Name:            stringPtr(plan.Name),
		Description:     stringPtr(plan.Description),
		IntervalSeconds: int64Ptr(plan.IntervalSeconds),
	}
}

// unresolvedInputDiagnostics rejects the inputs that are read once, at publish
// time, while they are still unknown. Terraform re-plans with dependencies
// resolved before it applies, so this should not be reachable — but every
// alternative if it ever is comes out worse: an unknown template_slug falls out
// of the template branch and quietly creates a plain workflow instead, and an
// unknown canvas is dropped from the published version and then written into
// state, failing the apply with a far vaguer message than this one.
func unresolvedInputDiagnostics(plan workflowModel) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, input := range []struct {
		name    string
		unknown bool
	}{
		{"template_slug", plan.TemplateSlug.IsUnknown()},
		{"canvas_data_json", plan.CanvasDataJSON.IsUnknown()},
	} {
		if !input.unknown {
			continue
		}
		diags.Append(diag.NewAttributeErrorDiagnostic(
			path.Root(input.name),
			"Value is not known at apply time",
			fmt.Sprintf("%s is read once, when the workflow version is published, so it has to be "+
				"known before the workflow is created. Remove whatever leaves it unknown at plan "+
				"time, or give it a literal value.", input.name),
		))
	}
	return diags
}

// shouldRepublishCanvas is deliberately not shouldPublishGraph. For the graph, a
// null plan means "no graph configured, publish nothing". For the canvas it
// means "stop pinning this layout", and the only way to act on that is to
// republish the graph without a canvas so the API lays it out again. Treating
// null as "nothing to do" left the pinned layout on the published version while
// Terraform quietly dropped it from state.
func shouldRepublishCanvas(ctx context.Context, planned, stored jsontypes.Normalized) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if planned.IsUnknown() || stored.IsUnknown() {
		return false, diags
	}
	if planned.IsNull() != stored.IsNull() {
		// Newly pinned, or newly dropped. Both change the published version.
		return true, diags
	}
	if planned.IsNull() {
		return false, diags
	}

	unchanged, d := planned.StringSemanticEquals(ctx, stored)
	diags.Append(d...)
	if diags.HasError() {
		return false, diags
	}
	return !unchanged, diags
}

// canvasNeedsGraphDiagnostic rejects a layout pinned with no graph to publish it
// against. A canvas only ever reaches the API as part of a workflow version, so
// on its own it is silently dropped — the same shape of bug as `active = true`
// on a workflow with no published version.
func canvasNeedsGraphDiagnostic(graph, canvas jsontypes.Normalized) diag.Diagnostic {
	graphMissing := graph.IsNull() || graph.IsUnknown()
	canvasSet := !canvas.IsNull() && !canvas.IsUnknown()
	if !graphMissing || !canvasSet {
		return nil
	}
	return diag.NewAttributeErrorDiagnostic(
		path.Root("canvas_data_json"),
		"Canvas layout with no graph to publish it with",
		"canvas_data_json is published as part of a workflow version, so it needs an "+
			"execution graph to travel with. Set execution_graph_json as well, or drop "+
			"canvas_data_json and let the API lay out the graph the workflow already has.",
	)
}

// shouldPublishGraph decides whether an update has to cut a new workflow
// version on account of one of the two JSON payloads a version carries. The
// comparison is semantic, so reformatting — a different key order, different
// indentation — publishes nothing. A plan with no value publishes nothing
// either; a value that was never in state always publishes.
func shouldPublishGraph(ctx context.Context, planned, stored jsontypes.Normalized) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if planned.IsNull() || planned.IsUnknown() {
		return false, diags
	}
	if stored.IsNull() || stored.IsUnknown() {
		return true, diags
	}

	unchanged, d := planned.StringSemanticEquals(ctx, stored)
	diags.Append(d...)
	if diags.HasError() {
		return false, diags
	}
	return !unchanged, diags
}

// publishGraph creates a new workflow version from JSON and publishes it. A
// null canvas is left out of the request, which is how the API is asked to
// generate the layout itself.
func (r *workflowResource) publishGraph(ctx context.Context, workflowID int64, graphJSON, canvasJSON jsontypes.Normalized) error {
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
// workflow's published version, both null when nothing is published yet.
func (r *workflowResource) readPublishedGraph(ctx context.Context, w *client.Workflow) (jsontypes.Normalized, jsontypes.Normalized, error) {
	null := jsontypes.NewNormalizedNull()
	if w.PublishedVersionID == nil {
		return null, null, nil
	}

	version, err := r.client.GetWorkflowVersion(ctx, w.ID, *w.PublishedVersionID)
	if err != nil {
		return null, null, fmt.Errorf("reading version %d: %w", *w.PublishedVersionID, err)
	}

	graph, err := marshalGraph(version.ExecutionGraph)
	if err != nil {
		return null, null, fmt.Errorf("encoding execution_graph: %w", err)
	}
	canvas, err := marshalGraph(version.CanvasData)
	if err != nil {
		return null, null, fmt.Errorf("encoding canvas_data: %w", err)
	}
	return graph, canvas, nil
}

// resolveUnknownGraph settles execution_graph_json when the configuration left
// it unset. Optional+Computed with no schema default means the plan holds
// unknown on create, and an unknown left in state fails the apply outright with
// "Provider produced inconsistent result after apply". After a template
// instantiation the published version is the only place the value can come
// from; where nothing is published the answer is null.
//
// A configured graph keeps its planned value untouched: writing the API's
// re-serialisation of it back would break the plan contract the moment the
// server normalises a graph at all.
func (r *workflowResource) resolveUnknownGraph(ctx context.Context, w *client.Workflow, m *workflowModel) error {
	if !m.ExecutionGraphJSON.IsUnknown() {
		return nil
	}

	graph, _, err := r.readPublishedGraph(ctx, w)
	if err != nil {
		return err
	}
	m.ExecutionGraphJSON = graph
	return nil
}

// activationErrorDetail explains the 422 the API returns for a workflow with no
// published version to activate. Activating unconditionally is what turned that
// case from a silent no-op into an error, so the error has to say what to do.
func activationErrorDetail(err error) string {
	if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 422 {
		return fmt.Sprintf("%s\n\nA workflow needs a published version before it can be activated. "+
			"Set execution_graph_json or template_slug, or publish a version outside Terraform first.", err.Error())
	}
	return err.Error()
}

// marshalGraph encodes an API graph payload, returning null for an absent one.
func marshalGraph(graph map[string]interface{}) (jsontypes.Normalized, error) {
	if graph == nil {
		return jsontypes.NewNormalizedNull(), nil
	}
	encoded, err := json.Marshal(graph)
	if err != nil {
		return jsontypes.NewNormalizedNull(), err
	}
	return jsontypes.NewNormalizedValue(string(encoded)), nil
}

func mapWorkflowToState(w *client.Workflow, state *workflowModel) {
	state.ID = types.Int64Value(w.ID)
	state.Name = types.StringValue(w.Name)
	state.Description = optionalString(w.Description)
	state.IntervalSeconds = optionalInt64(w.IntervalSeconds)
	state.Status = types.StringValue(w.Status)
	state.Active = types.BoolValue(w.Status == "active")
	// Both are derived from the published graph, so a draft has neither.
	state.TriggerType = optionalString(w.TriggerType)
	state.TriggerTypeLabel = optionalString(w.TriggerTypeLabel)
	state.PublishedVersionID = optionalInt64(w.PublishedVersionID)
	state.NextEvaluationAt = optionalString(w.NextEvaluationAt)
	state.LastEvaluationAt = optionalString(w.LastEvaluationAt)
	state.CreatedAt = types.StringValue(w.CreatedAt)
	state.UpdatedAt = types.StringValue(w.UpdatedAt)

	// execution_graph_json and canvas_data_json live on the published version,
	// not on the workflow object — callers resolve them separately. template_slug
	// is a creation-time input the API never echoes back, so it is left alone.
}
