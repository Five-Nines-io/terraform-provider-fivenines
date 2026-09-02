package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &organizationMembersDataSource{}

type organizationMembersDataSource struct {
	client *client.Client
}

type organizationMembersModel struct {
	Members []organizationMemberModel `tfsdk:"members"`
}

type organizationMemberModel struct {
	ID               types.Int64  `tfsdk:"id"`
	UserID           types.Int64  `tfsdk:"user_id"`
	Email            types.String `tfsdk:"email"`
	Role             types.String `tfsdk:"role"`
	TwoFactorEnabled types.Bool   `tfsdk:"two_factor_enabled"`
	JoinedAt         types.String `tfsdk:"joined_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

func NewOrganizationMembersDataSource() datasource.DataSource {
	return &organizationMembersDataSource{}
}

func (d *organizationMembersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_members"
}

func (d *organizationMembersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the organization's members, owners first then by join date. Each row carries the " +
			"person's two-factor enrollment state, which is where \"who has not enrolled\" is answered — the " +
			"`fivenines_organization_security` data source only has the org-wide totals.\n\n" +
			"Requires the `admin` or `owner` role on a plan that includes team features.",
		Attributes: map[string]schema.Attribute{
			"members": schema.ListNestedAttribute{
				Description: "List of members.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Membership ID — the id `fivenines_organization_member` addresses.",
							Computed:    true,
						},
						"user_id": schema.Int64Attribute{
							Description: "User ID — the durable identity to reconcile against your own directory.",
							Computed:    true,
						},
						"email": schema.StringAttribute{
							Description: "Email address.",
							Computed:    true,
						},
						"role": schema.StringAttribute{
							Description: "`owner`, `admin`, or `member`.",
							Computed:    true,
						},
						"two_factor_enabled": schema.BoolAttribute{
							Description: "Whether this person has enrolled a second factor.",
							Computed:    true,
						},
						"joined_at": schema.StringAttribute{
							Description: "When the member joined.",
							Computed:    true,
						},
						"updated_at": schema.StringAttribute{
							Description: "Last update timestamp of the membership.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *organizationMembersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *organizationMembersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	members, err := d.client.ListOrganizationMembers(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing organization members", err.Error())
		return
	}

	// Sized, never nil: an empty roster has to read back as an empty list, since
	// a `for` expression over a null one fails at plan time.
	var state organizationMembersModel
	state.Members = make([]organizationMemberModel, len(members))
	for i, m := range members {
		state.Members[i] = organizationMemberModel{
			ID:               types.Int64Value(m.ID),
			UserID:           types.Int64Value(m.UserID),
			Email:            types.StringValue(m.Email),
			Role:             types.StringValue(m.Role),
			TwoFactorEnabled: types.BoolValue(m.TwoFactorEnabled),
			JoinedAt:         types.StringValue(m.JoinedAt),
			UpdatedAt:        types.StringValue(m.UpdatedAt),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
