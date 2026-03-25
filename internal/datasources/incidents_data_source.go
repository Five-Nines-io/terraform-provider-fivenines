package datasources

import (
	"context"
	"strconv"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &incidentsDataSource{}

type incidentsDataSource struct {
	client *client.Client
}

type incidentsModel struct {
	Incidents []incidentModel `tfsdk:"incidents"`
}

type incidentModel struct {
	ID              types.String `tfsdk:"id"`
	Title           types.String `tfsdk:"title"`
	Summary         types.String `tfsdk:"summary"`
	Status          types.String `tfsdk:"status"`
	HostID          types.String `tfsdk:"host_id"`
	WorkflowID      types.String `tfsdk:"workflow_id"`
	TaskID          types.String `tfsdk:"task_id"`
	StartedAt       types.String `tfsdk:"started_at"`
	EndedAt         types.String `tfsdk:"ended_at"`
	DurationSeconds types.Int64  `tfsdk:"duration_seconds"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func NewIncidentsDataSource() datasource.DataSource {
	return &incidentsDataSource{}
}

func (d *incidentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_incidents"
}

func (d *incidentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all incidents in the organization.",
		Attributes: map[string]schema.Attribute{
			"incidents": schema.ListNestedAttribute{
				Description: "List of incidents.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "Unique identifier.",
							Computed:    true,
						},
						"title": schema.StringAttribute{
							Description: "Incident title.",
							Computed:    true,
						},
						"summary": schema.StringAttribute{
							Description: "Incident summary.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: "Current status (triggered, acknowledged, muted, resolved).",
							Computed:    true,
						},
						"host_id": schema.StringAttribute{
							Description: "Associated host ID.",
							Computed:    true,
						},
						"workflow_id": schema.StringAttribute{
							Description: "Workflow that created this incident.",
							Computed:    true,
						},
						"task_id": schema.StringAttribute{
							Description: "Associated task ID.",
							Computed:    true,
						},
						"started_at": schema.StringAttribute{
							Description: "When the incident started.",
							Computed:    true,
						},
						"ended_at": schema.StringAttribute{
							Description: "When the incident ended.",
							Computed:    true,
						},
						"duration_seconds": schema.Int64Attribute{
							Description: "Duration of the incident in seconds.",
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
				},
			},
		},
	}
}

func (d *incidentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type",
			"Expected *client.Client, got unexpected type.")
		return
	}
	d.client = c
}

func (d *incidentsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	incidents, err := d.client.ListIncidents(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing incidents", err.Error())
		return
	}

	var state incidentsModel
	for _, inc := range incidents {
		m := incidentModel{
			ID:        types.StringValue(strconv.FormatInt(inc.ID, 10)),
			Title:     types.StringValue(inc.Title),
			Summary:   types.StringValue(inc.Summary),
			Status:    types.StringValue(inc.Status),
			CreatedAt: types.StringValue(inc.CreatedAt),
			UpdatedAt: types.StringValue(inc.UpdatedAt),
		}
		m.HostID = optionalString(inc.HostID)
		if inc.WorkflowID != nil {
			m.WorkflowID = types.StringValue(strconv.FormatInt(*inc.WorkflowID, 10))
		} else {
			m.WorkflowID = types.StringNull()
		}
		m.TaskID = optionalString(inc.TaskID)
		m.StartedAt = optionalString(inc.StartedAt)
		m.EndedAt = optionalString(inc.EndedAt)
		if inc.DurationSeconds != nil {
			m.DurationSeconds = types.Int64Value(*inc.DurationSeconds)
		} else {
			m.DurationSeconds = types.Int64Null()
		}
		state.Incidents = append(state.Incidents, m)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
