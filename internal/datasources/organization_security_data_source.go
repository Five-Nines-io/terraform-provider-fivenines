package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &organizationSecurityDataSource{}

type organizationSecurityDataSource struct {
	client *client.Client
}

type organizationSecurityModel struct {
	RequireTwoFactor        types.Bool   `tfsdk:"require_two_factor"`
	TwoFactorEnforcedAt     types.String `tfsdk:"two_factor_enforced_at"`
	MembersCount            types.Int64  `tfsdk:"members_count"`
	MembersWithTwoFactor    types.Int64  `tfsdk:"members_with_two_factor"`
	MembersPendingTwoFactor types.Int64  `tfsdk:"members_pending_two_factor"`
	SSOEnforced             types.Bool   `tfsdk:"sso_enforced"`
}

func NewOrganizationSecurityDataSource() datasource.DataSource {
	return &organizationSecurityDataSource{}
}

func (d *organizationSecurityDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_security"
}

func (d *organizationSecurityDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the organization's sign-in policy: whether two-factor authentication is required, and " +
			"how many members have actually enrolled.\n\n" +
			"This is a data source rather than a resource because the policy is deliberately **read-only over the " +
			"API** — the server refuses `PATCH /api/v1/organization/security` with `403` on every plan and scope, " +
			"so that a stolen API token cannot disarm the control that makes a stolen password survivable. Change " +
			"it in the dashboard, under **Organization > Security**.\n\n" +
			"Read the counters, not just the flag: enforcement only bites a member on their next request, so " +
			"`require_two_factor = true` with people still pending is a real state and not a compliant one.\n\n" +
			"Requires the `admin` or `owner` role. Not plan-gated.",
		Attributes: map[string]schema.Attribute{
			"require_two_factor": schema.BoolAttribute{
				Description: "Whether two-factor authentication is required for this organization.",
				Computed:    true,
			},
			"two_factor_enforced_at": schema.StringAttribute{
				Description: "When the policy was last switched on. Null while it is off.",
				Computed:    true,
			},
			"members_count": schema.Int64Attribute{
				Description: "Number of members.",
				Computed:    true,
			},
			"members_with_two_factor": schema.Int64Attribute{
				Description: "Members who have enrolled a second factor.",
				Computed:    true,
			},
			"members_pending_two_factor": schema.Int64Attribute{
				Description: "Members who have not. Per-person state is on the `fivenines_organization_members` data source.",
				Computed:    true,
			},
			"sso_enforced": schema.BoolAttribute{
				Description: "Whether password login is disabled in favour of SAML. An SSO-enforced organization delegates the second factor to its IdP, so a low enrollment count is not the same finding.",
				Computed:    true,
			},
		},
	}
}

func (d *organizationSecurityDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *organizationSecurityDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	security, err := d.client.GetOrganizationSecurity(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization security policy", err.Error())
		return
	}

	state := organizationSecurityModel{
		RequireTwoFactor:        types.BoolValue(security.RequireTwoFactor),
		TwoFactorEnforcedAt:     optionalString(security.TwoFactorEnforcedAt),
		MembersCount:            types.Int64Value(security.MembersCount),
		MembersWithTwoFactor:    types.Int64Value(security.MembersWithTwoFactor),
		MembersPendingTwoFactor: types.Int64Value(security.MembersPendingTwoFactor),
		SSOEnforced:             types.BoolValue(security.SSOEnforced),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
