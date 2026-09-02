package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &uptimeMonitorStatusDataSource{}

type uptimeMonitorStatusDataSource struct {
	client *client.Client
}

type uptimeMonitorStatusModel struct {
	ID           types.String `tfsdk:"id"`
	Status       types.String `tfsdk:"status"`
	Paused       types.Bool   `tfsdk:"paused"`
	LastCheckAt  types.String `tfsdk:"last_check_at"`
	NextCheckAt  types.String `tfsdk:"next_check_at"`
	LastError    types.String `tfsdk:"last_error"`
	SSLExpiresAt types.String `tfsdk:"ssl_expires_at"`
}

func NewUptimeMonitorStatusDataSource() datasource.DataSource {
	return &uptimeMonitorStatusDataSource{}
}

func (d *uptimeMonitorStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_uptime_monitor_status"
}

func (d *uptimeMonitorStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the current status of a single uptime monitor. This is the lightweight " +
			"counterpart to the fivenines_uptime_monitor resource: it returns liveness fields only, " +
			"which makes it cheap to refresh repeatedly.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "UUID of the uptime monitor to read.",
				Required:    true,
			},
			"status": schema.StringAttribute{
				Description: `Current status: "unknown", "up", "down", "paused" or "recovering".`,
				Computed:    true,
			},
			"paused": schema.BoolAttribute{
				Description: "Whether the monitor is currently paused.",
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
		},
	}
}

func (d *uptimeMonitorStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *uptimeMonitorStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config uptimeMonitorStatusModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	status, err := d.client.GetUptimeMonitorStatus(ctx, config.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading uptime monitor status", err.Error())
		return
	}

	state := uptimeMonitorStatusModel{
		ID:           config.ID,
		Status:       types.StringValue(status.Status),
		Paused:       types.BoolValue(status.Status == client.StatusPaused),
		LastCheckAt:  optionalString(status.LastCheckAt),
		NextCheckAt:  optionalString(status.NextCheckAt),
		LastError:    optionalString(status.LastError),
		SSLExpiresAt: optionalString(status.SSLExpiresAt),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
