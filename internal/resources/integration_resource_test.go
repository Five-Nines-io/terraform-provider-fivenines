package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
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
			want:  "require `url`",
		},
		"pagerduty without name": {
			attrs: map[string]tftypes.Value{"type": str("pagerduty"), "routing_key": str("k")},
			want:  "require `name`",
		},
		"pagerduty without routing_key": {
			attrs: map[string]tftypes.Value{"type": str("pagerduty"), "name": str("Ops")},
			want:  "require `routing_key`",
		},
		"pushover without app_token": {
			attrs: map[string]tftypes.Value{"type": str("pushover"), "name": str("On-call"), "user_key": str("uk_1")},
			want:  "require `app_token`",
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
			want: "`routing_key` does not apply to \"webhook\"",
		},
		"url on pagerduty": {
			attrs: map[string]tftypes.Value{
				"type": str("pagerduty"), "name": str("Ops"), "routing_key": str("k"), "url": str("https://example.com/h"),
			},
			want: "`url` does not apply to \"pagerduty\"",
		},
		"verify_webhook on pushover": {
			attrs: map[string]tftypes.Value{
				"type": str("pushover"), "name": str("On-call"), "user_key": str("uk"), "app_token": str("at"),
				"verify_webhook": boolean(true),
			},
			want: "`verify_webhook` does not apply to \"pushover\"",
		},
		"app_token on a webhook": {
			attrs: map[string]tftypes.Value{
				"type": str("webhook"), "url": str("https://example.com/h"), "app_token": str("at"),
			},
			want: "`app_token` does not apply to \"webhook\"",
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
	state := &integrationModel{}
	mapWebhookVerificationToState(nil, state)

	for name, value := range map[string]types.String{
		"webhook_signing_secret":                state.WebhookSigningSecret,
		"webhook_verification_header":           state.WebhookVerificationHeader,
		"webhook_verification_token":            state.WebhookVerificationToken,
		"webhook_verification_token_expires_at": state.WebhookVerificationTokenExpiresAt,
	} {
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
