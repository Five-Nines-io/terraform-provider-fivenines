package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &hostGroupsDataSource{}

type hostGroupsDataSource struct {
	client *client.Client
}

type hostGroupsModel struct {
	Q          types.String     `tfsdk:"q"`
	HostGroups []hostGroupModel `tfsdk:"host_groups"`
}

type hostGroupModel struct {
	ID        types.Int64  `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Position  types.Int64  `tfsdk:"position"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func NewHostGroupsDataSource() datasource.DataSource {
	return &hostGroupsDataSource{}
}

func (d *hostGroupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host_groups"
}

func (d *hostGroupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up host groups, so a host group ID can be wired in by name instead of being hardcoded. Groups are returned in display order (position, then name).",
		Attributes: map[string]schema.Attribute{
			"q": schema.StringAttribute{
				Description: "Filter on a case-insensitive substring of the group name. Applied server-side; omit to list every group.",
				Optional:    true,
			},
			"host_groups": schema.ListNestedAttribute{
				Description: "List of host groups.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Host group ID.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Host group name.",
							Computed:    true,
						},
						"position": schema.Int64Attribute{
							Description: "1-based sort order. Groups created without one keep the default 0.",
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

func (d *hostGroupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *hostGroupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state hostGroupsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groups, err := d.client.ListHostGroups(ctx, state.Q.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error listing host groups", err.Error())
		return
	}

	// Sized rather than appended so a filter that matches nothing reads back as
	// an empty list, not null.
	state.HostGroups = make([]hostGroupModel, len(groups))
	for i, g := range groups {
		state.HostGroups[i] = hostGroupModel{
			ID:        types.Int64Value(g.ID),
			Name:      types.StringValue(g.Name),
			Position:  types.Int64Value(g.Position),
			CreatedAt: types.StringValue(g.CreatedAt),
			UpdatedAt: types.StringValue(g.UpdatedAt),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
