package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &sslStatusDataSource{}

type sslStatusDataSource struct {
	client *client.Client
}

type sslStatusModel struct {
	Monitors          types.List        `tfsdk:"monitors"`
	CollapseScope     types.Bool        `tfsdk:"collapse_scope"`
	Aggregation       types.String      `tfsdk:"aggregation"`
	SoonestExpiryDays types.Float64     `tfsdk:"soonest_expiry_days"`
	Items             []metricItemModel `tfsdk:"items"`
}

func NewSSLStatusDataSource() datasource.DataSource {
	return &sslStatusDataSource{}
}

func (d *sslStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssl_status"
}

func (d *sslStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Days until each uptime monitor's TLS certificate expires, read from its last probe. " +
			"A snapshot of now — no time range applies.\n\n" +
			"Soonest expiry first. Monitors with no certificate data (tcp, icmp and dns monitors, or one never " +
			"probed) are omitted rather than reported as 0 days, so a missing monitor never reads as an " +
			"imminent expiry. A negative value means the certificate has already expired. Requires the uptime " +
			"monitoring feature." + ephemeralNote,
		Attributes: map[string]schema.Attribute{
			"monitors": schema.ListAttribute{
				Description: "Uptime monitor UUIDs to read. Monitors your organization does not own are " +
					"omitted. May not be empty: an empty selection reads back as \"no certificate data\", " +
					"which is indistinguishable from a healthy fleet.",
				ElementType: types.StringType,
				Required:    true,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
			},
			"collapse_scope": schema.BoolAttribute{
				Description: "Reduce the per-monitor values to a single value using `aggregation`.",
				Optional:    true,
			},
			"aggregation": schema.StringAttribute{
				Description: "How `collapse_scope` reduces: `min` (default, the next certificate to expire), " +
					"`avg` or `max`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("avg", "min", "max"),
				},
			},
			"soonest_expiry_days": schema.Float64Attribute{
				Description: "Days until the next certificate expires — the lowest value in `items`, negative " +
					"once expired. Null when no requested monitor has certificate data, so an alert on it " +
					"cannot silently read a missing certificate as a healthy one.",
				Computed: true,
			},
			"items": metricItemsAttribute("Days to expiry per monitor, soonest first."),
		},
	}
}

func (d *sslStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *sslStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config sslStatusModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitors, diags := stringSlice(ctx, config.Monitors)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := d.client.MetricsMonitorSSL(ctx, client.MetricsMonitorSSLRequest{
		Monitors:      monitors,
		CollapseScope: filterBool(config.CollapseScope),
		Aggregation:   config.Aggregation.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error reading certificate expiry", err.Error())
		return
	}

	state := config
	state.SoonestExpiryDays = lowestValue(result.Result.Items)
	state.Items = mapMetricItems(result.Result.Items)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
