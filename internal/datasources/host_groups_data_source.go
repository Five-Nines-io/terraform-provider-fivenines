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

var _ datasource.DataSource = &hostGroupsDataSource{}

type hostGroupsDataSource struct {
	client *client.Client
}

type hostGroupsModel struct {
	Query        types.String     `tfsdk:"query"`
	UpdatedSince types.String     `tfsdk:"updated_since"`
	Order        types.String     `tfsdk:"order"`
	Direction    types.String     `tfsdk:"direction"`
	HostGroups   []hostGroupModel `tfsdk:"host_groups"`
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
		Description: "Lists the host groups in the organization, so a `host_group_id` can be looked " +
			"up by name instead of hardcoded. All filters are optional and are combined; omitting " +
			"them returns every group, in the dashboard's display order.",
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				Description: "Case-insensitive substring match on the group name (the API's `q` filter). " +
					"A `%` in the term matches a literal percent sign.",
				Optional: true,
			},
			"updated_since": schema.StringAttribute{
				Description: "Only return groups updated at or after this ISO8601 timestamp. Inclusive, " +
					"and it surfaces creates and updates only — a deleted group leaves no tombstone.",
				Optional: true,
			},
			"order": schema.StringAttribute{
				Description: "Column to sort by. Defaults to `position`, the display order rather than " +
					"`created_at`; sorting by `position` breaks ties on `name`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("position", "name", "created_at", "updated_at"),
				},
			},
			"direction": schema.StringAttribute{
				Description: `Sort direction: "asc" or "desc". Defaults to "asc", the top of the dashboard list.`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"host_groups": schema.ListNestedAttribute{
				Description: "Matching host groups.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Host group ID, the value a `host_group_id` argument expects.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Host group name. Unique within the organization, case-insensitively.",
							Computed:    true,
						},
						"position": schema.Int64Attribute{
							Description: "1-based display order. Groups never explicitly ordered keep the default 0.",
							Computed:    true,
						},
						"created_at": schema.StringAttribute{
							Description: "Creation timestamp.",
							Computed:    true,
						},
						"updated_at": schema.StringAttribute{
							Description: "Last update timestamp. A reposition does not move it.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *hostGroupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *hostGroupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state hostGroupsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := client.ListHostGroupsOptions{
		Query:        state.Query.ValueString(),
		UpdatedSince: state.UpdatedSince.ValueString(),
		Order:        state.Order.ValueString(),
		Direction:    state.Direction.ValueString(),
	}

	groups, err := d.client.ListHostGroups(ctx, &opts)
	if err != nil {
		resp.Diagnostics.AddError("Error listing host groups", err.Error())
		return
	}

	// Non-nil even when nothing matches: a nil slice serialises as a null list, and
	// length()/for_each/toset over a null fail. Zero matches is the normal case for
	// a filtered read, so it has to come back as [].
	state.HostGroups = make([]hostGroupModel, 0, len(groups))
	for _, g := range groups {
		state.HostGroups = append(state.HostGroups, hostGroupModel{
			ID:        types.Int64Value(g.ID),
			Name:      types.StringValue(g.Name),
			Position:  types.Int64Value(g.Position),
			CreatedAt: types.StringValue(g.CreatedAt),
			UpdatedAt: types.StringValue(g.UpdatedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
