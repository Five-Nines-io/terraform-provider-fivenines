package datasources

import (
	"context"
	"strconv"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &incidentsDataSource{}

type incidentsDataSource struct {
	client *client.Client
}

type incidentsModel struct {
	Status          types.String `tfsdk:"status"`
	Q               types.String `tfsdk:"q"`
	HostID          types.String `tfsdk:"host_id"`
	TaskID          types.String `tfsdk:"task_id"`
	UptimeMonitorID types.String `tfsdk:"uptime_monitor_id"`
	WorkflowID      types.Int64  `tfsdk:"workflow_id"`
	From            types.String `tfsdk:"from"`
	To              types.String `tfsdk:"to"`
	UpdatedSince    types.String `tfsdk:"updated_since"`
	Order           types.String `tfsdk:"order"`
	Direction       types.String `tfsdk:"direction"`

	Incidents []incidentModel `tfsdk:"incidents"`
}

type incidentModel struct {
	ID              types.String `tfsdk:"id"`
	Title           types.String `tfsdk:"title"`
	Summary         types.String `tfsdk:"summary"`
	Status          types.String `tfsdk:"status"`
	Public          types.Bool   `tfsdk:"public"`
	HostID          types.String `tfsdk:"host_id"`
	WorkflowID      types.String `tfsdk:"workflow_id"`
	TaskID          types.String `tfsdk:"task_id"`
	UptimeMonitorID types.String `tfsdk:"uptime_monitor_id"`
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
		Description: "Lists incidents in the organization. All arguments are server-side filters; omitting them lists every incident.",
		Attributes: map[string]schema.Attribute{
			"status": schema.StringAttribute{
				Description: "Only incidents with this status. One of `unknown`, `warning`, `resolved`, `open`, `muted`, `acknowledged`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("unknown", "warning", "resolved", "open", "muted", "acknowledged"),
				},
			},
			"q": schema.StringAttribute{
				Description: "Case-insensitive substring match on the incident title.",
				Optional:    true,
			},
			"host_id": schema.StringAttribute{
				Description: "Only incidents affecting this instance (UUID).",
				Optional:    true,
			},
			"task_id": schema.StringAttribute{
				Description: "Only incidents affecting this task (UUID).",
				Optional:    true,
			},
			"uptime_monitor_id": schema.StringAttribute{
				Description: "Only incidents affecting this uptime monitor (UUID).",
				Optional:    true,
			},
			"workflow_id": schema.Int64Attribute{
				Description: "Only incidents raised by this workflow.",
				Optional:    true,
			},
			"from": schema.StringAttribute{
				Description: "Only incidents whose active window reaches this ISO 8601 time or later. " +
					"This is the incident's duration, not its creation time: an incident that opened last week and is still open matches a `from` of yesterday.",
				Optional: true,
			},
			"to": schema.StringAttribute{
				Description: "Exclusive upper bound of the active window (ISO 8601). Combined with `from` the window is the half-open range `[from, to)`, " +
					"so an incident starting exactly at `to` does not match and `from == to` matches nothing. An inverted window is an API error, not an empty list.",
				Optional: true,
			},
			"updated_since": schema.StringAttribute{
				Description: "Only incidents whose `updated_at` is at or after this ISO 8601 timestamp (inclusive).",
				Optional:    true,
			},
			"order": schema.StringAttribute{
				Description: "Sort column: `created_at`, `updated_at` or `title`. Defaults to `created_at`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("created_at", "updated_at", "title"),
				},
			},
			"direction": schema.StringAttribute{
				Description: "Sort direction, `asc` or `desc`. Defaults to `desc`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
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
							Description: "Current status (unknown, warning, resolved, open, muted, acknowledged).",
							Computed:    true,
						},
						"public": schema.BoolAttribute{
							Description: "Whether the incident is shown on the organization's public status pages.",
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
						"uptime_monitor_id": schema.StringAttribute{
							Description: "Associated uptime monitor ID.",
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
							Description: "Duration of the incident in whole seconds. For an open incident this is the elapsed time so far.",
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
	d.client = configureClient(req, resp)
}

func (d *incidentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state incidentsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	incidents, err := d.client.ListIncidents(ctx, client.IncidentListOptions{
		Status:          filterString(state.Status),
		Q:               filterString(state.Q),
		HostID:          filterString(state.HostID),
		TaskID:          filterString(state.TaskID),
		UptimeMonitorID: filterString(state.UptimeMonitorID),
		WorkflowID:      filterInt64(state.WorkflowID),
		From:            filterString(state.From),
		To:              filterString(state.To),
		UpdatedSince:    filterString(state.UpdatedSince),
		Order:           filterString(state.Order),
		Direction:       filterString(state.Direction),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing incidents", err.Error())
		return
	}

	// Sized, never nil: a filter that matches nothing has to read back as an
	// empty list, since a `for` expression over a null one fails at plan time.
	state.Incidents = make([]incidentModel, len(incidents))
	for i, inc := range incidents {
		m := incidentModel{
			ID:        types.StringValue(strconv.FormatInt(inc.ID, 10)),
			Title:     types.StringValue(inc.Title),
			Summary:   types.StringValue(inc.Summary),
			Status:    types.StringValue(inc.Status),
			Public:    types.BoolValue(inc.Public),
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
		m.UptimeMonitorID = optionalString(inc.UptimeMonitorID)
		m.StartedAt = optionalString(inc.StartedAt)
		m.EndedAt = optionalString(inc.EndedAt)
		m.DurationSeconds = optionalInt64(inc.DurationSeconds)
		state.Incidents[i] = m
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
