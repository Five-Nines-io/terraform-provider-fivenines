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

var _ datasource.DataSource = &uptimeMonitorsDataSource{}

type uptimeMonitorsDataSource struct {
	client *client.Client
}

type uptimeMonitorsModel struct {
	Status       types.String              `tfsdk:"status"`
	Protocol     types.String              `tfsdk:"protocol"`
	Query        types.String              `tfsdk:"query"`
	UpdatedSince types.String              `tfsdk:"updated_since"`
	Order        types.String              `tfsdk:"order"`
	Direction    types.String              `tfsdk:"direction"`
	Monitors     []uptimeMonitorEntryModel `tfsdk:"monitors"`
}

type uptimeMonitorEntryModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Protocol        types.String `tfsdk:"protocol"`
	Status          types.String `tfsdk:"status"`
	Paused          types.Bool   `tfsdk:"paused"`
	URL             types.String `tfsdk:"url"`
	Hostname        types.String `tfsdk:"hostname"`
	Port            types.Int64  `tfsdk:"port"`
	IntervalSeconds types.Int64  `tfsdk:"interval_seconds"`
	LastCheckAt     types.String `tfsdk:"last_check_at"`
	NextCheckAt     types.String `tfsdk:"next_check_at"`
	LastError       types.String `tfsdk:"last_error"`
	SSLExpiresAt    types.String `tfsdk:"ssl_expires_at"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func NewUptimeMonitorsDataSource() datasource.DataSource {
	return &uptimeMonitorsDataSource{}
}

func (d *uptimeMonitorsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_uptime_monitors"
}

func (d *uptimeMonitorsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the uptime monitors in the organization. All filters are optional and " +
			"are combined; omitting them returns every monitor.",
		Attributes: map[string]schema.Attribute{
			"status": schema.StringAttribute{
				Description: `Only return monitors in this status: "unknown", "up", "down", "paused" or "recovering".`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("unknown", "up", "down", "paused", "recovering"),
				},
			},
			"protocol": schema.StringAttribute{
				Description: `Only return monitors using this protocol: "https", "tcp", "icmp" or "dns".`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("https", "tcp", "icmp", "dns"),
				},
			},
			"query": schema.StringAttribute{
				Description: "Case-insensitive substring match on the monitor name (the API's `q` filter). " +
					"It does not search the URL or hostname.",
				Optional: true,
			},
			"updated_since": schema.StringAttribute{
				Description: "Only return monitors updated at or after this ISO8601 timestamp.",
				Optional:    true,
			},
			"order": schema.StringAttribute{
				Description: `Column to sort by: "created_at", "updated_at" or "name". Defaults to "created_at".`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("created_at", "updated_at", "name"),
				},
			},
			"direction": schema.StringAttribute{
				Description: `Sort direction: "asc" or "desc". Defaults to "desc", newest first.`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"monitors": schema.ListNestedAttribute{
				Description: "Matching uptime monitors.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "Unique identifier (UUID).",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Monitor name.",
							Computed:    true,
						},
						"protocol": schema.StringAttribute{
							Description: "Protocol being monitored.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: `Current status: "unknown", "up", "down", "paused" or "recovering".`,
							Computed:    true,
						},
						"paused": schema.BoolAttribute{
							Description: "Whether the monitor is currently paused.",
							Computed:    true,
						},
						"url": schema.StringAttribute{
							Description: "Monitored URL, for https monitors.",
							Computed:    true,
						},
						"hostname": schema.StringAttribute{
							Description: "Monitored hostname, for tcp/icmp/dns monitors.",
							Computed:    true,
						},
						"port": schema.Int64Attribute{
							Description: "Monitored port, for tcp monitors.",
							Computed:    true,
						},
						"interval_seconds": schema.Int64Attribute{
							Description: "Check interval in seconds.",
							Computed:    true,
						},
						"last_check_at": schema.StringAttribute{
							Description: "Last check time.",
							Computed:    true,
						},
						"next_check_at": schema.StringAttribute{
							Description: "Next scheduled check time.",
							Computed:    true,
						},
						"last_error": schema.StringAttribute{
							Description: "Last error message.",
							Computed:    true,
						},
						"ssl_expires_at": schema.StringAttribute{
							Description: "SSL certificate expiration date.",
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

func (d *uptimeMonitorsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *uptimeMonitorsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state uptimeMonitorsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := client.ListUptimeMonitorsOptions{
		Status:       state.Status.ValueString(),
		Protocol:     state.Protocol.ValueString(),
		Query:        state.Query.ValueString(),
		UpdatedSince: state.UpdatedSince.ValueString(),
		Order:        state.Order.ValueString(),
		Direction:    state.Direction.ValueString(),
	}

	monitors, err := d.client.ListUptimeMonitors(ctx, &opts)
	if err != nil {
		resp.Diagnostics.AddError("Error listing uptime monitors", err.Error())
		return
	}

	// Non-nil even when nothing matches: a nil slice serialises as a null list, and
	// length()/for_each/toset over a null fail. Zero matches is the normal case for
	// a filtered read, so it has to come back as [].
	state.Monitors = make([]uptimeMonitorEntryModel, 0, len(monitors))
	for _, m := range monitors {
		entry := uptimeMonitorEntryModel{
			ID:              types.StringValue(m.ID),
			Name:            types.StringValue(m.Name),
			Protocol:        types.StringValue(m.Protocol),
			Status:          types.StringValue(m.Status),
			Paused:          types.BoolValue(m.Status == client.StatusPaused),
			URL:             types.StringValue(m.URL),
			Hostname:        types.StringValue(m.Hostname),
			IntervalSeconds: types.Int64Value(int64(m.IntervalSeconds)),
			LastCheckAt:     optionalString(m.LastCheckAt),
			NextCheckAt:     optionalString(m.NextCheckAt),
			LastError:       optionalString(m.LastError),
			SSLExpiresAt:    optionalString(m.SSLExpiresAt),
			CreatedAt:       types.StringValue(m.CreatedAt),
			UpdatedAt:       types.StringValue(m.UpdatedAt),
		}
		if m.Port != nil {
			entry.Port = types.Int64Value(int64(*m.Port))
		} else {
			entry.Port = types.Int64Null()
		}
		state.Monitors = append(state.Monitors, entry)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
