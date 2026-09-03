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

var _ datasource.DataSource = &metricQueryDataSource{}

type metricQueryDataSource struct {
	client *client.Client
}

type metricQueryModel struct {
	Hosts          types.List   `tfsdk:"hosts"`
	Monitors       types.List   `tfsdk:"monitors"`
	Devices        types.List   `tfsdk:"devices"`
	Resource       types.String `tfsdk:"resource"`
	Format         types.String `tfsdk:"format"`
	Aggregation    types.String `tfsdk:"aggregation"`
	GroupBy        types.String `tfsdk:"group_by"`
	Limit          types.Int64  `tfsdk:"limit"`
	MergeLabels    types.Bool   `tfsdk:"merge_labels"`
	MergeMetrics   types.Bool   `tfsdk:"merge_metrics"`
	ScrapeInterval types.Int64  `tfsdk:"scrape_interval"`
	Filters        types.Map    `tfsdk:"filters"`
	Exclude        types.Map    `tfsdk:"exclude"`
	From           types.String `tfsdk:"from"`
	To             types.String `tfsdk:"to"`

	Items  []metricItemModel   `tfsdk:"items"`
	Series []metricSeriesModel `tfsdk:"series"`
}

func NewMetricQueryDataSource() datasource.DataSource {
	return &metricQueryDataSource{}
}

func (d *metricQueryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metric_query"
}

func (d *metricQueryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads any metric in the FiveNines catalogue for instances, uptime monitors or network " +
			"devices — the escape hatch for figures the dedicated data sources do not cover.\n\n" +
			"Query an instance with `hosts`, an SNMP device with `devices`, an uptime monitor with `monitors`. " +
			"The catalogue of `resource` names is documented on `POST /api/v1/metrics/query` in the " +
			"[API reference](https://fivenines.io/api-docs); host-scoped metrics also accept wildcard patterns " +
			"(`load_average*`). IDs your organization does not own are silently scoped out, the combined target " +
			"count must be 50 or fewer, and the window may not span more than 30 days.\n\n" +
			"Availability, certificate expiry and the organization-wide incident and CVE figures are **not** " +
			"in this catalogue — they are not stored series. Use `fivenines_uptime`, `fivenines_ssl_status`, " +
			"`fivenines_incident_stats` and `fivenines_cve_stats` for those." + ephemeralNote,
		Attributes: map[string]schema.Attribute{
			"hosts": schema.ListAttribute{
				Description: "Instance UUIDs to query.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"monitors": schema.ListAttribute{
				Description: "Uptime monitor UUIDs to query, for the `uptime_check_success` and " +
					"`uptime_response_time_ms` metrics.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"devices": schema.ListAttribute{
				Description: "Network device UUIDs to query, for the SNMP `network_*` metrics.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"resource": schema.StringAttribute{
				Description: "Metric name, for example `cpu_usage`.",
				Required:    true,
			},
			"format": schema.StringAttribute{
				Description: "`aggregated` (default) reduces the window to one value per target in `items`; " +
					"`time_series` returns the points over the window in `series`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("aggregated", "time_series"),
				},
			},
			"aggregation": schema.StringAttribute{
				Description: "How the window is reduced: `avg` (default), `sum`, `min`, `max` or `last`. " +
					"`sum` is rejected on percentage metrics.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("avg", "sum", "min", "max", "last"),
				},
			},
			"group_by": schema.StringAttribute{
				Description: "Break the metric down per label. Which value applies depends on the metric: " +
					"`partition_*`, `io_*` and `network_*` take `instance_device`, `cpu_core_usage` takes " +
					"`instance_core`, Redis per-database metrics take `instance_db`, SNMP interfaces take " +
					"`if_name` or `if_index`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("instance", "instance_device", "instance_container", "instance_core",
						"instance_db", "if_name", "if_index", "region", "pool", "osd"),
				},
			},
			"limit": schema.Int64Attribute{
				Description: "Top-N for list-shaped metrics.",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.Between(1, 500),
				},
			},
			"merge_labels": schema.BoolAttribute{
				Description: "Sum every label of the metric (all interfaces, all devices, all containers) " +
					"into a single series per target.",
				Optional: true,
			},
			"merge_metrics": schema.BoolAttribute{
				Description: "Sum related metrics into one series: in plus out for network, read plus write for I/O.",
				Optional:    true,
			},
			"scrape_interval": schema.Int64Attribute{
				Description: "The target's collection interval in seconds. Widens the rate window so a slow " +
					"poller still has two samples in it — usually only needed for `devices`.",
				Optional: true,
			},
			"filters": schema.MapAttribute{
				Description: "Keep only these label values, keyed by label name, " +
					"for example `{ device = [\"sda\", \"sdb\"] }`.",
				ElementType: types.ListType{ElemType: types.StringType},
				Optional:    true,
			},
			"exclude": schema.MapAttribute{
				Description: "The inverse of `filters`: drop these label values. The same keys apply.",
				ElementType: types.ListType{ElemType: types.StringType},
				Optional:    true,
			},
			"from": schema.StringAttribute{
				Description: "Start of the window, ISO 8601. Defaults to one hour ago.",
				Optional:    true,
			},
			"to": schema.StringAttribute{
				Description: "End of the window, ISO 8601. Defaults to now. The span may not exceed 30 days.",
				Optional:    true,
			},
			"items":  metricItemsAttribute("One reduced value per target. Populated for the `aggregated` format; empty for `time_series`."),
			"series": metricSeriesAttribute("The points over the window. Populated for the `time_series` format; empty for `aggregated`."),
		},
	}
}

func (d *metricQueryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *metricQueryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config metricQueryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hosts, diags := stringSlice(ctx, config.Hosts)
	resp.Diagnostics.Append(diags...)
	monitors, diags := stringSlice(ctx, config.Monitors)
	resp.Diagnostics.Append(diags...)
	devices, diags := stringSlice(ctx, config.Devices)
	resp.Diagnostics.Append(diags...)
	filters, diags := labelFilters(ctx, config.Filters)
	resp.Diagnostics.Append(diags...)
	exclude, diags := labelFilters(ctx, config.Exclude)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(hosts) == 0 && len(monitors) == 0 && len(devices) == 0 {
		resp.Diagnostics.AddError("No query targets",
			"At least one of hosts, monitors or devices must be set: a query with no target reads no series "+
				"and would return an empty result indistinguishable from a metric that is not collected.")
		return
	}

	// The API requires format and aggregation; default them here so the common
	// case is a resource name and a target list. Left null in state, since the
	// framework requires optional attributes to read back exactly as configured.
	format := config.Format.ValueString()
	if format == "" {
		format = "aggregated"
	}
	aggregation := config.Aggregation.ValueString()
	if aggregation == "" {
		aggregation = "avg"
	}

	request := client.MetricsQueryRequest{
		Hosts:    hosts,
		Monitors: monitors,
		Devices:  devices,
		Query: client.MetricsQuerySpec{
			Resource:       config.Resource.ValueString(),
			Format:         format,
			Aggregation:    aggregation,
			GroupBy:        config.GroupBy.ValueString(),
			Limit:          filterInt64(config.Limit),
			MergeLabels:    filterBool(config.MergeLabels),
			MergeMetrics:   filterBool(config.MergeMetrics),
			ScrapeInterval: filterInt64(config.ScrapeInterval),
			Filters:        filters,
			Exclude:        exclude,
		},
	}
	if window := timeRange(config.From, config.To); window != nil {
		request.TimeRange = *window
	}

	result, err := d.client.QueryMetrics(ctx, request)
	if err != nil {
		resp.Diagnostics.AddError("Error querying metrics", err.Error())
		return
	}

	state := config
	state.Items = mapMetricItems(result.Result.Items)
	series, err := mapMetricSeries(result.Result.Series)
	if err != nil {
		resp.Diagnostics.AddError("Error querying metrics", err.Error())
		return
	}
	state.Series = series

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
