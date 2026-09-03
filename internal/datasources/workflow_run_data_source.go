package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &workflowRunDataSource{}

type workflowRunDataSource struct {
	client *client.Client
}

type workflowRunModel struct {
	WorkflowID        types.Int64  `tfsdk:"workflow_id"`
	RunID             types.Int64  `tfsdk:"run_id"`
	Status            types.String `tfsdk:"status"`
	ResourceKey       types.String `tfsdk:"resource_key"`
	WorkflowVersionID types.Int64  `tfsdk:"workflow_version_id"`
	StartedAt         types.String `tfsdk:"started_at"`
	CompletedAt       types.String `tfsdk:"completed_at"`
	DurationSeconds   types.Int64  `tfsdk:"duration_seconds"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
	Error             types.String `tfsdk:"error"`
	TriggerOutputJSON types.String `tfsdk:"trigger_output_json"`

	Steps []workflowRunStepModel `tfsdk:"steps"`
}

type workflowRunStepModel struct {
	ID              types.Int64   `tfsdk:"id"`
	NodeID          types.String  `tfsdk:"node_id"`
	NodeType        types.String  `tfsdk:"node_type"`
	Status          types.String  `tfsdk:"status"`
	ErrorMessage    types.String  `tfsdk:"error_message"`
	OutputDataJSON  types.String  `tfsdk:"output_data_json"`
	StartedAt       types.String  `tfsdk:"started_at"`
	CompletedAt     types.String  `tfsdk:"completed_at"`
	DurationSeconds types.Float64 `tfsdk:"duration_seconds"`
	CreatedAt       types.String  `tfsdk:"created_at"`
}

func NewWorkflowRunDataSource() datasource.DataSource {
	return &workflowRunDataSource{}
}

func (d *workflowRunDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow_run"
}

func (d *workflowRunDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads a single workflow run with its per-step detail. The `fivenines_workflow_runs` index returns headers only, " +
			"so a `failed` status there says a run broke but not where — `steps[].status` and `steps[].error_message` answer that.",
		Attributes: map[string]schema.Attribute{
			"workflow_id": schema.Int64Attribute{
				Description: "Workflow the run belongs to.",
				Required:    true,
			},
			"run_id": schema.Int64Attribute{
				Description: "Run ID. Run IDs are not global: a run belonging to another workflow is not found here.",
				Required:    true,
			},
			"status": schema.StringAttribute{
				Description: "Run status (running, completed, failed, cancelled).",
				Computed:    true,
			},
			"resource_key": schema.StringAttribute{
				Description: "The subject this run is for on a fan-out workflow (one run per host, guest, pool, ...). Null on a workflow that dispatches once.",
				Computed:    true,
			},
			"workflow_version_id": schema.Int64Attribute{
				Description: "The workflow version that was executed — not necessarily the currently published one.",
				Computed:    true,
			},
			"started_at": schema.StringAttribute{
				Description: "When the run started.",
				Computed:    true,
			},
			"completed_at": schema.StringAttribute{
				Description: "When the run completed.",
				Computed:    true,
			},
			"duration_seconds": schema.Int64Attribute{
				Description: "Run duration in whole seconds. For a run still running this is the elapsed time so far, not a final duration.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description: "When the run was created.",
				Computed:    true,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp.",
				Computed:    true,
			},
			"error": schema.StringAttribute{
				Description: "Run-level failure message. A run can fail between steps — a trigger that raised before any step row existed — in which case no step explains it and this is the only account of what happened.",
				Computed:    true,
			},
			"trigger_output_json": schema.StringAttribute{
				Description: "What the trigger node emitted, as JSON. Decode with `jsondecode()`.",
				Computed:    true,
			},
			"steps": schema.ListNestedAttribute{
				Description: "Every node execution, in the order the engine created them.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Step ID.",
							Computed:    true,
						},
						"node_id": schema.StringAttribute{
							Description: "The node's ID within the version's execution graph (e.g. `trigger_1`).",
							Computed:    true,
						},
						"node_type": schema.StringAttribute{
							Description: "The node's type.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: "Step status (pending, running, completed, failed, waiting, skipped). `waiting` is a node holding for a condition, not a failure; `skipped` means the graph routed around it.",
							Computed:    true,
						},
						"error_message": schema.StringAttribute{
							Description: "Set on a failed step. This is the answer to \"which node broke\".",
							Computed:    true,
						},
						"output_data_json": schema.StringAttribute{
							Description: "Whatever the node produced, as JSON, its shape depending on `node_type`. Decode with `jsondecode()`.",
							Computed:    true,
						},
						"started_at": schema.StringAttribute{
							Description: "When the step started.",
							Computed:    true,
						},
						"completed_at": schema.StringAttribute{
							Description: "When the step completed.",
							Computed:    true,
						},
						"duration_seconds": schema.Float64Attribute{
							Description: "Step duration in seconds, millisecond resolution. Null — never 0 — while the step has not both started and finished.",
							Computed:    true,
						},
						"created_at": schema.StringAttribute{
							Description: "When the step was created.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *workflowRunDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *workflowRunDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state workflowRunModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	run, err := d.client.GetWorkflowRun(ctx, state.WorkflowID.ValueInt64(), state.RunID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Error reading workflow run", err.Error())
		return
	}

	state.Status = types.StringValue(run.Status)
	state.ResourceKey = optionalString(run.ResourceKey)
	state.WorkflowVersionID = types.Int64Value(run.WorkflowVersionID)
	state.StartedAt = optionalString(run.StartedAt)
	state.CompletedAt = optionalString(run.CompletedAt)
	state.DurationSeconds = optionalInt64(run.DurationSeconds)
	state.CreatedAt = types.StringValue(run.CreatedAt)
	state.UpdatedAt = types.StringValue(run.UpdatedAt)
	state.Error = optionalString(run.Error)
	state.TriggerOutputJSON = jsonString(run.TriggerOutput)

	state.Steps = make([]workflowRunStepModel, len(run.Steps))
	for i, s := range run.Steps {
		state.Steps[i] = workflowRunStepModel{
			ID:              types.Int64Value(s.ID),
			NodeID:          types.StringValue(s.NodeID),
			NodeType:        types.StringValue(s.NodeType),
			Status:          types.StringValue(s.Status),
			ErrorMessage:    optionalString(s.ErrorMessage),
			OutputDataJSON:  jsonString(s.OutputData),
			StartedAt:       optionalString(s.StartedAt),
			CompletedAt:     optionalString(s.CompletedAt),
			DurationSeconds: optionalFloat64(s.DurationSeconds),
			CreatedAt:       types.StringValue(s.CreatedAt),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
