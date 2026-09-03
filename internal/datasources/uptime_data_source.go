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

var _ datasource.DataSource = &uptimeDataSource{}

type uptimeDataSource struct {
	client *client.Client
}

type uptimeModel struct {
	Hosts         types.List          `tfsdk:"hosts"`
	Monitors      types.List          `tfsdk:"monitors"`
	Tasks         types.List          `tfsdk:"tasks"`
	From          types.String        `tfsdk:"from"`
	To            types.String        `tfsdk:"to"`
	CollapseScope types.Bool          `tfsdk:"collapse_scope"`
	Aggregation   types.String        `tfsdk:"aggregation"`
	Availability  types.Float64       `tfsdk:"availability"`
	Items         []metricItemModel   `tfsdk:"items"`
	Series        []uptimeSeriesModel `tfsdk:"series"`
}

type uptimeSeriesModel struct {
	Name   types.String       `tfsdk:"name"`
	ID     types.String       `tfsdk:"id"`
	Blocks []uptimeBlockModel `tfsdk:"blocks"`
}

type uptimeBlockModel struct {
	From   types.Int64   `tfsdk:"from"`
	To     types.Int64   `tfsdk:"to"`
	Status types.String  `tfsdk:"status"`
	Uptime types.Float64 `tfsdk:"uptime"`
}

func NewUptimeDataSource() datasource.DataSource {
	return &uptimeDataSource{}
}

func (d *uptimeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_uptime"
}

func (d *uptimeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Availability over a window for instances, uptime monitors or tasks — the SLA number. " +
			"It is computed from incident history by the same code the dashboard renders, including the " +
			"incident-overlap union that stops one outage covered by several incident rows from counting " +
			"several times.\n\n" +
			"Target exactly one kind: `monitors` takes precedence over `tasks`, which takes precedence over " +
			"`hosts`. Instance availability counts only availability incidents — a metric-threshold alert " +
			"(high CPU) does not reduce it. IDs your organization does not own are silently omitted, and the " +
			"combined target count must be 50 or fewer." + ephemeralNote,
		Attributes: map[string]schema.Attribute{
			"hosts": schema.ListAttribute{
				Description: "Instance UUIDs to measure.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"monitors": schema.ListAttribute{
				Description: "Uptime monitor UUIDs to measure. Takes precedence over `tasks` and `hosts`.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"tasks": schema.ListAttribute{
				Description: "Task UUIDs to measure. Takes precedence over `hosts`.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"from": schema.StringAttribute{
				Description: "Start of the window, ISO 8601. Defaults to 30 days ago.",
				Optional:    true,
			},
			"to": schema.StringAttribute{
				Description: "End of the window, ISO 8601. Defaults to now, and is clamped to now. " +
					"The span may not exceed 366 days.",
				Optional: true,
			},
			"collapse_scope": schema.BoolAttribute{
				Description: "Reduce the per-entity percentages to a single value using `aggregation`.",
				Optional:    true,
			},
			"aggregation": schema.StringAttribute{
				Description: "How `collapse_scope` reduces: `avg` (default), `min` (the worst entity) or `max`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("avg", "min", "max"),
				},
			},
			"availability": schema.Float64Attribute{
				Description: "The lowest availability percentage in `items` — the worst target over the window. " +
					"Null when no target returned data, so an SLA gate on it fails loudly instead of reading a " +
					"fabricated zero as an outage.",
				Computed: true,
			},
			"items": metricItemsAttribute("The availability percentage per entity, worst first. This is the SLA figure."),
			"series": schema.ListNestedAttribute{
				Description: "The up/partial/down timeline behind each percentage, one per entity, " +
					"bucketed into roughly 40 blocks across the window.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "Display name of the entity.",
							Computed:    true,
						},
						"id": schema.StringAttribute{
							Description: "UUID of the instance, uptime monitor or task.",
							Computed:    true,
						},
						"blocks": schema.ListNestedAttribute{
							Description: "The buckets of the timeline, oldest first.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"from": schema.Int64Attribute{
										Description: "Unix timestamp of the bucket start, in seconds.",
										Computed:    true,
									},
									"to": schema.Int64Attribute{
										Description: "Unix timestamp of the bucket end, in seconds.",
										Computed:    true,
									},
									"status": schema.StringAttribute{
										Description: "Bucket status: `up`, `partial` or `down`.",
										Computed:    true,
									},
									"uptime": schema.Float64Attribute{
										Description: "Availability percentage within the bucket.",
										Computed:    true,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *uptimeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *uptimeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config uptimeModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hosts, diags := stringSlice(ctx, config.Hosts)
	resp.Diagnostics.Append(diags...)
	monitors, diags := stringSlice(ctx, config.Monitors)
	resp.Diagnostics.Append(diags...)
	tasks, diags := stringSlice(ctx, config.Tasks)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(hosts) == 0 && len(monitors) == 0 && len(tasks) == 0 {
		resp.Diagnostics.AddError("No uptime targets",
			"At least one of hosts, monitors or tasks must be set: availability is measured per entity, "+
				"and an empty target set returns nothing to gate on.")
		return
	}

	result, err := d.client.MetricsUptime(ctx, client.MetricsUptimeRequest{
		Hosts:         hosts,
		Monitors:      monitors,
		Tasks:         tasks,
		CollapseScope: filterBool(config.CollapseScope),
		Aggregation:   config.Aggregation.ValueString(),
		TimeRange:     timeRange(config.From, config.To),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error reading uptime", err.Error())
		return
	}

	state := config
	state.Availability = lowestValue(result.Result.Items)
	state.Items = mapMetricItems(result.Result.Items)
	state.Series = make([]uptimeSeriesModel, 0, len(result.Result.Series))
	for _, s := range result.Result.Series {
		blocks := make([]uptimeBlockModel, 0, len(s.Blocks))
		for _, b := range s.Blocks {
			blocks = append(blocks, uptimeBlockModel{
				From:   types.Int64Value(b.From),
				To:     types.Int64Value(b.To),
				Status: types.StringValue(b.Status),
				Uptime: types.Float64Value(b.Uptime),
			})
		}
		state.Series = append(state.Series, uptimeSeriesModel{
			Name:   types.StringValue(s.Name),
			ID:     nullIfEmpty(s.EntityID()),
			Blocks: blocks,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
