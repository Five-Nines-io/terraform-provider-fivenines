package resources

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                   = &apiTokenResource{}
	_ resource.ResourceWithImportState    = &apiTokenResource{}
	_ resource.ResourceWithValidateConfig = &apiTokenResource{}
)

type apiTokenResource struct {
	client *client.Client
}

type apiTokenModel struct {
	ID              types.Int64  `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Scopes          types.Set    `tfsdk:"scopes"`
	ExpiresAt       types.String `tfsdk:"expires_at"`
	AllowSelfRevoke types.Bool   `tfsdk:"allow_self_revoke"`
	Token           types.String `tfsdk:"token"`
	TokenPrefix     types.String `tfsdk:"token_prefix"`
	Active          types.Bool   `tfsdk:"active"`
	LastUsedAt      types.String `tfsdk:"last_used_at"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func NewAPITokenResource() resource.Resource {
	return &apiTokenResource{}
}

func (r *apiTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_token"
}

// Schema.
//
// Every Computed attribute carries UseStateForUnknown, which is unusually
// blanket coverage and follows from what this resource can do: the only change
// it applies in place is allow_self_revoke, which lives in state and touches
// nothing server-side, so the values the API owns are still the ones the last
// refresh wrote. Without it the framework marks each of them unknown as soon as
// anything in the proposed plan differs, and toggling that local flag reads as
// an update with half the schema going "known after apply". A replacement is
// unaffected either way: Terraform re-plans the create half against a null prior
// state, which UseStateForUnknown returns on.
func (r *apiTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines API token — the bearer credential this provider itself authenticates with. " +
			"Minting one here is what turns key rotation into a scheduled job instead of a browser errand.\n\n" +
			"Three things follow from how the API stores tokens, and all three shape how you use this resource.\n\n" +
			"**The value is returned once.** Only a SHA-256 digest is kept server-side, so `token` is readable " +
			"in state and outputs but can never be re-fetched. A token imported into Terraform has no `token` value, " +
			"because there is nowhere left to read it from.\n\n" +
			"**Nothing is editable.** The API exposes create, list and revoke. Changing `name`, `scopes` or " +
			"`expires_at` destroys the token and mints a new one with a new value — use `create_before_destroy` so " +
			"the replacement exists before the old one dies.\n\n" +
			"**A token can revoke itself.** Destroying the token the provider is currently authenticated with " +
			"locks Terraform out of the API, so this resource refuses to do it unless `allow_self_revoke` is set.\n\n" +
			"Managing tokens requires a `write`-scoped token: a read-scoped one cannot mint anything, and no token " +
			"can grant a scope it does not hold itself.\n\n" +
			"**One user owns these.** The API lists only the calling user's own tokens, and there is no endpoint " +
			"that reads one by id, so a token minted under one key is invisible to a provider configured with " +
			"another user's key — indistinguishable, from here, from a token that was deleted. Point Terraform at " +
			"one durable service-account key, and be aware that swapping it for a different user's key makes the " +
			"next plan propose recreating every token in the configuration while the originals stay live and " +
			"unmanaged.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "How you will recognise this token in the list. Changing it replaces the token, which mints a new value.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"scopes": schema.SetAttribute{
				Description: "Permissions carried by the token: `read` and/or `write`. Defaults to `[\"read\"]`. " +
					"`read` is the floor every token carries and is folded in automatically, so `[\"write\"]` and " +
					"`[\"read\", \"write\"]` describe the same token. Changing the permissions replaces the token — " +
					"and so does removing the attribute, which applies the `[\"read\"]` default rather than keeping " +
					"what the token has. There is no edit: narrowing a token means minting a new one.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Default: setdefault.StaticValue(types.SetValueMust(
					types.StringType,
					[]attr.Value{types.StringValue(scopeRead)},
				)),
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf(scopeRead, scopeWrite)),
				},
				PlanModifiers: []planmodifier.Set{
					retainEquivalentScopesModifier{},
					setplanmodifier.RequiresReplace(),
				},
			},
			"expires_at": schema.StringAttribute{
				Description: "ISO 8601 expiry, which must be in the future (`2026-12-01` or `2026-12-01T00:00:00Z`). " +
					"Omit for a token that never expires. Changing this replaces the token. An expired token stays in " +
					"state with `active = false` rather than being recreated automatically — pair it with a " +
					"`time_rotating` resource if you want expiry to drive rotation.",
				Optional: true,
				PlanModifiers: []planmodifier.String{
					retainEquivalentExpiryModifier{},
					stringplanmodifier.RequiresReplace(),
				},
			},
			"allow_self_revoke": schema.BoolAttribute{
				Description: "Permit destroying this token even when it is the one the provider is authenticated with. " +
					"Defaults to `false`, which fails the destroy with an explanation instead of locking Terraform out " +
					"of the API. Set it to `true` only when cutting yourself off is the point, such as revoking a " +
					"credential that leaked. The destroy reads this from state rather than from the pending plan, so " +
					"turning it on takes one apply of its own before the apply that destroys the token. Terraform " +
					"orders nothing else around that revoke either: anything still in flight against the API in " +
					"the same apply starts failing with 401 the moment it lands.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"token": schema.StringAttribute{
				Description: "The bearer value, in `fn_...` form. Returned by the create call and never again: it is " +
					"stored as a digest server-side, so this is the only copy. Null for an imported token.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					retainPriorModifier{},
				},
			},
			"token_prefix": schema.StringAttribute{
				Description: "First 8 characters of the value (`fn_` plus 5 hex), enough to match a token against a secret you hold.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"active": schema.BoolAttribute{
				Description: "Whether the token can still authenticate. False once it passes `expires_at`. " +
					"A revoked token is dropped from state instead, since revocation is what destroying this resource does.",
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"last_used_at": schema.StringAttribute{
				Description: "When the token last authenticated a request.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					retainPriorModifier{},
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
				Description: "Last update timestamp. Moves on create and on revoke — not when the token is used.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *apiTokenResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *apiTokenResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config apiTokenModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateExpiresAt(config.ExpiresAt)...)
}

// validateExpiresAt refuses a timestamp this provider cannot parse.
//
// The API would accept some of these anyway, but a value the provider cannot
// read is a value it cannot compare against the one the API echoes back — and
// that shows up as a plan that proposes replacing a live credential on every
// run. Failing at plan time with the accepted formats is the kinder answer.
func validateExpiresAt(expiresAt types.String) diag.Diagnostics {
	var diags diag.Diagnostics

	if expiresAt.IsNull() || expiresAt.IsUnknown() {
		return diags
	}
	parsed, _, ok := parseISOTime(expiresAt.ValueString(), time.UTC)
	if ok && parsed.Nanosecond() != 0 {
		// The serializer renders expires_at with `iso8601` and no fractional
		// digits, so a sub-second value comes back truncated and the apply dies
		// on "inconsistent result after apply". A zero fraction is fine: it
		// names the same instant the API will echo.
		diags.AddAttributeError(
			path.Root("expires_at"),
			"Invalid expires_at",
			fmt.Sprintf(
				"%q carries sub-second precision, which the API drops when it stores and re-renders the "+
					"expiry. Round it to the second.",
				expiresAt.ValueString(),
			),
		)
		return diags
	}
	if !ok {
		diags.AddAttributeError(
			path.Root("expires_at"),
			"Invalid expires_at",
			fmt.Sprintf(
				"%q is not an ISO 8601 date or datetime. Write it as 2026-12-01, 2026-12-01T00:00:00Z "+
					"or 2026-12-01T00:00:00+01:00, or omit the attribute for a token that never expires.",
				expiresAt.ValueString(),
			),
		)
	}
	return diags
}

// Create.
//
// No expires_at re-check here, deliberately. ValidateConfig defers on an unknown
// value — an expiry interpolated from another resource is exactly that — which
// looks like a hole a credential could be minted through. It is not: Terraform
// calls ValidateResourceConfig again during the apply walk, with the value
// resolved, before ApplyResourceChange. Measured, not assumed:
// TestAPITokenPlan_UnknownSubSecondExpiryFailsBeforeMinting keeps passing with
// a re-check in Create deleted, and no token is minted either way.
func (r *apiTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan apiTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Sent as configured. The read floor is the server's rule, not the
	// provider's — ["write"] and ["read", "write"] mint the same token — so
	// folding it in here would be a transform with no effect on the wire.
	// normalizeScopes still owns the comparison side, where the two renderings
	// have to be recognised as one set.
	var scopes []string
	resp.Diagnostics.Append(plan.Scopes.ElementsAs(ctx, &scopes, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateAPITokenInput{
		Name:      plan.Name.ValueString(),
		Scopes:    scopes,
		ExpiresAt: stringPtr(plan.ExpiresAt),
	}

	tflog.Debug(ctx, "Creating API token", map[string]interface{}{"name": input.Name, "scopes": input.Scopes})

	token, err := r.client.CreateAPIToken(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating API token", err.Error())
		return
	}

	resp.Diagnostics.Append(mapAPITokenToState(ctx, token, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *apiTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state apiTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	token, err := r.client.GetAPIToken(ctx, state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading API token", err.Error())
		return
	}

	// A revoked token is gone as far as Terraform is concerned. The row survives
	// server-side for the audit trail, but it authenticates nothing and cannot be
	// un-revoked, so dropping it here is what makes the next apply mint a
	// replacement — the same answer as if the row had been deleted outright.
	//
	// Expiry is deliberately not treated the same way: a token past a hardcoded
	// expires_at would be recreated with that same past date, which the API
	// refuses, so the plan would fail on every run. Those stay in state with
	// active = false.
	if token.RevokedAt != nil {
		tflog.Debug(ctx, "API token has been revoked, removing from state", map[string]interface{}{
			"id":         token.ID,
			"revoked_at": *token.RevokedAt,
		})
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(mapAPITokenToState(ctx, token, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only ever handles allow_self_revoke, which lives in state and nowhere
// else. Every attribute the API stores is immutable — there is no PATCH for
// api_tokens — so each one carries RequiresReplace and cannot reach here.
func (r *apiTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state apiTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Name.Equal(state.Name) || !plan.Scopes.Equal(state.Scopes) || !plan.ExpiresAt.Equal(state.ExpiresAt) {
		resp.Diagnostics.AddError(
			"API tokens cannot be updated in place",
			"The FiveNines API can create and revoke tokens but not edit them, so this change has to replace the "+
				"token. Reaching this error means a plan modifier is missing — please report it.",
		)
		return
	}

	// Prior state carried forward with the one local attribute replaced, rather
	// than the plan wholesale: nothing was asked of the API, so every value it
	// owns is still the one already in state.
	state.AllowSelfRevoke = plan.AllowSelfRevoke
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *apiTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state apiTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !state.AllowSelfRevoke.ValueBool() {
		self, identified := r.isProviderCredential(state)
		switch {
		case !identified:
			// Fails CLOSED. The guard exists to stop Terraform cutting itself
			// off, and "I cannot tell whether this is the key in use" is not an
			// answer to build a revocation on — allow_self_revoke is the way
			// past it, the same as for a token known to be its own.
			resp.Diagnostics.AddError(
				"Cannot tell whether this is the token the provider is authenticated with",
				fmt.Sprintf(
					"API token %q has neither its value nor a token_prefix in state, so the provider cannot "+
						"check it against the credential it is using, and revoking the wrong one locks "+
						"Terraform out of the API.\n\n"+
						"Run `terraform refresh` to repopulate it. If the token is genuinely gone, remove it "+
						"from state with `terraform state rm`. To revoke without the check, set "+
						"allow_self_revoke = true.",
					state.Name.ValueString(),
				),
			)
			return
		case self:
			resp.Diagnostics.AddError(
				"Refusing to revoke the token this provider is authenticated with",
				fmt.Sprintf(
					"API token %q (prefix %s) is the credential this provider is using. Revoking it would lock "+
						"Terraform out of the FiveNines API, with the dashboard as the only way back in.\n\n"+
						"Rotate in this order instead: create the replacement token, point the provider at it "+
						"(FIVENINES_API_KEY or the api_key argument), and only then destroy this one.\n\n"+
						"If cutting yourself off is the point — the credential leaked, say — set "+
						"allow_self_revoke = true on this resource and apply again.",
					state.Name.ValueString(), state.TokenPrefix.ValueString(),
				),
			)
			return
		}
	}

	tflog.Debug(ctx, "Revoking API token", map[string]interface{}{"id": state.ID.ValueInt64()})

	if err := r.client.RevokeAPIToken(ctx, state.ID.ValueInt64()); err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error revoking API token", err.Error())
	}
}

// ImportState brings an existing token under management. Everything but the
// value comes back: the plaintext was destroyed at creation, so `token` stays
// null and the only way to hold a usable secret is to mint a new one.
func (r *apiTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse %q as int64: %s", req.ID, err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

const (
	scopeRead  = "read"
	scopeWrite = "write"
)

// retainPriorModifier keeps the value already in state, a null one included.
//
// stringplanmodifier.UseStateForUnknown declines to act when the prior value is
// null, which leaves a never-used token's `last_used_at` — and an imported
// token's absent `token` — flipping to "known after apply" on any in-place
// update. The framework marks every Computed attribute unknown as soon as
// anything in the proposed plan differs, so without this a rewrite of `scopes`
// that changes nothing at all plans as "1 to change".
//
// That is not cosmetic, and it has been measured: a /ship review argued this
// type was a redundant copy of UseStateForUnknown, and swapping the two turns
// TestAPITokenPlan_EquivalentScopeRewriteIsNoOp red — PlanOnly asserts an EMPTY
// plan, and a null attribute going unknown is not empty. Keep both, and keep
// the split: stock modifier where the prior value cannot be null, this one
// where it can.
//
// A replacement is unaffected: Terraform re-plans the create half against a null
// prior state, which the first guard here returns on, exactly as
// UseStateForUnknown does.
type retainPriorModifier struct{}

func (m retainPriorModifier) Description(_ context.Context) string {
	return "Retains the value already in state, which cannot change without the token being replaced."
}

func (m retainPriorModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m retainPriorModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Nothing to retain when the resource is being created, and nothing to plan
	// when it is being destroyed.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !req.PlanValue.IsUnknown() {
		return
	}
	resp.PlanValue = req.StateValue
}

// retainEquivalentExpiryModifier keeps the expiry already in state when the
// configuration names the same instant in a different rendering.
//
// The counterpart of retainEquivalentScopesModifier, for the other attribute the
// server re-renders. Rewriting "2026-12-01" to "2026-12-01T00:00:00Z" — a
// normalisation, or a switch from a literal to timeadd() — changes no expiry at
// all, but expires_at forces replacement, so without this the plan revokes a
// live credential to mint an identical one.
type retainEquivalentExpiryModifier struct{}

func (m retainEquivalentExpiryModifier) Description(_ context.Context) string {
	return "Retains the expiry already in state when the configured one names the same instant."
}

func (m retainEquivalentExpiryModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m retainEquivalentExpiryModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !isKnown(req.StateValue) || !isKnown(req.PlanValue) {
		return
	}
	if sameInstant(req.StateValue.ValueString(), req.PlanValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}

// sameInstant reports whether two renderings name the same moment. Anything
// this provider cannot parse is not equivalent to anything.
func sameInstant(a, b string) bool {
	ta, _, okA := parseISOTime(a, time.UTC)
	tb, _, okB := parseISOTime(b, time.UTC)
	return okA && okB && ta.Equal(tb)
}

// retainEquivalentScopesModifier keeps the scope set already in state when the
// config asks for the same permissions in a different shape.
//
// The API folds the implicit "read" floor into every token, so a config of
// ["write"] is stored and returned as ["read", "write"]. Left alone, the
// refreshed state and the config disagree forever — and since scopes forces
// replacement, every plan would propose destroying a live credential to mint an
// identical one.
//
// It plans the PRIOR value rather than a normalized one deliberately: Terraform
// lets a provider depart from a non-null config value only by retaining what is
// already in state ("the provider wishes to retain the prior value rather than
// change to a new functionally equivalent value"), never by planning a third
// rendering of its own.
type retainEquivalentScopesModifier struct{}

func (m retainEquivalentScopesModifier) Description(_ context.Context) string {
	return `Retains the scopes already in state when the configured set describes the same permissions, ` +
		`which it does whenever it differs only by the implicit "read" scope.`
}

func (m retainEquivalentScopesModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m retainEquivalentScopesModifier) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	// No prior value to retain (create), or nothing planned yet (destroy).
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}

	var planned, prior []string
	resp.Diagnostics.Append(req.PlanValue.ElementsAs(ctx, &planned, false)...)
	resp.Diagnostics.Append(req.StateValue.ElementsAs(ctx, &prior, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if scopesEquivalent(planned, prior) {
		resp.PlanValue = req.StateValue
	}
}

// scopesEquivalent reports whether two scope sets describe the same token once
// the read floor and the API's own ordering are accounted for.
func scopesEquivalent(a, b []string) bool {
	return slices.Equal(normalizeScopes(a), normalizeScopes(b))
}

// normalizeScopes mirrors ApiToken.normalize_scopes server-side: trim, downcase,
// drop blanks, dedupe and fold in the "read" floor. Sorted on top of that, which
// the server does not do — it returns `["write", "read"]` for a write token —
// so that plan and state hold the same rendering of the same set.
func normalizeScopes(scopes []string) []string {
	normalized := []string{scopeRead}
	for _, scope := range scopes {
		if s := strings.ToLower(strings.TrimSpace(scope)); s != "" {
			normalized = append(normalized, s)
		}
	}
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

// isProviderCredential reports whether the token in state is the one this
// provider authenticates with, and whether it could tell at all.
//
// The value itself is the exact answer and Terraform holds it for any token it
// minted, so use it. token_prefix is the fallback for an imported token, where
// the value no longer exists anywhere: 8 characters is `fn_` plus 5 hex, about
// 20 bits, so it can in principle refuse an unrelated token's destroy — a
// false refusal a practitioner can override, rather than a lockout they cannot
// undo.
func (r *apiTokenResource) isProviderCredential(state apiTokenModel) (self, identified bool) {
	if isKnown(state.Token) && state.Token.ValueString() != "" {
		return state.Token.ValueString() == r.client.APIKey, true
	}
	prefix := state.TokenPrefix.ValueString()
	if prefix == "" {
		return false, false
	}
	return tokenMatchesPrefix(r.client.APIKey, prefix), true
}

// tokenMatchesPrefix reports whether apiKey is the token identified by prefix.
// token_prefix is `fn_` plus 5 hex characters, which is what the API publishes
// precisely so a caller can recognise a secret it holds without revealing one.
func tokenMatchesPrefix(apiKey, prefix string) bool {
	if apiKey == "" || prefix == "" {
		return false
	}
	return strings.HasPrefix(apiKey, prefix)
}

// preserveScopes keeps the practitioner's own rendering of a scope set when it
// describes the same token the API returned.
//
// The API answers ["write"] with ["read", "write"], having folded in the floor
// every token carries. Storing that rendering would read as a change to an
// attribute that forces replacement, so the plan after any create would offer to
// destroy the credential just minted.
func preserveScopes(ctx context.Context, configured types.Set, apiScopes []string) (types.Set, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !configured.IsNull() && !configured.IsUnknown() {
		var current []string
		diags.Append(configured.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return configured, diags
		}
		if scopesEquivalent(current, apiScopes) {
			return configured, diags
		}
	}

	return types.SetValueFrom(ctx, types.StringType, normalizeScopes(apiScopes))
}

func mapAPITokenToState(ctx context.Context, t *client.APIToken, state *apiTokenModel) diag.Diagnostics {
	var diags diag.Diagnostics

	state.ID = types.Int64Value(t.ID)
	state.Name = types.StringValue(t.Name)

	scopes, scopeDiags := preserveScopes(ctx, state.Scopes, t.Scopes)
	diags.Append(scopeDiags...)
	if diags.HasError() {
		return diags
	}
	state.Scopes = scopes

	if t.ExpiresAt == nil {
		state.ExpiresAt = types.StringNull()
	} else {
		// "" for the timezone: the api_tokens API renders in UTC.
		state.ExpiresAt = preserveTimestamp(state.ExpiresAt, *t.ExpiresAt, "")
	}

	// The value exists in exactly one response, the create. Every other call
	// leaves whatever is already in state — null for an imported token, since
	// there is no longer anything to read.
	if t.Token != "" {
		state.Token = types.StringValue(t.Token)
	} else if state.Token.IsUnknown() {
		state.Token = types.StringNull()
	}

	state.TokenPrefix = types.StringValue(t.TokenPrefix)
	state.Active = types.BoolValue(t.Active)
	state.LastUsedAt = optionalString(t.LastUsedAt)
	state.CreatedAt = types.StringValue(t.CreatedAt)
	state.UpdatedAt = types.StringValue(t.UpdatedAt)

	if state.AllowSelfRevoke.IsNull() || state.AllowSelfRevoke.IsUnknown() {
		state.AllowSelfRevoke = types.BoolValue(false)
	}

	return diags
}
