package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &probeRegionsDataSource{}

type probeRegionsDataSource struct {
	client *client.Client
}

type probeRegionsModel struct {
	Regions []probeRegionModel `tfsdk:"regions"`
}

type probeRegionModel struct {
	ID     types.Int64  `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Slug   types.String `tfsdk:"slug"`
	Status types.String `tfsdk:"status"`
}

func NewProbeRegionsDataSource() datasource.DataSource {
	return &probeRegionsDataSource{}
}

func (d *probeRegionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_probe_regions"
}

func (d *probeRegionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all active FiveNines probe regions for uptime monitoring.",
		Attributes: map[string]schema.Attribute{
			"regions": schema.ListNestedAttribute{
				Description: "List of probe regions.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Region ID.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Region name.",
							Computed:    true,
						},
						"slug": schema.StringAttribute{
							Description: "Region slug.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: "Region status.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *probeRegionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *probeRegionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	regions, err := d.client.ListProbeRegions()
	if err != nil {
		resp.Diagnostics.AddError("Error listing probe regions", err.Error())
		return
	}

	var state probeRegionsModel
	for _, r := range regions {
		state.Regions = append(state.Regions, probeRegionModel{
			ID:     types.Int64Value(r.ID),
			Name:   types.StringValue(r.Name),
			Slug:   types.StringValue(r.Slug),
			Status: types.StringValue(r.Status),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
