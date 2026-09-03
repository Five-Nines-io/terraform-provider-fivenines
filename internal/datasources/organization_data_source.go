package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &organizationDataSource{}

type organizationDataSource struct {
	client *client.Client
}

type organizationModel struct {
	ID                      types.Int64  `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Slug                    types.String `tfsdk:"slug"`
	DisplayName             types.String `tfsdk:"display_name"`
	Plan                    types.String `tfsdk:"plan"`
	Trialing                types.Bool   `tfsdk:"trialing"`
	SeatsUsed               types.Int64  `tfsdk:"seats_used"`
	SeatsTotal              types.Int64  `tfsdk:"seats_total"`
	SeatsRemaining          types.Int64  `tfsdk:"seats_remaining"`
	MembersCount            types.Int64  `tfsdk:"members_count"`
	PendingInvitationsCount types.Int64  `tfsdk:"pending_invitations_count"`
	CreatedAt               types.String `tfsdk:"created_at"`
	UpdatedAt               types.String `tfsdk:"updated_at"`
}

func NewOrganizationDataSource() datasource.DataSource {
	return &organizationDataSource{}
}

func (d *organizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *organizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the organization the API key belongs to: identity, effective plan and seat accounting. " +
			"Readable by any role — unlike the member and invitation endpoints, which need `admin` or `owner`. " +
			"`seats_remaining` answers \"is there room to invite this person\" before an invite is attempted.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Organization ID.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the organization. Null for an organization that has never been named.",
				Computed:    true,
			},
			"slug": schema.StringAttribute{
				Description: "Stable URL identifier.",
				Computed:    true,
			},
			"display_name": schema.StringAttribute{
				Description: "`name` when set, otherwise `slug`.",
				Computed:    true,
			},
			"plan": schema.StringAttribute{
				Description: "Effective plan — a trialing organization reads as the plan it is trialing.",
				Computed:    true,
			},
			"trialing": schema.BoolAttribute{
				Description: "Whether the organization is in a trial.",
				Computed:    true,
			},
			"seats_used": schema.Int64Attribute{
				Description: "Members plus pending invitations — an unaccepted invite holds a seat.",
				Computed:    true,
			},
			"seats_total": schema.Int64Attribute{
				Description: "Total seats on the plan. Null when the plan is unmetered.",
				Computed:    true,
			},
			"seats_remaining": schema.Int64Attribute{
				Description: "Seats still available.",
				Computed:    true,
			},
			"members_count": schema.Int64Attribute{
				Description: "Number of members.",
				Computed:    true,
			},
			"pending_invitations_count": schema.Int64Attribute{
				Description: "Number of invitations sent but not yet accepted.",
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
	}
}

func (d *organizationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *organizationDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	org, _, err := d.client.GetOrganization(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization", err.Error())
		return
	}

	state := organizationModel{
		ID:                      types.Int64Value(org.ID),
		Name:                    optionalString(org.Name),
		Slug:                    types.StringValue(org.Slug),
		DisplayName:             types.StringValue(org.DisplayName),
		Plan:                    types.StringValue(org.Plan),
		Trialing:                types.BoolValue(org.Trialing),
		SeatsUsed:               types.Int64Value(org.SeatsUsed),
		SeatsTotal:              optionalInt64(org.SeatsTotal),
		SeatsRemaining:          types.Int64Value(org.SeatsRemaining),
		MembersCount:            types.Int64Value(org.MembersCount),
		PendingInvitationsCount: types.Int64Value(org.PendingInvitationsCount),
		CreatedAt:               types.StringValue(org.CreatedAt),
		UpdatedAt:               types.StringValue(org.UpdatedAt),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
