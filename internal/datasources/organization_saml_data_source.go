package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &organizationSAMLDataSource{}

type organizationSAMLDataSource struct {
	client *client.Client
}

type organizationSAMLModel struct {
	Configured              types.Bool   `tfsdk:"configured"`
	Enabled                 types.Bool   `tfsdk:"enabled"`
	EnforceSSO              types.Bool   `tfsdk:"enforce_sso"`
	AutoProvisionUsers      types.Bool   `tfsdk:"auto_provision_users"`
	AllowIdpInitiated       types.Bool   `tfsdk:"allow_idp_initiated"`
	IdpEntityID             types.String `tfsdk:"idp_entity_id"`
	IdpSSOURL               types.String `tfsdk:"idp_sso_url"`
	IdpSLOURL               types.String `tfsdk:"idp_slo_url"`
	IdpMetadataURL          types.String `tfsdk:"idp_metadata_url"`
	MetadataLastFetchedAt   types.String `tfsdk:"metadata_last_fetched_at"`
	IdpCertificatePresent   types.Bool   `tfsdk:"idp_certificate_present"`
	IdpCertificateExpiresAt types.String `tfsdk:"idp_certificate_expires_at"`
	NameIDFormat            types.String `tfsdk:"name_id_format"`
	DefaultUserRole         types.String `tfsdk:"default_user_role"`
	SessionDurationHours    types.Int64  `tfsdk:"session_duration_hours"`
	Domains                 types.List   `tfsdk:"domains"`
	UpdatedAt               types.String `tfsdk:"updated_at"`
}

func NewOrganizationSAMLDataSource() datasource.DataSource {
	return &organizationSAMLDataSource{}
}

func (d *organizationSAMLDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_saml"
}

func (d *organizationSAMLDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the organization's SAML SSO posture: whether SAML is enabled and enforced, which " +
			"domains it covers, and **when the IdP signing certificate expires** — a date that is surfaced " +
			"nowhere else, and whose passing locks every member out at once. Wire " +
			"`idp_certificate_expires_at` into an alert.\n\n" +
			"This is a data source rather than a resource because the configuration is deliberately **read-only " +
			"over the API** — the server refuses `PATCH` and `DELETE` on `/api/v1/organization/saml` with `403` " +
			"on every plan and scope, since rewriting `idp_sso_url` and the certificate repoints the " +
			"organization's identity provider outright. Change it in the dashboard, under **Organization > SAML " +
			"SSO**.\n\n" +
			"An organization that has never configured SAML answers with the same keys, all null or false. " +
			"Requires the `admin` or `owner` role on the Business plan or above.",
		Attributes: map[string]schema.Attribute{
			"configured": schema.BoolAttribute{
				Description: "Entity ID, SSO URL and certificate are all present. Distinguishes \"SAML is off\" from \"SAML is half set up and would fail on the first login\".",
				Computed:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether SAML SSO is enabled.",
				Computed:    true,
			},
			"enforce_sso": schema.BoolAttribute{
				Description: "When true, password login is disabled for this organization.",
				Computed:    true,
			},
			"auto_provision_users": schema.BoolAttribute{
				Description: "JIT account creation on first login through the IdP.",
				Computed:    true,
			},
			"allow_idp_initiated": schema.BoolAttribute{
				Description: "Whether IdP-initiated sign-on is allowed.",
				Computed:    true,
			},
			"idp_entity_id": schema.StringAttribute{
				Description: "IdP entity ID.",
				Computed:    true,
			},
			"idp_sso_url": schema.StringAttribute{
				Description: "IdP single sign-on URL.",
				Computed:    true,
			},
			"idp_slo_url": schema.StringAttribute{
				Description: "IdP single logout URL.",
				Computed:    true,
			},
			"idp_metadata_url": schema.StringAttribute{
				Description: "IdP metadata URL.",
				Computed:    true,
			},
			"metadata_last_fetched_at": schema.StringAttribute{
				Description: "When the IdP metadata was last fetched.",
				Computed:    true,
			},
			"idp_certificate_present": schema.BoolAttribute{
				Description: "Whether a signing certificate is stored. The certificate itself is not returned.",
				Computed:    true,
			},
			"idp_certificate_expires_at": schema.StringAttribute{
				Description: "When the IdP signing certificate lapses, locking every member out at once. Null when the stored value will not parse — never a guess.",
				Computed:    true,
			},
			"name_id_format": schema.StringAttribute{
				Description: "SAML NameID format.",
				Computed:    true,
			},
			"default_user_role": schema.StringAttribute{
				Description: "Role given to auto-provisioned users.",
				Computed:    true,
			},
			"session_duration_hours": schema.Int64Attribute{
				Description: "Session lifetime granted to SSO logins, in hours.",
				Computed:    true,
			},
			"domains": schema.ListAttribute{
				Description: "Email domains covered by this SAML configuration.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp.",
				Computed:    true,
			},
		},
	}
}

func (d *organizationSAMLDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *organizationSAMLDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	saml, err := d.client.GetOrganizationSAML(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization SAML configuration", err.Error())
		return
	}

	// Sized, never nil: an organization with no SAML domains has to read back as
	// an empty list, since a `for` expression over a null one fails at plan time.
	if saml.Domains == nil {
		saml.Domains = []string{}
	}
	domains, diags := types.ListValueFrom(ctx, types.StringType, saml.Domains)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := organizationSAMLModel{
		Configured:              types.BoolValue(saml.Configured),
		Enabled:                 types.BoolValue(saml.Enabled),
		EnforceSSO:              types.BoolValue(saml.EnforceSSO),
		AutoProvisionUsers:      types.BoolValue(saml.AutoProvisionUsers),
		AllowIdpInitiated:       types.BoolValue(saml.AllowIdpInitiated),
		IdpEntityID:             optionalString(saml.IdpEntityID),
		IdpSSOURL:               optionalString(saml.IdpSSOURL),
		IdpSLOURL:               optionalString(saml.IdpSLOURL),
		IdpMetadataURL:          optionalString(saml.IdpMetadataURL),
		MetadataLastFetchedAt:   optionalString(saml.MetadataLastFetchedAt),
		IdpCertificatePresent:   types.BoolValue(saml.IdpCertificatePresent),
		IdpCertificateExpiresAt: optionalString(saml.IdpCertificateExpiresAt),
		NameIDFormat:            optionalString(saml.NameIDFormat),
		DefaultUserRole:         optionalString(saml.DefaultUserRole),
		SessionDurationHours:    optionalInt64(saml.SessionDurationHours),
		Domains:                 domains,
		UpdatedAt:               optionalString(saml.UpdatedAt),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
