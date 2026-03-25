package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &integrationsDataSource{}

type integrationsDataSource struct {
	client *client.Client
}

type integrationsModel struct {
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
}

func NewIntegrationsDataSource() datasource.DataSource {
	return &integrationsDataSource{}
}

func (d *integrationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integrations"
}

func (d *integrationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all FiveNines integrations (notification channels) for the organization.",
		Attributes: map[string]schema.Attribute{
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
							Description: "Integration type.",
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
							Description: "Whether the integration is verified.",
							Computed:    true,
						},
						"created_at": schema.StringAttribute{
							Description: "Creation timestamp.",
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

func (d *integrationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	integrations, err := d.client.ListIntegrations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing integrations", err.Error())
		return
	}

	var state integrationsModel
	for _, i := range integrations {
		state.Integrations = append(state.Integrations, integrationModel{
			ID:        types.Int64Value(i.ID),
			Type:      types.StringValue(i.Type),
			Name:      types.StringValue(i.Name),
			Provider:  types.StringValue(i.Provider),
			Enabled:   types.BoolValue(i.Enabled),
			Verified:  types.BoolValue(i.Verified),
			CreatedAt: types.StringValue(i.CreatedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
