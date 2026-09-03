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

var _ datasource.DataSource = &integrationsDataSource{}

type integrationsDataSource struct {
	client *client.Client
}

type integrationsModel struct {
	Type         types.String `tfsdk:"type"`
	Enabled      types.Bool   `tfsdk:"enabled"`
	Q            types.String `tfsdk:"q"`
	UpdatedSince types.String `tfsdk:"updated_since"`
	Order        types.String `tfsdk:"order"`
	Direction    types.String `tfsdk:"direction"`

	Integrations []integrationModel `tfsdk:"integrations"`
}

type integrationModel struct {
	ID        types.Int64  `tfsdk:"id"`
	Type      types.String `tfsdk:"type"`
	Name      types.String `tfsdk:"name"`
	Provider  types.String `tfsdk:"provider"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	Verified  types.Bool   `tfsdk:"verified"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func NewIntegrationsDataSource() datasource.DataSource {
	return &integrationsDataSource{}
}

func (d *integrationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integrations"
}

func (d *integrationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists FiveNines integrations (notification channels) for the organization. All arguments are server-side filters; omitting them lists every channel.",
		Attributes: map[string]schema.Attribute{
			"type": schema.StringAttribute{
				Description: "Only channels of this type. This is the backing class name the `type` field returns, not the short key used to create one: send `SlackIntegration`, not `slack`. " +
					"One of `SlackIntegration`, `TelegramIntegration`, `DiscordIntegration`, `TeamsIntegration`, `EmailIntegration`, `WebhookIntegration`, `PushoverIntegration`, `PagerdutyIntegration`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf(
						"SlackIntegration",
						"TelegramIntegration",
						"DiscordIntegration",
						"TeamsIntegration",
						"EmailIntegration",
						"WebhookIntegration",
						"PushoverIntegration",
						"PagerdutyIntegration",
					),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Only channels with this enabled flag. Pair it with `verified` on the rows: a workflow notification node refuses to deliver to an unverified channel even when it is enabled.",
				Optional:    true,
			},
			"q": schema.StringAttribute{
				Description: "Case-insensitive substring match on the channel's `name` column. A channel that was never given a name is not matched — the name returned for one is the channel's own identifier, which is not searchable.",
				Optional:    true,
			},
			"updated_since": schema.StringAttribute{
				Description: "Only channels whose `updated_at` is at or after this ISO 8601 timestamp (inclusive).",
				Optional:    true,
			},
			"order": schema.StringAttribute{
				Description: "Sort column: `created_at`, `updated_at`, `name` or `type`. Defaults to `created_at`. Channels whose `name` column was never set sort together at one end.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("created_at", "updated_at", "name", "type"),
				},
			},
			"direction": schema.StringAttribute{
				Description: "Sort direction, `asc` or `desc`. Defaults to `desc`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"integrations": schema.ListNestedAttribute{
				Description: "List of integrations.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Integration ID.",
							Computed:    true,
						},
						"type": schema.StringAttribute{
							Description: "Integration type (backing class name, e.g. `SlackIntegration`).",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Integration name.",
							Computed:    true,
						},
						"provider": schema.StringAttribute{
							Description: "Provider (email, slack, discord, etc.).",
							Computed:    true,
						},
						"enabled": schema.BoolAttribute{
							Description: "Whether the integration is enabled.",
							Computed:    true,
						},
						"verified": schema.BoolAttribute{
							Description: "Whether the integration is verified. Workflow notification nodes refuse to deliver to an unverified channel.",
							Computed:    true,
						},
						"created_at": schema.StringAttribute{
							Description: "Creation timestamp.",
							Computed:    true,
						},
						"updated_at": schema.StringAttribute{
							Description: "Last update timestamp. This is the field `updated_since` compares.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *integrationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *integrationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state integrationsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	integrations, err := d.client.ListIntegrations(ctx, client.IntegrationListOptions{
		Type:         filterString(state.Type),
		Enabled:      filterBool(state.Enabled),
		Q:            filterString(state.Q),
		UpdatedSince: filterString(state.UpdatedSince),
		Order:        filterString(state.Order),
		Direction:    filterString(state.Direction),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing integrations", err.Error())
		return
	}

	// Sized, never nil: a filter that matches nothing has to read back as an
	// empty list, since a `for` expression over a null one fails at plan time.
	state.Integrations = make([]integrationModel, len(integrations))
	for i, integration := range integrations {
		state.Integrations[i] = integrationModel{
			ID:        types.Int64Value(integration.ID),
			Type:      types.StringValue(integration.Type),
			Name:      types.StringValue(integration.Name),
			Provider:  types.StringValue(integration.Provider),
			Enabled:   types.BoolValue(integration.Enabled),
			Verified:  types.BoolValue(integration.Verified),
			CreatedAt: types.StringValue(integration.CreatedAt),
			UpdatedAt: types.StringValue(integration.UpdatedAt),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
