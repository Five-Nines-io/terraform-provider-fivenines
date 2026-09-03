package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &cveStatsDataSource{}

type cveStatsDataSource struct {
	client *client.Client
}

type cveStatsModel struct {
	Metric  types.String `tfsdk:"metric"`
	Format  types.String `tfsdk:"format"`
	GroupBy types.String `tfsdk:"group_by"`
	Limit   types.Int64  `tfsdk:"limit"`
	Hosts   types.List   `tfsdk:"hosts"`
	From    types.String `tfsdk:"from"`
	To      types.String `tfsdk:"to"`

	Value  types.Float64       `tfsdk:"value"`
	Unit   types.String        `tfsdk:"unit"`
	Items  []metricItemModel   `tfsdk:"items"`
	Series []metricSeriesModel `tfsdk:"series"`
}

func NewCveStatsDataSource() datasource.DataSource {
	return &cveStatsDataSource{}
}

func (d *cveStatsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cve_stats"
}

func (d *cveStatsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Organization-wide vulnerability counts across the instances FiveNines scans.\n\n" +
			"The `aggregated` format reads **current state** — the same numbers the Security page shows — and " +
			"ignores the window; `time_series` reads the trend recorded at each scan, which starts at your " +
			"first scan rather than being backfilled. `hosts` is an optional instance filter: instances you do " +
			"not own are dropped, and a filter matching none of yours returns zero rather than the " +
			"organization-wide total." + ephemeralNote,
		Attributes: map[string]schema.Attribute{
			"metric": schema.StringAttribute{
				Description: "`cve_count` for every finding, `cve_actionable_count` for the patchable work " +
					"queue (a fix exists and is not subscription-gated).",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("cve_count", "cve_actionable_count"),
				},
			},
			"format": schema.StringAttribute{
				Description: "`aggregated` (default) returns current state in `items`; `time_series` returns " +
					"the trend in `series`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("aggregated", "time_series"),
				},
			},
			"group_by": schema.StringAttribute{
				Description: "`severity` buckets Critical, High, Medium, Low and Unknown, worst first and " +
					"empty buckets omitted; `host` ranks instances.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("severity", "host"),
				},
			},
			"limit": schema.Int64Attribute{
				Description: "Top-N for a `group_by` ranking.",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.Between(1, 100),
				},
			},
			"hosts": schema.ListAttribute{
				Description: "Optional instance filter. Omit for the whole organization; 50 UUIDs at most. " +
					"An explicitly empty list is rejected rather than sent: the API reads an absent filter " +
					"as the whole organization, so `hosts = []` would silently answer with the org-wide " +
					"total under a name that says otherwise.",
				ElementType: types.StringType,
				Optional:    true,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
			},
			"from": schema.StringAttribute{
				Description: "Start of the `time_series` window, ISO 8601. Defaults to 30 days ago.",
				Optional:    true,
			},
			"to": schema.StringAttribute{
				Description: "End of the `time_series` window, ISO 8601. Defaults to now, and is clamped to " +
					"now. The span may not exceed 366 days.",
				Optional: true,
			},
			"value": schema.Float64Attribute{
				Description: "The single count for an ungrouped `aggregated` read. Null when grouped, when " +
					"reading a `time_series`, or when the scan found nothing.",
				Computed: true,
			},
			"unit": schema.StringAttribute{
				Description: "Unit of the values, `count`. Null when the API omits it.",
				Computed:    true,
			},
			"items": metricItemsAttribute("The counts for the `aggregated` format: one row ungrouped, or the " +
				"severity buckets or host ranking when `group_by` is set. Empty for `time_series`."),
			"series": metricSeriesAttribute("The trend over the window, one series per `group_by` bucket. " +
				"Populated for the `time_series` format; empty for `aggregated`."),
		},
	}
}

func (d *cveStatsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *cveStatsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config cveStatsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hosts, diags := stringSlice(ctx, config.Hosts)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := d.client.MetricsCveStats(ctx, client.MetricsCveStatsRequest{
		Metric:    config.Metric.ValueString(),
		Format:    config.Format.ValueString(),
		GroupBy:   config.GroupBy.ValueString(),
		Limit:     filterInt64(config.Limit),
		Hosts:     hosts,
		TimeRange: timeRange(config.From, config.To),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error reading CVE stats", err.Error())
		return
	}

	state := config
	state.Value = singleValue(result.Result.Items, config.GroupBy)
	state.Unit = nullIfEmpty(result.Result.Unit)
	state.Items = mapMetricItems(result.Result.Items)
	series, err := mapMetricSeries(result.Result.Series)
	if err != nil {
		resp.Diagnostics.AddError("Error reading CVE stats", err.Error())
		return
	}
	state.Series = series

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
