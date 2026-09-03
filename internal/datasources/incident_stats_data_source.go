package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &incidentStatsDataSource{}

type incidentStatsDataSource struct {
	client *client.Client
}

type incidentStatsModel struct {
	Metric  types.String `tfsdk:"metric"`
	Format  types.String `tfsdk:"format"`
	GroupBy types.String `tfsdk:"group_by"`
	Limit   types.Int64  `tfsdk:"limit"`
	From    types.String `tfsdk:"from"`
	To      types.String `tfsdk:"to"`

	Value  types.Float64       `tfsdk:"value"`
	Unit   types.String        `tfsdk:"unit"`
	Items  []metricItemModel   `tfsdk:"items"`
	Series []metricSeriesModel `tfsdk:"series"`
}

func NewIncidentStatsDataSource() datasource.DataSource {
	return &incidentStatsDataSource{}
}

func (d *incidentStatsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_incident_stats"
}

func (d *incidentStatsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Organization-wide incident analytics: how many incidents opened in a window, and the " +
			"mean time to resolve and to acknowledge them. There is no entity — the metric spans the whole " +
			"organization.\n\n" +
			"`incident_mttr` averages over incidents *resolved* in the window and `incident_mtta` over " +
			"incidents *first acknowledged* in it, both in seconds. In the `time_series` format they emit no " +
			"point for a bucket with no qualifying incident, because a mean of nothing is undefined and a 0 " +
			"would dip the line misleadingly." + ephemeralNote,
		Attributes: map[string]schema.Attribute{
			"metric": schema.StringAttribute{
				Description: "`incident_count` for incidents opened in the window, `incident_mttr` for mean " +
					"seconds to resolve, `incident_mtta` for mean seconds to acknowledge.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("incident_count", "incident_mttr", "incident_mtta"),
				},
			},
			"format": schema.StringAttribute{
				Description: "`aggregated` (default) returns one value in `items`; `time_series` returns the " +
					"trend in `series`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("aggregated", "time_series"),
				},
			},
			"group_by": schema.StringAttribute{
				Description: "`entity` ranks the instances, monitors and tasks that opened the incidents. " +
					"`incident_count` only.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("entity"),
				},
			},
			"limit": schema.Int64Attribute{
				Description: "Top-N for a `group_by` ranking.",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.Between(1, 100),
				},
			},
			"from": schema.StringAttribute{
				Description: "Start of the window, ISO 8601. Defaults to 24 hours ago.",
				Optional:    true,
			},
			"to": schema.StringAttribute{
				Description: "End of the window, ISO 8601. Defaults to now, and is clamped to now. " +
					"The span may not exceed 366 days.",
				Optional: true,
			},
			"value": schema.Float64Attribute{
				Description: "The single figure for an ungrouped `aggregated` read — the count, or the mean " +
					"seconds for MTTR and MTTA. Null when grouped, when reading a `time_series`, or when no " +
					"incident in the window qualified.",
				Computed: true,
			},
			"unit": schema.StringAttribute{
				Description: "Unit of the values: `count` or `duration` (seconds). Null when the API omits it.",
				Computed:    true,
			},
			"items": metricItemsAttribute("The values for the `aggregated` format: one row ungrouped, " +
				"or the ranking when `group_by` is set. Empty for `time_series`."),
			"series": metricSeriesAttribute("The trend over the window. Populated for the `time_series` " +
				"format; empty for `aggregated`."),
		},
	}
}

func (d *incidentStatsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *incidentStatsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config incidentStatsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := d.client.MetricsIncidentStats(ctx, client.MetricsIncidentStatsRequest{
		Metric:    config.Metric.ValueString(),
		Format:    config.Format.ValueString(),
		GroupBy:   config.GroupBy.ValueString(),
		Limit:     filterInt64(config.Limit),
		TimeRange: timeRange(config.From, config.To),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error reading incident stats", err.Error())
		return
	}

	state := config
	state.Value = singleValue(result.Result.Items, config.GroupBy)
	state.Unit = nullIfEmpty(result.Result.Unit)
	state.Items = mapMetricItems(result.Result.Items)
	series, err := mapMetricSeries(result.Result.Series)
	if err != nil {
		resp.Diagnostics.AddError("Error reading incident stats", err.Error())
		return
	}
	state.Series = series

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
