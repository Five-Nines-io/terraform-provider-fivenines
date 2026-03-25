package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &workflowRunsDataSource{}

type workflowRunsDataSource struct {
	client *client.Client
}

type workflowRunsModel struct {
	WorkflowID types.Int64       `tfsdk:"workflow_id"`
	Runs       []workflowRunItem `tfsdk:"runs"`
}

type workflowRunItem struct {
	ID          types.Int64  `tfsdk:"id"`
	Status      types.String `tfsdk:"status"`
	ResourceKey types.String `tfsdk:"resource_key"`
	StartedAt   types.String `tfsdk:"started_at"`
	CompletedAt types.String `tfsdk:"completed_at"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func NewWorkflowRunsDataSource() datasource.DataSource {
	return &workflowRunsDataSource{}
}

func (d *workflowRunsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow_runs"
}

func (d *workflowRunsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists execution runs for a FiveNines workflow.",
		Attributes: map[string]schema.Attribute{
			"workflow_id": schema.Int64Attribute{
				Description: "Workflow ID to list runs for.",
				Required:    true,
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
							Description: "Run status (pending, running, completed, failed).",
							Computed:    true,
						},
						"resource_key": schema.StringAttribute{
							Description: "Resource key that triggered the run.",
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
						"created_at": schema.StringAttribute{
							Description: "When the run was created.",
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

	runs, err := d.client.ListWorkflowRuns(state.WorkflowID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Error listing workflow runs", err.Error())
		return
	}

	state.Runs = make([]workflowRunItem, len(runs))
	for i, r := range runs {
		state.Runs[i] = workflowRunItem{
			ID:          types.Int64Value(r.ID),
			Status:      types.StringValue(r.Status),
			ResourceKey: types.StringValue(r.ResourceKey),
			StartedAt:   optionalString(r.StartedAt),
			CompletedAt: optionalString(r.CompletedAt),
			CreatedAt:   types.StringValue(r.CreatedAt),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
