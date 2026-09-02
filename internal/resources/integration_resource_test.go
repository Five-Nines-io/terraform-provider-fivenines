package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func integrationSchema(t *testing.T) fwschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	(&integrationResource{}).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// validateIntegrationConfig runs ValidateConfig over a config in which the named
// attributes are set and every other one is null, the way an omitted argument
// reaches a provider.
func validateIntegrationConfig(t *testing.T, attrs map[string]tftypes.Value) []string {
	t.Helper()
	ctx := context.Background()
	s := integrationSchema(t)

	objType, ok := s.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected an object schema type, got %T", s.Type().TerraformType(ctx))
	}
	values := map[string]tftypes.Value{}
	for name, attrType := range objType.AttributeTypes {
		if v, set := attrs[name]; set {
			values[name] = v
			continue
		}
		values[name] = tftypes.NewValue(attrType, nil)
	}
	for name := range attrs {
		if _, known := objType.AttributeTypes[name]; !known {
			t.Fatalf("test sets unknown attribute %q", name)
		}
	}

	resp := &resource.ValidateConfigResponse{}
	(&integrationResource{}).ValidateConfig(ctx,
		resource.ValidateConfigRequest{
			Config: tfsdk.Config{Schema: s, Raw: tftypes.NewValue(objType, values)},
		},
		resp,
	)

	var messages []string
	for _, d := range resp.Diagnostics.Errors() {
		messages = append(messages, d.Summary()+": "+d.Detail())
	}
	return messages
}

func str(v string) tftypes.Value   { return tftypes.NewValue(tftypes.String, v) }
func boolean(v bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, v) }

func TestIntegrationValidateConfig_Valid(t *testing.T) {
	cases := map[string]map[string]tftypes.Value{
		"webhook with url only": {
			"type": str("webhook"),
			"url":  str("https://example.com/hook"),
		},
		"webhook with every webhook argument": {
			"type":           str("webhook"),
			"name":           str("Ops hook"),
			"url":            str("https://example.com/hook"),
			"secret":         str("whsec_mine"),
			"verify_webhook": boolean(true),
		},
		"pagerduty": {
			"type":        str("pagerduty"),
			"name":        str("Ops"),
			"routing_key": str("R0UT1NGK3Y"),
		},
		"pushover": {
			"type":      str("pushover"),
			"name":      str("On-call"),
			"user_key":  str("uk_1"),
			"app_token": str("at_1"),
		},
		"unknown type defers to apply": {
			"type": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		},
	}

	for name, attrs := range cases {
		t.Run(name, func(t *testing.T) {
			if errs := validateIntegrationConfig(t, attrs); len(errs) > 0 {
				t.Errorf("expected no errors, got %v", errs)
			}
		})
	}
}

func TestIntegrationValidateConfig_MissingRequired(t *testing.T) {
	cases := map[string]struct {
		attrs map[string]tftypes.Value
		want  string
	}{
		"webhook without url": {
			attrs: map[string]tftypes.Value{"type": str("webhook")},
			want:  `"webhook" integrations require "url"`,
		},
		"pagerduty without name": {
			attrs: map[string]tftypes.Value{"type": str("pagerduty"), "routing_key": str("k")},
			want:  `"pagerduty" integrations require "name"`,
		},
		"pagerduty without routing_key": {
			attrs: map[string]tftypes.Value{"type": str("pagerduty"), "name": str("Ops")},
			want:  `"pagerduty" integrations require "routing_key"`,
		},
		"pushover without app_token": {
			attrs: map[string]tftypes.Value{"type": str("pushover"), "name": str("On-call"), "user_key": str("uk_1")},
			want:  `"pushover" integrations require "app_token"`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			errs := validateIntegrationConfig(t, tc.attrs)
			if !containsSubstring(errs, tc.want) {
				t.Errorf("expected an error containing %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestIntegrationValidateConfig_ArgumentFromAnotherType(t *testing.T) {
	cases := map[string]struct {
		attrs map[string]tftypes.Value
		want  string
	}{
		"routing_key on a webhook": {
			attrs: map[string]tftypes.Value{
				"type": str("webhook"), "url": str("https://example.com/h"), "routing_key": str("k"),
			},
			want: `"webhook" integrations do not use "routing_key"`,
		},
		"url on pagerduty": {
			attrs: map[string]tftypes.Value{
				"type": str("pagerduty"), "name": str("Ops"), "routing_key": str("k"), "url": str("https://example.com/h"),
			},
			want: `"pagerduty" integrations do not use "url"`,
		},
		"verify_webhook on pushover": {
			attrs: map[string]tftypes.Value{
				"type": str("pushover"), "name": str("On-call"), "user_key": str("uk"), "app_token": str("at"),
				"verify_webhook": boolean(true),
			},
			want: `"pushover" integrations do not use "verify_webhook"`,
		},
		"app_token on a webhook": {
			attrs: map[string]tftypes.Value{
				"type": str("webhook"), "url": str("https://example.com/h"), "app_token": str("at"),
			},
			want: `"webhook" integrations do not use "app_token"`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			errs := validateIntegrationConfig(t, tc.attrs)
			if !containsSubstring(errs, tc.want) {
				t.Errorf("expected an error containing %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestIntegrationValidateConfig_UncreatableTypes(t *testing.T) {
	cases := map[string]string{
		"slack":    "Settings > Integrations",
		"discord":  "Settings > Integrations",
		"teams":    "Settings > Integrations",
		"telegram": "Settings > Integrations",
		"email":    "6-digit code",
		"pigeon":   "not a known integration type",
	}

	for integrationType, want := range cases {
		t.Run(integrationType, func(t *testing.T) {
			errs := validateIntegrationConfig(t, map[string]tftypes.Value{"type": str(integrationType)})
			if len(errs) != 1 {
				t.Fatalf("expected exactly one error, got %v", errs)
			}
			if !strings.Contains(errs[0], want) {
				t.Errorf("expected error to mention %q, got %q", want, errs[0])
			}
		})
	}
}

func TestMapIntegrationToState(t *testing.T) {
	state := &integrationModel{
		Type:   types.StringValue("webhook"),
		URL:    types.StringValue("https://example.com/hook"),
		Secret: types.StringValue("whsec_mine"),
	}

	mapIntegrationToState(&client.Integration{
		ID:        7,
		Type:      "WebhookIntegration",
		Name:      "Ops hook",
		Enabled:   true,
		Verified:  true,
		CreatedAt: "2026-09-01T00:00:00Z",
		UpdatedAt: "2026-09-01T01:00:00Z",
	}, state)

	if state.ID.ValueInt64() != 7 {
		t.Errorf("expected id 7, got %d", state.ID.ValueInt64())
	}
	if state.Name.ValueString() != "Ops hook" {
		t.Errorf("expected name from the API, got %q", state.Name.ValueString())
	}
	if !state.Verified.ValueBool() {
		t.Error("expected verified true")
	}
	if state.UpdatedAt.ValueString() != "2026-09-01T01:00:00Z" {
		t.Errorf("expected updated_at, got %q", state.UpdatedAt.ValueString())
	}
	// The class name in the response must not overwrite the configured short key.
	if state.Type.ValueString() != "webhook" {
		t.Errorf("expected type to stay webhook, got %q", state.Type.ValueString())
	}
	// Nothing the request carried is ever readable, so a refresh must leave it alone.
	if state.URL.ValueString() != "https://example.com/hook" {
		t.Errorf("expected url to be preserved, got %q", state.URL.ValueString())
	}
	if state.Secret.ValueString() != "whsec_mine" {
		t.Errorf("expected secret to be preserved, got %q", state.Secret.ValueString())
	}
}

func TestMapWebhookVerificationToState(t *testing.T) {
	state := &integrationModel{}
	mapWebhookVerificationToState(&client.WebhookVerification{
		VerificationHeader:         "X-Fivenines-Verification",
		VerificationToken:          "tok_abc",
		VerificationTokenExpiresAt: "2026-09-02T00:00:00Z",
		Secret:                     "whsec_generated",
	}, state)

	if state.WebhookSigningSecret.ValueString() != "whsec_generated" {
		t.Errorf("expected generated signing secret, got %q", state.WebhookSigningSecret.ValueString())
	}
	if state.WebhookVerificationToken.ValueString() != "tok_abc" {
		t.Errorf("expected verification token, got %q", state.WebhookVerificationToken.ValueString())
	}
	if state.WebhookVerificationHeader.ValueString() != "X-Fivenines-Verification" {
		t.Errorf("expected verification header, got %q", state.WebhookVerificationHeader.ValueString())
	}
	if state.WebhookVerificationTokenExpiresAt.ValueString() != "2026-09-02T00:00:00Z" {
		t.Errorf("expected expiry, got %q", state.WebhookVerificationTokenExpiresAt.ValueString())
	}
}

func TestMapWebhookVerificationToState_FallsBackToConfiguredSecret(t *testing.T) {
	// A response that omits the signing key still leaves the practitioner's own
	// `secret` as the key deliveries are signed with.
	state := &integrationModel{Secret: types.StringValue("whsec_mine")}
	mapWebhookVerificationToState(&client.WebhookVerification{
		VerificationHeader: "X-Fivenines-Verification",
		VerificationToken:  "tok_abc",
	}, state)

	if state.WebhookSigningSecret.ValueString() != "whsec_mine" {
		t.Errorf("expected the configured secret, got %q", state.WebhookSigningSecret.ValueString())
	}
}

func TestMapWebhookVerificationToState_NonWebhook(t *testing.T) {
	// Computed attributes must resolve to something after apply; for pagerduty
	// and pushover there is nothing to resolve them to but null.
	//
	// The starting values have to be UNKNOWN, not the zero value. A zero-value
	// types.String is already null, so seeding an empty model would assert
	// nothing — the test would pass with the function body deleted. Unknown is
	// also what Create really holds here: these are Computed, so the plan leaves
	// them unknown, and a leftover unknown fails the apply with "Provider
	// produced inconsistent result after apply".
	state := &integrationModel{
		WebhookSigningSecret:              types.StringUnknown(),
		WebhookVerificationHeader:         types.StringUnknown(),
		WebhookVerificationToken:          types.StringUnknown(),
		WebhookVerificationTokenExpiresAt: types.StringUnknown(),
	}
	mapWebhookVerificationToState(nil, state)

	for name, value := range map[string]types.String{
		"webhook_signing_secret":                state.WebhookSigningSecret,
		"webhook_verification_header":           state.WebhookVerificationHeader,
		"webhook_verification_token":            state.WebhookVerificationToken,
		"webhook_verification_token_expires_at": state.WebhookVerificationTokenExpiresAt,
	} {
		if value.IsUnknown() {
			t.Errorf("%s is still unknown; the apply would fail as an inconsistent result", name)
			continue
		}
		if !value.IsNull() {
			t.Errorf("expected %s to be null, got %v", name, value)
		}
	}
}

func TestTypesAccepting(t *testing.T) {
	if got := typesAccepting("routing_key"); got != `"pagerduty"` {
		t.Errorf(`expected "pagerduty", got %s`, got)
	}
	if got := typesAccepting("secret"); got != `"webhook"` {
		t.Errorf(`expected "webhook", got %s`, got)
	}
}

func TestCreateErrorDetail(t *testing.T) {
	detail := createErrorDetail("pagerduty", &client.APIError{StatusCode: 403, Message: "plan does not include PagerDuty"})
	if !strings.Contains(detail, "plan has to include pagerduty alerts") {
		t.Errorf("expected the 403 hint to name the plan gate, got %q", detail)
	}

	detail = createErrorDetail("pushover", &client.APIError{StatusCode: 502, Message: "bad gateway"})
	if !strings.Contains(detail, "retry the apply") {
		t.Errorf("expected the 502 hint to say the failure is not a verdict, got %q", detail)
	}

	// Anything else is passed through unchanged.
	detail = createErrorDetail("webhook", &client.APIError{StatusCode: 422, Message: "url is invalid"})
	if strings.Contains(detail, "retry") {
		t.Errorf("expected a plain 422 message, got %q", detail)
	}
}

func TestVerifyErrorDetail(t *testing.T) {
	detail := verifyErrorDetail(&client.APIError{StatusCode: 422, Message: "endpoint returned 404"})
	if !strings.Contains(detail, "X-Fivenines-Verification") {
		t.Errorf("expected the 422 detail to name the header, got %q", detail)
	}
	// The channel exists either way, so every message has to say so.
	if !strings.Contains(detail, "recorded in state as unverified") {
		t.Errorf("expected the detail to explain the webhook still exists, got %q", detail)
	}
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// --- Create ---

// integrationPlan builds the plan Terraform hands Create: configured arguments
// carry values, and every Computed attribute is UNKNOWN. That distinction is
// the whole point — Create has to resolve each unknown to something concrete,
// and an unknown left behind fails the apply.
func integrationPlan(t *testing.T, attrs map[string]tftypes.Value) tfsdk.Plan {
	t.Helper()
	ctx := context.Background()
	s := integrationSchema(t)
	objType := s.Type().TerraformType(ctx).(tftypes.Object)

	computed := map[string]bool{
		"id": true, "enabled": true, "verified": true,
		"webhook_signing_secret": true, "webhook_verification_header": true,
		"webhook_verification_token": true, "webhook_verification_token_expires_at": true,
		"created_at": true, "updated_at": true,
	}

	values := map[string]tftypes.Value{}
	for name, attrType := range objType.AttributeTypes {
		if v, set := attrs[name]; set {
			values[name] = v
			continue
		}
		switch {
		case computed[name]:
			values[name] = tftypes.NewValue(attrType, tftypes.UnknownValue)
		case name == "name":
			// Optional+Computed: unknown when the config omits it, which is how
			// the server-assigned fallback reaches state at all.
			values[name] = tftypes.NewValue(attrType, tftypes.UnknownValue)
		case name == "verify_webhook":
			// Optional+Computed with a schema default, resolved before Create.
			values[name] = tftypes.NewValue(attrType, false)
		default:
			values[name] = tftypes.NewValue(attrType, nil)
		}
	}
	for name := range attrs {
		if _, known := objType.AttributeTypes[name]; !known {
			t.Fatalf("test sets unknown attribute %q", name)
		}
	}
	return tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(objType, values)}
}

func createIntegration(t *testing.T, handler http.HandlerFunc, attrs map[string]tftypes.Value) (integrationModel, diag.Diagnostics) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ctx := context.Background()
	s := integrationSchema(t)
	objType := s.Type().TerraformType(ctx).(tftypes.Object)

	r := &integrationResource{client: client.NewClient(srv.URL, "test-key")}
	resp := &resource.CreateResponse{
		State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)},
	}
	r.Create(ctx, resource.CreateRequest{Plan: integrationPlan(t, attrs)}, resp)

	var state integrationModel
	if !resp.State.Raw.IsNull() {
		resp.State.Get(ctx, &state)
	}
	return state, resp.Diagnostics
}

func webhookCreateHandler(t *testing.T, verifyStatus int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/integrations":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"integration": map[string]interface{}{
					"id": 7, "type": "WebhookIntegration", "name": "https://example.com/h",
					"provider": "Webhook", "enabled": true, "verified": false,
					"created_at": "2026-09-01T00:00:00Z", "updated_at": "2026-09-01T00:00:00Z",
				},
				"webhook": map[string]interface{}{
					"verification_header":           "X-Fivenines-Verification",
					"verification_token":            "tok_7",
					"verification_token_expires_at": "2026-09-02T00:00:00Z",
					"secret":                        "whsec_generated",
				},
			})
		case r.Method == "POST" && r.URL.Path == "/api/v1/integrations/7/verify_webhook":
			if verifyStatus != http.StatusOK {
				w.WriteHeader(verifyStatus)
				json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"endpoint returned 404"}})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"integration": map[string]interface{}{
					"id": 7, "type": "WebhookIntegration", "name": "https://example.com/h",
					"enabled": true, "verified": true,
					"created_at": "2026-09-01T00:00:00Z", "updated_at": "2026-09-01T02:00:00Z",
				},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

func TestIntegrationCreate_WebhookRecordsOneShotCredentials(t *testing.T) {
	state, diags := createIntegration(t, webhookCreateHandler(t, http.StatusOK), map[string]tftypes.Value{
		"type": str("webhook"),
		"url":  str("https://example.com/h"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	if state.ID.ValueInt64() != 7 {
		t.Errorf("expected id 7, got %d", state.ID.ValueInt64())
	}
	if state.WebhookSigningSecret.ValueString() != "whsec_generated" {
		t.Errorf("signing secret was not recorded: %v", state.WebhookSigningSecret)
	}
	if state.WebhookVerificationToken.ValueString() != "tok_7" {
		t.Errorf("verification token was not recorded: %v", state.WebhookVerificationToken)
	}
	// verify_webhook defaulted to false, so the channel stays unverified.
	if state.Verified.ValueBool() {
		t.Error("expected verified=false without verify_webhook")
	}
	// The server fell back to the URL for the name; the plan held unknown.
	if state.Name.ValueString() != "https://example.com/h" {
		t.Errorf("expected the server-assigned name, got %q", state.Name.ValueString())
	}
}

func TestIntegrationCreate_VerifyWebhookSucceeds(t *testing.T) {
	state, diags := createIntegration(t, webhookCreateHandler(t, http.StatusOK), map[string]tftypes.Value{
		"type":           str("webhook"),
		"url":            str("https://example.com/h"),
		"verify_webhook": boolean(true),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	if !state.Verified.ValueBool() {
		t.Error("expected verified=true after a successful verify_webhook")
	}
	// Re-reading the integration must not wipe the credentials the create
	// returned: no read can ever return them again.
	if state.WebhookSigningSecret.ValueString() != "whsec_generated" {
		t.Errorf("verification clobbered the signing secret: %v", state.WebhookSigningSecret)
	}
	if state.WebhookVerificationToken.ValueString() != "tok_7" {
		t.Errorf("verification clobbered the token: %v", state.WebhookVerificationToken)
	}
}

// A failed verification must still persist the channel. The API created it
// before the verify call ran, so returning an error with no state would leak a
// real integration that Terraform no longer knows about.
func TestIntegrationCreate_VerifyWebhookFailsButKeepsState(t *testing.T) {
	state, diags := createIntegration(t, webhookCreateHandler(t, http.StatusUnprocessableEntity), map[string]tftypes.Value{
		"type":           str("webhook"),
		"url":            str("https://example.com/h"),
		"verify_webhook": boolean(true),
	})
	if !diags.HasError() {
		t.Fatal("expected the apply to fail when verification fails")
	}
	if state.ID.ValueInt64() != 7 {
		t.Fatalf("the created channel was not saved to state (id %d) — it would leak", state.ID.ValueInt64())
	}
	if state.Verified.ValueBool() {
		t.Error("expected verified=false after a failed verification")
	}
	if state.WebhookVerificationToken.ValueString() != "tok_7" {
		t.Error("the one-shot token was lost on the failure path")
	}
	if !strings.Contains(diags.Errors()[0].Detail(), "X-Fivenines-Verification") {
		t.Errorf("expected the API's reason in the diagnostic, got %q", diags.Errors()[0].Detail())
	}
}

// The end of the same chain M5 exposed, through the real Create path: a channel
// with no webhook block must leave every webhook attribute null. Any left
// unknown fails the apply with "Provider produced inconsistent result".
func TestIntegrationCreate_PagerdutyResolvesWebhookAttributesToNull(t *testing.T) {
	state, diags := createIntegration(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integration": map[string]interface{}{
				"id": 8, "type": "PagerdutyIntegration", "name": "Ops",
				"enabled": true, "verified": true,
				"created_at": "2026-09-01T00:00:00Z", "updated_at": "2026-09-01T00:00:00Z",
			},
		})
	}, map[string]tftypes.Value{
		"type":        str("pagerduty"),
		"name":        str("Ops"),
		"routing_key": str("R0UT1NGK3Y"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	for name, value := range map[string]types.String{
		"webhook_signing_secret":                state.WebhookSigningSecret,
		"webhook_verification_header":           state.WebhookVerificationHeader,
		"webhook_verification_token":            state.WebhookVerificationToken,
		"webhook_verification_token_expires_at": state.WebhookVerificationTokenExpiresAt,
	} {
		if value.IsUnknown() {
			t.Errorf("%s is still unknown after apply", name)
		} else if !value.IsNull() {
			t.Errorf("expected %s to be null on a pagerduty channel, got %v", name, value)
		}
	}
}

// A 202 means the API mailed a verification code and created nothing. The
// resource never sends `email`, but a nil integration must fail loudly rather
// than panic on the next dereference.
func TestIntegrationCreate_NoIntegrationInResponseFailsCleanly(t *testing.T) {
	_, diags := createIntegration(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "pending_verification",
			"verification": map[string]interface{}{"id": 42},
		})
	}, map[string]tftypes.Value{
		"type": str("webhook"),
		"url":  str("https://example.com/h"),
	})
	if !diags.HasError() {
		t.Fatal("expected an error when the API returns no integration")
	}
	if !strings.Contains(diags.Errors()[0].Summary(), "not created") {
		t.Errorf("unexpected diagnostic: %q", diags.Errors()[0].Summary())
	}
}

func TestIntegrationCreate_SurfacesPlanGateAndRetryHints(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusForbidden, "plan has to include"},
		{http.StatusBadGateway, "retry the apply"},
	} {
		_, diags := createIntegration(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "nope"})
		}, map[string]tftypes.Value{
			"type": str("pagerduty"), "name": str("Ops"), "routing_key": str("k"),
		})
		if !diags.HasError() {
			t.Fatalf("status %d: expected an error", tc.status)
		}
		if !strings.Contains(diags.Errors()[0].Detail(), tc.want) {
			t.Errorf("status %d: expected detail to contain %q, got %q", tc.status, tc.want, diags.Errors()[0].Detail())
		}
	}
}

// --- Read and Delete ---

func readIntegration(t *testing.T, handler http.HandlerFunc, prior integrationModel) (integrationModel, bool, diag.Diagnostics) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ctx := context.Background()
	s := integrationSchema(t)
	objType := s.Type().TerraformType(ctx).(tftypes.Object)

	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}
	if d := state.Set(ctx, &prior); d.HasError() {
		t.Fatalf("seeding state: %v", d.Errors())
	}

	r := &integrationResource{client: client.NewClient(srv.URL, "test-key")}
	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	var out integrationModel
	removed := resp.State.Raw.IsNull()
	if !removed {
		resp.State.Get(ctx, &out)
	}
	return out, removed, resp.Diagnostics
}

// priorWebhookState is what a previous apply left behind: the API-visible fields
// plus everything only Terraform still knows.
func priorWebhookState() integrationModel {
	return integrationModel{
		ID: types.Int64Value(7), Type: types.StringValue("webhook"),
		Name: types.StringValue("Ops hook"), URL: types.StringValue("https://example.com/h"),
		Secret: types.StringValue("whsec_mine"), VerifyWebhook: types.BoolValue(false),
		Enabled: types.BoolValue(true), Verified: types.BoolValue(false),
		WebhookSigningSecret:              types.StringValue("whsec_mine"),
		WebhookVerificationHeader:         types.StringValue("X-Fivenines-Verification"),
		WebhookVerificationToken:          types.StringValue("tok_7"),
		WebhookVerificationTokenExpiresAt: types.StringValue("2026-09-02T00:00:00Z"),
		CreatedAt:                         types.StringValue("2026-09-01T00:00:00Z"),
		UpdatedAt:                         types.StringValue("2026-09-01T00:00:00Z"),
		RoutingKey:                        types.StringNull(), UserKey: types.StringNull(), AppToken: types.StringNull(),
	}
}

// A refresh has to pick up what the API serializes and leave alone everything it
// does not. Every argument that identifies this channel, and both one-shot
// credentials, exist only in state — a Read that overwrote them with the zero
// value would destroy them permanently and force a replacement on the next plan.
func TestIntegrationRead_PreservesWriteOnlyFieldsAndPicksUpDrift(t *testing.T) {
	state, removed, diags := readIntegration(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/integrations/7" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integration": map[string]interface{}{
				"id": 7, "type": "WebhookIntegration", "name": "Ops hook",
				"enabled": true,
				// Verified out of band, from the dashboard.
				"verified":   true,
				"created_at": "2026-09-01T00:00:00Z", "updated_at": "2026-09-01T03:00:00Z",
			},
		})
	}, priorWebhookState())

	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	if removed {
		t.Fatal("resource was removed from state on a successful read")
	}
	if !state.Verified.ValueBool() {
		t.Error("expected the read to pick up verified=true")
	}
	if state.UpdatedAt.ValueString() != "2026-09-01T03:00:00Z" {
		t.Errorf("expected updated_at to refresh, got %q", state.UpdatedAt.ValueString())
	}
	for name, got := range map[string]string{
		"type":                       state.Type.ValueString(),
		"url":                        state.URL.ValueString(),
		"secret":                     state.Secret.ValueString(),
		"webhook_signing_secret":     state.WebhookSigningSecret.ValueString(),
		"webhook_verification_token": state.WebhookVerificationToken.ValueString(),
	} {
		if got == "" {
			t.Errorf("%s was wiped by the read; no API response can ever restore it", name)
		}
	}
	if state.Type.ValueString() != "webhook" {
		t.Errorf("the API class name overwrote the configured type: %q", state.Type.ValueString())
	}
}

// Deleted from the dashboard: the next plan has to offer to recreate it rather
// than fail every apply against an id that is gone.
func TestIntegrationRead_RemovesDeletedChannelFromState(t *testing.T) {
	_, removed, diags := readIntegration(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	}, priorWebhookState())

	if diags.HasError() {
		t.Fatalf("a 404 is drift, not an error: %v", diags.Errors())
	}
	if !removed {
		t.Error("expected the resource to be removed from state after a 404")
	}
}

func TestIntegrationRead_SurfacesRealErrors(t *testing.T) {
	_, removed, diags := readIntegration(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "boom"})
	}, priorWebhookState())

	if !diags.HasError() {
		t.Fatal("expected a 500 to surface as an error")
	}
	if removed {
		t.Error("a 500 must not be mistaken for drift and drop the resource from state")
	}
}

func deleteIntegration(t *testing.T, handler http.HandlerFunc) diag.Diagnostics {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ctx := context.Background()
	s := integrationSchema(t)
	objType := s.Type().TerraformType(ctx).(tftypes.Object)
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}
	if d := state.Set(ctx, priorWebhookState()); d.HasError() {
		t.Fatalf("seeding state: %v", d.Errors())
	}

	r := &integrationResource{client: client.NewClient(srv.URL, "test-key")}
	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)
	return resp.Diagnostics
}

func TestIntegrationDelete(t *testing.T) {
	t.Run("204", func(t *testing.T) {
		diags := deleteIntegration(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" || r.URL.Path != "/api/v1/integrations/7" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		})
		if diags.HasError() {
			t.Errorf("unexpected diagnostics: %v", diags.Errors())
		}
	})

	// Already gone is the outcome destroy wanted, not a failure.
	t.Run("404 is not an error", func(t *testing.T) {
		diags := deleteIntegration(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
		})
		if diags.HasError() {
			t.Errorf("a 404 on delete must not fail the destroy: %v", diags.Errors())
		}
	})

	t.Run("403 is an error", func(t *testing.T) {
		diags := deleteIntegration(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "no write scope"})
		})
		if !diags.HasError() {
			t.Error("expected a 403 to fail the destroy rather than silently drop state")
		}
	})
}

// Terraform should never reach Update: every argument is RequiresReplace. If a
// plan modifier is ever dropped, this is the message that explains why.
func TestIntegrationUpdate_IsUnreachable(t *testing.T) {
	resp := &resource.UpdateResponse{}
	(&integrationResource{}).Update(context.Background(), resource.UpdateRequest{}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected Update to report that integrations cannot be updated")
	}
}
