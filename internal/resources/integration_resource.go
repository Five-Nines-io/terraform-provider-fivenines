package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                   = &integrationResource{}
	_ resource.ResourceWithValidateConfig = &integrationResource{}
)

type integrationResource struct {
	client *client.Client
}

type integrationModel struct {
	ID            types.Int64  `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	Name          types.String `tfsdk:"name"`
	URL           types.String `tfsdk:"url"`
	Secret        types.String `tfsdk:"secret"`
	RoutingKey    types.String `tfsdk:"routing_key"`
	UserKey       types.String `tfsdk:"user_key"`
	AppToken      types.String `tfsdk:"app_token"`
	VerifyWebhook types.Bool   `tfsdk:"verify_webhook"`

	Enabled  types.Bool `tfsdk:"enabled"`
	Verified types.Bool `tfsdk:"verified"`

	WebhookSigningSecret              types.String `tfsdk:"webhook_signing_secret"`
	WebhookVerificationHeader         types.String `tfsdk:"webhook_verification_header"`
	WebhookVerificationToken          types.String `tfsdk:"webhook_verification_token"`
	WebhookVerificationTokenExpiresAt types.String `tfsdk:"webhook_verification_token_expires_at"`

	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

// integrationRules records what each API-creatable type accepts. `name` is
// listed separately because every type takes it — it is only *required* for the
// two that have no other human-readable identifier to fall back on.
var integrationRules = map[string]struct {
	requireName bool
	required    []string
	allowed     []string
}{
	"webhook":   {required: []string{"url"}, allowed: []string{"url", "secret", "verify_webhook"}},
	"pagerduty": {requireName: true, required: []string{"routing_key"}, allowed: []string{"routing_key"}},
	"pushover":  {requireName: true, required: []string{"user_key", "app_token"}, allowed: []string{"user_key", "app_token"}},
}

// dashboardOnlyTypes are interactive OAuth or app-install flows. They have no
// headless equivalent, so the API answers 422 rather than half-creating a row.
var dashboardOnlyTypes = map[string]string{
	"slack":    "Slack",
	"discord":  "Discord",
	"teams":    "Microsoft Teams",
	"telegram": "Telegram",
}

func NewIntegrationResource() resource.Resource {
	return &integrationResource{}
}

func (r *integrationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration"
}

func (r *integrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines integration (notification channel). Create and delete only: the API has no update endpoint, so every argument forces replacement. Only the webhook, pagerduty and pushover types can be created over the API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"type": schema.StringAttribute{
				Description: "Channel to connect: webhook, pagerduty or pushover. The email, slack, discord, teams and telegram types cannot be created over the API — connect them in the dashboard and reference them with the fivenines_integrations data source.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name. Required for pagerduty and pushover; optional for webhook, which falls back to the URL.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"url": schema.StringAttribute{
				Description: "webhook only. HTTP(S) endpoint to deliver to. Must be publicly reachable — private and internal addresses are rejected — and HTTPS is required in production. Write-only: never returned by the API.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"secret": schema.StringAttribute{
				Description: "webhook only. HMAC-SHA256 key used to sign every delivery. One is generated for you if omitted; either way the effective key is exported as webhook_signing_secret. Write-only: never returned by the API.",
				Optional:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"routing_key": schema.StringAttribute{
				Description: "pagerduty only. Events API v2 integration key. Write-only: never returned by the API.",
				Optional:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_key": schema.StringAttribute{
				Description: "pushover only. Pushover user or group key. Write-only: never returned by the API.",
				Optional:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"app_token": schema.StringAttribute{
				Description: "pushover only. API token of your own Pushover application (https://pushover.net/apps/build). Write-only: never returned by the API.",
				Optional:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"verify_webhook": schema.BoolAttribute{
				Description: "webhook only. Verify endpoint ownership as part of the apply. FiveNines sends a GET to the URL and expects a 200 echoing webhook_verification_token in the X-Fivenines-Verification header; the apply fails if it does not. Your endpoint must already be serving that header when you apply. Defaults to false, which leaves the webhook unverified — workflow notification nodes refuse to deliver to it until it is verified here or from the dashboard.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the channel is enabled.",
				Computed:    true,
			},
			"verified": schema.BoolAttribute{
				Description: "Whether the channel is verified. Workflow notification nodes refuse to deliver to an unverified channel.",
				Computed:    true,
			},
			"webhook_signing_secret": schema.StringAttribute{
				Description: "webhook only. The HMAC-SHA256 key deliveries are signed with, whether you supplied it or the API generated it. Returned once at create and never readable again.",
				Computed:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"webhook_verification_header": schema.StringAttribute{
				Description: "webhook only. Header your endpoint must echo the verification token in, normally X-Fivenines-Verification.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"webhook_verification_token": schema.StringAttribute{
				Description: "webhook only. Token your endpoint must echo back to prove ownership. Returned once at create and never readable again.",
				Computed:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"webhook_verification_token_expires_at": schema.StringAttribute{
				Description: "webhook only. When the verification token expires, 24 hours after it was minted. Past it, mint a new one with POST /api/v1/integrations/{id}/regenerate_webhook_token.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp.",
				Computed:    true,
			},
		},
	}
}

func (r *integrationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *client.Client.")
		return
	}
	r.client = c
}

// ValidateConfig enforces the per-type argument rules at plan time. The API
// 422s cleanly on all of them, but only after an apply has started — and for
// pagerduty that apply has already sent a live test alert.
func (r *integrationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config integrationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown type resolves at apply time; there is nothing to check yet.
	if config.Type.IsNull() || config.Type.IsUnknown() {
		return
	}
	integrationType := config.Type.ValueString()

	rules, ok := integrationRules[integrationType]
	if !ok {
		resp.Diagnostics.AddAttributeError(path.Root("type"), "Unsupported integration type", unsupportedTypeDetail(integrationType))
		return
	}

	if rules.requireName && config.Name.IsNull() {
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Missing required argument",
			fmt.Sprintf("%q integrations require `name`.", integrationType),
		)
	}

	scoped := typeScopedAttrs(&config)
	for _, name := range rules.required {
		if scoped[name].IsNull() {
			resp.Diagnostics.AddAttributeError(
				path.Root(name),
				"Missing required argument",
				fmt.Sprintf("%q integrations require `%s`.", integrationType, name),
			)
		}
	}

	allowed := map[string]bool{}
	for _, name := range rules.allowed {
		allowed[name] = true
	}
	for name, value := range scoped {
		if allowed[name] || value.IsNull() {
			continue
		}
		resp.Diagnostics.AddAttributeError(
			path.Root(name),
			"Argument not valid for this integration type",
			fmt.Sprintf("`%s` does not apply to %q integrations. It is only used by %s.", name, integrationType, typesAccepting(name)),
		)
	}
}

func (r *integrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan integrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	integrationType := plan.Type.ValueString()
	input := client.CreateIntegrationInput{
		Type:       integrationType,
		Name:       configuredString(plan.Name),
		URL:        configuredString(plan.URL),
		Secret:     configuredString(plan.Secret),
		RoutingKey: configuredString(plan.RoutingKey),
		UserKey:    configuredString(plan.UserKey),
		AppToken:   configuredString(plan.AppToken),
	}

	tflog.Debug(ctx, "Creating integration", map[string]interface{}{"type": integrationType})

	result, err := r.client.CreateIntegration(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating integration", createErrorDetail(integrationType, err))
		return
	}
	if result.Integration == nil {
		resp.Diagnostics.AddError(
			"Integration was not created",
			fmt.Sprintf("The API accepted the request for a %q integration but returned no integration. This provider only supports types that are created in a single call.", integrationType),
		)
		return
	}

	mapIntegrationToState(result.Integration, &plan)
	mapWebhookVerificationToState(result.Webhook, &plan)

	if plan.VerifyWebhook.ValueBool() {
		tflog.Debug(ctx, "Verifying webhook integration", map[string]interface{}{"id": result.Integration.ID})

		verified, verifyErr := r.client.VerifyWebhookIntegration(ctx, result.Integration.ID)
		if verifyErr != nil {
			// The channel exists. Persist it so the failed apply leaves
			// something Terraform can destroy instead of an orphan.
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			resp.Diagnostics.AddError("Webhook created but verification failed", verifyErrorDetail(verifyErr))
			return
		}
		mapIntegrationToState(verified, &plan)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *integrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state integrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	integration, err := r.client.GetIntegration(ctx, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading integration", err.Error())
		return
	}

	// Only the fields the API serializes are refreshed. Everything the request
	// carried — url, secret, routing_key, user_key, app_token — plus the
	// one-shot webhook credentials stay as state recorded them, because no read
	// can ever return them.
	mapIntegrationToState(integration, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable: the API has no PATCH for integrations, so every
// argument is RequiresReplace. It exists to satisfy resource.Resource.
func (r *integrationResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Integrations cannot be updated in place",
		"The FiveNines API has no update endpoint for integrations, so every argument should force replacement. Reaching this point is a bug in the provider — please report it.",
	)
}

func (r *integrationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state integrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting integration", map[string]interface{}{"id": state.ID.ValueInt64()})

	err := r.client.DeleteIntegration(ctx, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting integration", err.Error())
	}
}

// typeScopedAttrs returns the arguments that belong to a single integration type.
func typeScopedAttrs(m *integrationModel) map[string]attr.Value {
	return map[string]attr.Value{
		"url":            m.URL,
		"secret":         m.Secret,
		"routing_key":    m.RoutingKey,
		"user_key":       m.UserKey,
		"app_token":      m.AppToken,
		"verify_webhook": m.VerifyWebhook,
	}
}

// typesAccepting names the integration types that take the given argument.
func typesAccepting(attrName string) string {
	var types []string
	for integrationType, rules := range integrationRules {
		for _, allowed := range rules.allowed {
			if allowed == attrName {
				types = append(types, fmt.Sprintf("%q", integrationType))
				break
			}
		}
	}
	sort.Strings(types)
	return strings.Join(types, ", ")
}

func unsupportedTypeDetail(integrationType string) string {
	if label, ok := dashboardOnlyTypes[integrationType]; ok {
		return fmt.Sprintf(
			"%s integrations are an interactive OAuth / app install and cannot be created over the API.\n\n"+
				"Connect it from Settings > Integrations in the dashboard, then reference it with the fivenines_integrations data source.",
			label,
		)
	}
	if integrationType == "email" {
		return "Email integrations are created in two steps: the API mails a 6-digit code that has to be exchanged within 15 minutes, which an apply cannot do.\n\n" +
			"Add the address from Settings > Integrations in the dashboard, or drive POST /api/v1/integrations and POST /api/v1/integrations/{verification_id}/verify yourself, then reference the result with the fivenines_integrations data source."
	}
	return fmt.Sprintf("%q is not a known integration type. Terraform can create \"webhook\", \"pagerduty\" and \"pushover\" integrations.", integrationType)
}

func createErrorDetail(integrationType string, err error) string {
	apiErr, ok := err.(*client.APIError)
	if !ok {
		return err.Error()
	}
	switch apiErr.StatusCode {
	case 403:
		return fmt.Sprintf("%s\n\nThe API key needs write scope, and the organization's plan has to include %s alerts.", apiErr.Error(), integrationType)
	case 502:
		return fmt.Sprintf("%s\n\nFiveNines could not reach the provider to validate the credentials. This is not a verdict on them — retry the apply.", apiErr.Error())
	}
	return apiErr.Error()
}

func verifyErrorDetail(err error) string {
	detail := err.Error()
	if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 422 {
		detail = fmt.Sprintf("%s\n\nFiveNines sent a GET to the webhook URL and did not get a 200 echoing the verification token in the X-Fivenines-Verification header.", apiErr.Error())
	}
	return detail + "\n\nThe webhook was created and is recorded in state as unverified, so the next apply will replace it. " +
		"Either bring the endpoint up before applying again, or drop verify_webhook and verify from the dashboard."
}

// configuredString returns the value of an argument the practitioner set, or ""
// for one they left out, which tells the API to apply its own default. A plan
// reaching Create has every reference resolved, so the only unknown left is an
// Optional+Computed argument with no config — `name` — and "" is right for it
// too: the API falls back to the channel's own identifier.
func configuredString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

func mapIntegrationToState(i *client.Integration, state *integrationModel) {
	state.ID = types.Int64Value(i.ID)
	state.Name = types.StringValue(i.Name)
	state.Enabled = types.BoolValue(i.Enabled)
	state.Verified = types.BoolValue(i.Verified)
	state.CreatedAt = types.StringValue(i.CreatedAt)
	state.UpdatedAt = types.StringValue(i.UpdatedAt)
}

// mapWebhookVerificationToState records the credentials the create call returned
// once. For every non-webhook type the API returns nothing and the attributes
// resolve to null, which a Computed attribute needs in order to stop being
// unknown after the apply.
func mapWebhookVerificationToState(w *client.WebhookVerification, state *integrationModel) {
	if w == nil {
		state.WebhookSigningSecret = types.StringNull()
		state.WebhookVerificationHeader = types.StringNull()
		state.WebhookVerificationToken = types.StringNull()
		state.WebhookVerificationTokenExpiresAt = types.StringNull()
		return
	}

	// The signing key is what a receiver needs to check X-Webhook-Signature,
	// whether the API generated it or echoed back the configured `secret`.
	signingSecret := w.Secret
	if signingSecret == "" {
		signingSecret = configuredString(state.Secret)
	}

	state.WebhookSigningSecret = nullIfEmpty(signingSecret)
	state.WebhookVerificationHeader = nullIfEmpty(w.VerificationHeader)
	state.WebhookVerificationToken = nullIfEmpty(w.VerificationToken)
	state.WebhookVerificationTokenExpiresAt = nullIfEmpty(w.VerificationTokenExpiresAt)
}

func nullIfEmpty(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
