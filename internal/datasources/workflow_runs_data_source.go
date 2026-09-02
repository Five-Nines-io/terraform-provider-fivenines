package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &workflowRunsDataSource{}

type workflowRunsDataSource struct {
	client *client.Client
}

type workflowRunsModel struct {
	WorkflowID types.Int64 `tfsdk:"workflow_id"`

	Status       types.String `tfsdk:"status"`
	UpdatedSince types.String `tfsdk:"updated_since"`
	Order        types.String `tfsdk:"order"`
	Direction    types.String `tfsdk:"direction"`

	Runs []workflowRunItem `tfsdk:"runs"`
}

type workflowRunItem struct {
	ID                types.Int64  `tfsdk:"id"`
	Status            types.String `tfsdk:"status"`
	ResourceKey       types.String `tfsdk:"resource_key"`
	WorkflowID        types.Int64  `tfsdk:"workflow_id"`
	WorkflowVersionID types.Int64  `tfsdk:"workflow_version_id"`
	StartedAt         types.String `tfsdk:"started_at"`
	CompletedAt       types.String `tfsdk:"completed_at"`
	DurationSeconds   types.Int64  `tfsdk:"duration_seconds"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func NewWorkflowRunsDataSource() datasource.DataSource {
	return &workflowRunsDataSource{}
}

func (d *workflowRunsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow_runs"
}

func (d *workflowRunsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists execution runs for a FiveNines workflow. Runs are returned as headers only — use the `fivenines_workflow_run` data source for per-step detail.",
		Attributes: map[string]schema.Attribute{
			"workflow_id": schema.Int64Attribute{
				Description: "Workflow ID to list runs for.",
				Required:    true,
			},
			"status": schema.StringAttribute{
				Description: "Only runs with this status. One of `running`, `completed`, `failed`, `cancelled`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("running", "completed", "failed", "cancelled"),
				},
			},
			"updated_since": schema.StringAttribute{
				Description: "Only runs whose `updated_at` is at or after this ISO 8601 timestamp (inclusive).",
				Optional:    true,
			},
			"order": schema.StringAttribute{
				Description: "Sort column: `created_at`, `updated_at`, `started_at` or `completed_at`. Defaults to `created_at`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("created_at", "updated_at", "started_at", "completed_at"),
				},
			},
			"direction": schema.StringAttribute{
				Description: "Sort direction, `asc` or `desc`. Defaults to `desc`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"runs": schema.ListNestedAttribute{
				Description: "List of workflow runs.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Run ID.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: "Run status (running, completed, failed, cancelled).",
							Computed:    true,
						},
						"resource_key": schema.StringAttribute{
							Description: "The subject this run is for on a fan-out workflow (one run per host, guest, pool, ...). Null on a workflow that dispatches once.",
							Computed:    true,
						},
						"workflow_id": schema.Int64Attribute{
							Description: "Workflow this run belongs to.",
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
							Description: "Last update timestamp. A run is written when it starts and again when it finishes, so a completion surfaces on it.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *workflowRunsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type",
			"Expected *client.Client, got unexpected type.")
		return
	}
	d.client = c
}

func (d *workflowRunsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state workflowRunsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	runs, err := d.client.ListWorkflowRuns(ctx, state.WorkflowID.ValueInt64(), client.WorkflowRunListOptions{
		Status:       filterString(state.Status),
		UpdatedSince: filterString(state.UpdatedSince),
		Order:        filterString(state.Order),
		Direction:    filterString(state.Direction),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing workflow runs", err.Error())
		return
	}

	state.Runs = make([]workflowRunItem, len(runs))
	for i, r := range runs {
		state.Runs[i] = workflowRunItem{
			ID:                types.Int64Value(r.ID),
			Status:            types.StringValue(r.Status),
			ResourceKey:       optionalString(r.ResourceKey),
			WorkflowID:        types.Int64Value(r.WorkflowID),
			WorkflowVersionID: types.Int64Value(r.WorkflowVersionID),
			StartedAt:         optionalString(r.StartedAt),
			CompletedAt:       optionalString(r.CompletedAt),
			DurationSeconds:   optionalInt64(r.DurationSeconds),
			CreatedAt:         types.StringValue(r.CreatedAt),
			UpdatedAt:         types.StringValue(r.UpdatedAt),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
