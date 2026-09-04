package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// uptimeMonitorSchema returns the live resource schema so the tests assert
// against what the provider actually serves, not a copy of it.
func uptimeMonitorSchema(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	(&uptimeMonitorResource{}).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// nullObjectValue builds an object value of typ with every attribute null,
// then applies overrides. It keeps the config fixtures to the attributes a
// case actually cares about.
func nullObjectValue(t *testing.T, typ tftypes.Type, overrides map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	obj, ok := typ.(tftypes.Object)
	if !ok {
		t.Fatalf("expected an object type, got %T", typ)
	}
	attrs := make(map[string]tftypes.Value, len(obj.AttributeTypes))
	for name, attrType := range obj.AttributeTypes {
		attrs[name] = tftypes.NewValue(attrType, nil)
	}
	for name, value := range overrides {
		if _, ok := obj.AttributeTypes[name]; !ok {
			t.Fatalf("override for unknown attribute %q", name)
		}
		attrs[name] = value
	}
	return tftypes.NewValue(obj, attrs)
}

// errorPaths returns the attribute paths of every error diagnostic, so a case
// can assert which attribute was blamed and not just how many errors fired.
func errorPaths(diags diag.Diagnostics) []string {
	var paths []string
	for _, d := range diags.Errors() {
		if withPath, ok := d.(diag.DiagnosticWithPath); ok {
			paths = append(paths, withPath.Path().String())
			continue
		}
		paths = append(paths, "")
	}
	return paths
}

// --- ValidateConfig ---

func TestUptimeMonitorValidateConfig(t *testing.T) {
	ctx := context.Background()
	s := uptimeMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)

	tests := []struct {
		name      string
		config    map[string]tftypes.Value
		wantPaths []string
	}{
		{
			name:      "https without url",
			config:    map[string]tftypes.Value{"protocol": tftypes.NewValue(tftypes.String, "https")},
			wantPaths: []string{"url"},
		},
		{
			name: "https with url",
			config: map[string]tftypes.Value{
				"protocol": tftypes.NewValue(tftypes.String, "https"),
				"url":      tftypes.NewValue(tftypes.String, "https://example.com"),
			},
		},
		{
			name:      "tcp without hostname or port",
			config:    map[string]tftypes.Value{"protocol": tftypes.NewValue(tftypes.String, "tcp")},
			wantPaths: []string{"hostname", "port"},
		},
		{
			name: "tcp with hostname but no port",
			config: map[string]tftypes.Value{
				"protocol": tftypes.NewValue(tftypes.String, "tcp"),
				"hostname": tftypes.NewValue(tftypes.String, "db.example.com"),
			},
			wantPaths: []string{"port"},
		},
		{
			name: "tcp fully configured",
			config: map[string]tftypes.Value{
				"protocol": tftypes.NewValue(tftypes.String, "tcp"),
				"hostname": tftypes.NewValue(tftypes.String, "db.example.com"),
				"port":     tftypes.NewValue(tftypes.Number, 5432),
			},
		},
		{
			name:      "icmp without hostname",
			config:    map[string]tftypes.Value{"protocol": tftypes.NewValue(tftypes.String, "icmp")},
			wantPaths: []string{"hostname"},
		},
		{
			name:      "dns without hostname or record type",
			config:    map[string]tftypes.Value{"protocol": tftypes.NewValue(tftypes.String, "dns")},
			wantPaths: []string{"hostname", "dns_record_type"},
		},
		{
			// The name being resolved is as required as the record type: the
			// server validates hostname presence on every dns write, so a config
			// carrying only the record type would 422 on apply.
			name: "dns with record type but no hostname",
			config: map[string]tftypes.Value{
				"protocol":        tftypes.NewValue(tftypes.String, "dns"),
				"dns_record_type": tftypes.NewValue(tftypes.String, "A"),
			},
			wantPaths: []string{"hostname"},
		},
		{
			name: "dns fully configured",
			config: map[string]tftypes.Value{
				"protocol":        tftypes.NewValue(tftypes.String, "dns"),
				"hostname":        tftypes.NewValue(tftypes.String, "example.com"),
				"dns_record_type": tftypes.NewValue(tftypes.String, "A"),
			},
		},
		{
			// Nothing to check yet; leave it to the OneOf validator and the server.
			name:   "null protocol short-circuits",
			config: map[string]tftypes.Value{},
		},
		{
			// A protocol behind an unresolved reference must not be guessed at.
			name:   "unknown protocol short-circuits",
			config: map[string]tftypes.Value{"protocol": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)},
		},
		{
			// Rails `presence: true` rejects "" as well as nil, so a variable that
			// collapsed to blank must fail at plan time, not as a 422 on apply.
			name: "an empty string url is a missing url",
			config: map[string]tftypes.Value{
				"protocol": tftypes.NewValue(tftypes.String, "https"),
				"url":      tftypes.NewValue(tftypes.String, ""),
			},
			wantPaths: []string{"url"},
		},
		{
			// The requirement is on the config being resolvable, not resolved.
			name: "unknown url is not a missing url",
			config: map[string]tftypes.Value{
				"protocol": tftypes.NewValue(tftypes.String, "https"),
				"url":      tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			},
		},
		{
			// Protocols outside protocolRequirements have no cross-field rules;
			// the OneOf validator on the attribute rejects them separately.
			name:   "unrecognised protocol has no cross-field rules",
			config: map[string]tftypes.Value{"protocol": tftypes.NewValue(tftypes.String, "gopher")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s, Raw: nullObjectValue(t, objType, tt.config)},
			}
			resp := &resource.ValidateConfigResponse{}
			(&uptimeMonitorResource{}).ValidateConfig(ctx, req, resp)

			got := errorPaths(resp.Diagnostics)
			if len(got) != len(tt.wantPaths) {
				t.Fatalf("expected errors on %v, got %v (%v)", tt.wantPaths, got, resp.Diagnostics.Errors())
			}
			for i := range got {
				if got[i] != tt.wantPaths[i] {
					t.Errorf("expected errors on %v, got %v", tt.wantPaths, got)
				}
			}
			for _, d := range resp.Diagnostics.Errors() {
				if d.Summary() != "Missing required attribute" {
					t.Errorf("unexpected diagnostic summary: %q", d.Summary())
				}
			}
		})
	}
}

// The API's clear_body_unless_post callback nils custom_body and content_type
// for anything but a POST, so a config that sets either beside a GET plans a
// known value the apply silently drops. The trap is the schema default: leaving
// http_method out means GET.
func TestUptimeMonitorValidateConfig_PostOnlyAttributes(t *testing.T) {
	ctx := context.Background()
	s := uptimeMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)

	https := tftypes.NewValue(tftypes.String, "https")
	url := tftypes.NewValue(tftypes.String, "https://example.com")

	tests := []struct {
		name      string
		config    map[string]tftypes.Value
		wantPaths []string
	}{
		{
			name: "body and content type on a POST",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"http_method":  tftypes.NewValue(tftypes.String, "POST"),
				"custom_body":  tftypes.NewValue(tftypes.String, `{"ping":true}`),
				"content_type": tftypes.NewValue(tftypes.String, "application/json"),
			},
		},
		{
			name: "body on an explicit GET",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"http_method": tftypes.NewValue(tftypes.String, "GET"),
				"custom_body": tftypes.NewValue(tftypes.String, `{"ping":true}`),
			},
			wantPaths: []string{"custom_body"},
		},
		{
			// The easy mistake: http_method is Optional+Computed with a GET
			// default, so omitting it is not "unset", it is GET.
			name: "body with http_method left out entirely",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"custom_body": tftypes.NewValue(tftypes.String, `{"ping":true}`),
			},
			wantPaths: []string{"custom_body"},
		},
		{
			name: "both attributes on a HEAD",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"http_method":  tftypes.NewValue(tftypes.String, "HEAD"),
				"custom_body":  tftypes.NewValue(tftypes.String, "x"),
				"content_type": tftypes.NewValue(tftypes.String, "text/plain"),
			},
			wantPaths: []string{"custom_body", "content_type"},
		},
		{
			// A method behind an unresolved reference may still be POST.
			name: "unknown http_method short-circuits",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"http_method": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
				"custom_body": tftypes.NewValue(tftypes.String, "x"),
			},
		},
		{
			name: "no post-only attributes set",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"http_method": tftypes.NewValue(tftypes.String, "GET"),
			},
		},
		{
			// content_type alone is just as lossy: the same callback nils both,
			// so a Content-Type pinned beside a GET is dropped on the way in.
			name: "content type alone on a GET",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"http_method":  tftypes.NewValue(tftypes.String, "GET"),
				"content_type": tftypes.NewValue(tftypes.String, "application/json"),
			},
			wantPaths: []string{"content_type"},
		},
		{
			// The body itself can be the unresolved half — jsonencode of a value
			// computed elsewhere. An unknown is not yet a value the apply could
			// drop, so it is not yet something to refuse.
			name: "unknown custom_body is not reported",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"http_method": tftypes.NewValue(tftypes.String, "GET"),
				"custom_body": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			},
		},
		{
			// The post-only rule reads http_method and nothing else, so an
			// unresolved protocol reference must not disable it: that config
			// still applies a GET, and still has its body dropped.
			name: "an unknown protocol does not disable the post-only rule",
			config: map[string]tftypes.Value{
				"protocol":    tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
				"custom_body": tftypes.NewValue(tftypes.String, "x"),
			},
			wantPaths: []string{"custom_body"},
		},
		{
			// tcp forbids both outright, so the protocol rule reports them and
			// the post-only rule stays quiet rather than doubling up.
			name: "not reported twice on a protocol that forbids them",
			config: map[string]tftypes.Value{
				"protocol":    tftypes.NewValue(tftypes.String, "tcp"),
				"hostname":    tftypes.NewValue(tftypes.String, "db.example.com"),
				"port":        tftypes.NewValue(tftypes.Number, 5432),
				"custom_body": tftypes.NewValue(tftypes.String, "x"),
			},
			wantPaths: []string{"custom_body"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s, Raw: nullObjectValue(t, objType, tt.config)},
			}
			resp := &resource.ValidateConfigResponse{}
			(&uptimeMonitorResource{}).ValidateConfig(ctx, req, resp)

			got := errorPaths(resp.Diagnostics)
			if len(got) != len(tt.wantPaths) {
				t.Fatalf("expected errors on %v, got %v (%v)", tt.wantPaths, got, resp.Diagnostics.Errors())
			}
			for i := range got {
				if got[i] != tt.wantPaths[i] {
					t.Errorf("expected errors on %v, got %v", tt.wantPaths, got)
				}
			}
		})
	}
}

// The detail has to name the method that will actually be applied, because the
// trap is a method the config never mentions. Asserting only the blamed path
// leaves effectiveHTTPMethod free to return anything at all: both of its
// branches run under the table above without either message being read.
func TestUptimeMonitorValidateConfig_PostOnlyDiagnosticNamesTheMethod(t *testing.T) {
	ctx := context.Background()
	s := uptimeMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)

	https := tftypes.NewValue(tftypes.String, "https")
	url := tftypes.NewValue(tftypes.String, "https://example.com")

	tests := []struct {
		name       string
		config     map[string]tftypes.Value
		wantDetail string
	}{
		{
			// Nothing in the HCL says GET, so the message has to say where it
			// came from or the reader goes hunting for a line they never wrote.
			name: "an omitted method is named as the schema default",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"custom_body": tftypes.NewValue(tftypes.String, "x"),
			},
			wantDetail: "clears it for GET (the default, since http_method is unset)",
		},
		{
			name: "an explicit method is quoted back verbatim",
			config: map[string]tftypes.Value{
				"protocol": https, "url": url,
				"http_method": tftypes.NewValue(tftypes.String, "HEAD"),
				"custom_body": tftypes.NewValue(tftypes.String, "x"),
			},
			wantDetail: `clears it for "HEAD"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s, Raw: nullObjectValue(t, objType, tt.config)},
			}
			resp := &resource.ValidateConfigResponse{}
			(&uptimeMonitorResource{}).ValidateConfig(ctx, req, resp)

			errs := resp.Diagnostics.Errors()
			if len(errs) != 1 {
				t.Fatalf("expected exactly one error, got %v", errs)
			}
			if got := errs[0].Summary(); got != "Attribute requires a POST request" {
				t.Errorf("unexpected summary: %q", got)
			}
			if got := errs[0].Detail(); !strings.Contains(got, tt.wantDetail) {
				t.Errorf("detail %q does not mention %q", got, tt.wantDetail)
			}
		})
	}
}

// custom_headers is Sensitive because it carries an Authorization header, and
// Terraform does NOT redact a sensitive value inside a provider diagnostic. The
// library's RegexMatches formats "Attribute %s %s, got: %s" with the raw value,
// so using it here printed the bearer token on every plan. Assert the message
// names the offence without ever quoting the value.
func TestUptimeMonitorSchema_CustomHeaderDiagnosticDoesNotLeakTheValue(t *testing.T) {
	s := uptimeMonitorSchema(t)
	attribute := s.Attributes["custom_headers"].(rschema.MapAttribute)

	const secret = "Bearer sk_live_SUPERSECRET_abcdef123456"
	diags := validateMap(t, attribute.MapValidators(), "custom_headers",
		stringMap(t, map[string]string{"Authorization": secret + "\n"}))

	if !diags.HasError() {
		t.Fatal("a trailing newline in a header value must be rejected")
	}
	for _, d := range diags.Errors() {
		message := d.Summary() + " " + d.Detail()
		if strings.Contains(message, secret) {
			t.Errorf("diagnostic leaked the header value: %q", message)
		}
		if !strings.Contains(message, "line feed") {
			t.Errorf("diagnostic should name the offending character, got: %q", message)
		}
	}
}

// A null entry clears every element validator by contract (they all skip nulls)
// and then marshals as "" — a value the plan never promised, which the API
// stores and echoes back.
func TestUptimeMonitorSchema_RejectsNullEntries(t *testing.T) {
	listValidators := listValidatorsFor(t, "dns_expected_records")
	if diags := validateList(t, listValidators, "dns_expected_records",
		mixedStringList(t, types.StringValue("1.2.3.4"), types.StringNull())); !diags.HasError() {
		t.Error("a null dns_expected_records entry must be rejected at plan time")
	}
	if diags := validateList(t, listValidators, "dns_expected_records",
		stringList(t, "1.2.3.4")); diags.HasError() {
		t.Errorf("a list with no nulls must pass, got %v", diags.Errors())
	}

	s := uptimeMonitorSchema(t)
	mapValidators := s.Attributes["custom_headers"].(rschema.MapAttribute).MapValidators()
	nullValued := types.MapValueMust(types.StringType, map[string]attr.Value{
		"X-Trace": types.StringNull(),
	})
	if diags := validateMap(t, mapValidators, "custom_headers", nullValued); !diags.HasError() {
		t.Error("a null custom_headers value must be rejected at plan time")
	}
}

// --- schema: protocol is updatable in place ---

func TestUptimeMonitorSchema_ProtocolIsUpdatableInPlace(t *testing.T) {
	s := uptimeMonitorSchema(t)
	attribute, ok := s.Attributes["protocol"].(rschema.StringAttribute)
	if !ok {
		t.Fatalf("expected protocol to be a StringAttribute, got %T", s.Attributes["protocol"])
	}
	// Re-adding RequiresReplace here would silently turn every protocol change
	// back into a destroy/create and lose the monitor's check history.
	if len(attribute.PlanModifiers) != 0 {
		t.Errorf("expected no plan modifiers on protocol, got %d", len(attribute.PlanModifiers))
	}
	if !attribute.IsRequired() {
		t.Error("expected protocol to stay required")
	}
}

// --- schema validators ---

func validateList(t *testing.T, validators []validator.List, name string, value types.List) diag.Diagnostics {
	t.Helper()
	req := validator.ListRequest{
		Path:           path.Root(name),
		PathExpression: path.MatchRoot(name),
		ConfigValue:    value,
	}
	resp := &validator.ListResponse{}
	for _, v := range validators {
		v.ValidateList(context.Background(), req, resp)
	}
	return resp.Diagnostics
}

func listValidatorsFor(t *testing.T, name string) []validator.List {
	t.Helper()
	s := uptimeMonitorSchema(t)
	attribute, ok := s.Attributes[name].(rschema.ListAttribute)
	if !ok {
		t.Fatalf("expected %s to be a ListAttribute, got %T", name, s.Attributes[name])
	}
	validators := attribute.ListValidators()
	if len(validators) == 0 {
		t.Fatalf("expected %s to declare validators", name)
	}
	return validators
}

func int64List(t *testing.T, values ...int64) types.List {
	t.Helper()
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.Int64Value(v))
	}
	return types.ListValueMust(types.Int64Type, elems)
}

func stringList(t *testing.T, values ...string) types.List {
	t.Helper()
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elems)
}

func TestUptimeMonitorSchema_ExpectedStatusCodesValidators(t *testing.T) {
	validators := listValidatorsFor(t, "expected_status_codes")

	fifty := make([]int64, 0, 51)
	for i := 0; i < 51; i++ {
		fifty = append(fifty, 200)
	}

	tests := []struct {
		name      string
		value     types.List
		wantError bool
	}{
		{name: "null is skipped", value: types.ListNull(types.Int64Type)},
		{name: "unknown is skipped", value: types.ListUnknown(types.Int64Type)},
		{name: "single code", value: int64List(t, 200)},
		{name: "lower bound", value: int64List(t, 100)},
		{name: "upper bound", value: int64List(t, 599)},
		{name: "several codes", value: int64List(t, 200, 201, 301)},
		// An empty list would match nothing, which silently breaks the monitor.
		{name: "empty list", value: int64List(t), wantError: true},
		{name: "below range", value: int64List(t, 99), wantError: true},
		{name: "above range", value: int64List(t, 600), wantError: true},
		{name: "zero", value: int64List(t, 0), wantError: true},
		{name: "negative", value: int64List(t, -1), wantError: true},
		{name: "one bad code among good ones", value: int64List(t, 200, 700), wantError: true},
		{name: "over the size cap", value: int64List(t, fifty...), wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateList(t, validators, "expected_status_codes", tt.value)
			if diags.HasError() != tt.wantError {
				t.Errorf("expected error=%v, got %v", tt.wantError, diags.Errors())
			}
		})
	}
}

func TestUptimeMonitorSchema_DNSExpectedRecordsValidators(t *testing.T) {
	validators := listValidatorsFor(t, "dns_expected_records")

	tooMany := make([]string, 0, 51)
	for i := 0; i < 51; i++ {
		tooMany = append(tooMany, "1.2.3.4")
	}

	tests := []struct {
		name      string
		value     types.List
		wantError bool
	}{
		{name: "null is skipped", value: types.ListNull(types.StringType)},
		{name: "unknown is skipped", value: types.ListUnknown(types.StringType)},
		// Unlike expected_status_codes, [] is the documented way to clear the
		// pinned expectation, so it must stay valid.
		{name: "empty list is allowed", value: stringList(t)},
		{name: "a few records", value: stringList(t, "1.2.3.4", "5.6.7.8")},
		// The API stores a blank record and then never matches it, so the plan is
		// the only place it can be caught.
		{name: "empty string record", value: stringList(t, ""), wantError: true},
		{name: "whitespace-only record", value: stringList(t, "   "), wantError: true},
		{name: "record with trailing whitespace", value: stringList(t, "1.2.3.4 "), wantError: true},
		{name: "record with a leading newline from a split", value: stringList(t, "1.2.3.4", "\n"), wantError: true},
		{name: "record at the length cap", value: stringList(t, strings.Repeat("a", 2048))},
		{name: "record over the length cap", value: stringList(t, strings.Repeat("a", 2049)), wantError: true},
		{name: "over the size cap", value: stringList(t, tooMany...), wantError: true},
		// The per-record cap is characters, matching the API's Ruby String#length.
		// Measured in bytes a 2048-rune multi-byte record is 6144 and would be
		// rejected here while the API accepts it.
		{name: "multi-byte record at the character cap", value: stringList(t, strings.Repeat("é", 2048))},
		{name: "multi-byte record over the character cap", value: stringList(t, strings.Repeat("é", 2049)), wantError: true},
		// A NUL is refused by Postgres inside jsonb — past every server validation,
		// so it is a 500 rather than a 422 unless it is caught first.
		{name: "record with a NUL byte", value: stringList(t, "1.2.3.4\x00"), wantError: true},
		{name: "record with a line feed", value: stringList(t, "1.2.3.4\nsecond"), wantError: true},
		{name: "record with a carriage return", value: stringList(t, "1.2.3.4\rsecond"), wantError: true},
		// Under every per-record rule, over the joined blob cap: 50 x 160
		// characters is 8000 of payload and 8098 once the CRLFs are counted.
		{name: "under the joined cap", value: stringList(t, joinedRecords(50, 160)...)},
		{name: "over the joined cap", value: stringList(t, joinedRecords(50, 165)...), wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateList(t, validators, "dns_expected_records", tt.value)
			if diags.HasError() != tt.wantError {
				t.Errorf("expected error=%v, got %v", tt.wantError, diags.Errors())
			}
		})
	}
}

// joinedRecords builds count records of width characters each, all under the
// per-record cap, for exercising the CRLF-joined total.
func joinedRecords(count, width int) []string {
	records := make([]string, 0, count)
	for i := 0; i < count; i++ {
		records = append(records, strings.Repeat("a", width))
	}
	return records
}

func validateInt64(t *testing.T, validators []validator.Int64, name string, value types.Int64) diag.Diagnostics {
	t.Helper()
	req := validator.Int64Request{
		Path:           path.Root(name),
		PathExpression: path.MatchRoot(name),
		ConfigValue:    value,
	}
	resp := &validator.Int64Response{}
	for _, v := range validators {
		v.ValidateInt64(context.Background(), req, resp)
	}
	return resp.Diagnostics
}

func int64ValidatorsFor(t *testing.T, name string) []validator.Int64 {
	t.Helper()
	s := uptimeMonitorSchema(t)
	attribute, ok := s.Attributes[name].(rschema.Int64Attribute)
	if !ok {
		t.Fatalf("expected %s to be an Int64Attribute, got %T", name, s.Attributes[name])
	}
	validators := attribute.Int64Validators()
	if len(validators) == 0 {
		t.Fatalf("expected %s to declare validators", name)
	}
	return validators
}

func validateMap(t *testing.T, validators []validator.Map, name string, value types.Map) diag.Diagnostics {
	t.Helper()
	req := validator.MapRequest{
		Path:           path.Root(name),
		PathExpression: path.MatchRoot(name),
		ConfigValue:    value,
	}
	resp := &validator.MapResponse{}
	for _, v := range validators {
		v.ValidateMap(context.Background(), req, resp)
	}
	return resp.Diagnostics
}

func validateString(t *testing.T, validators []validator.String, name string, value types.String) diag.Diagnostics {
	t.Helper()
	req := validator.StringRequest{
		Path:           path.Root(name),
		PathExpression: path.MatchRoot(name),
		ConfigValue:    value,
	}
	resp := &validator.StringResponse{}
	for _, v := range validators {
		v.ValidateString(context.Background(), req, resp)
	}
	return resp.Diagnostics
}

func stringMap(t *testing.T, pairs map[string]string) types.Map {
	t.Helper()
	elems := make(map[string]attr.Value, len(pairs))
	for k, v := range pairs {
		elems[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, elems)
}

// The API bounds these four, and every bound it rejects is an apply-time 422 the
// provider can turn into a plan-time message for free.
func TestUptimeMonitorSchema_NumericValidators(t *testing.T) {
	tests := []struct {
		attribute string
		value     int64
		wantError bool
	}{
		// timeout_seconds: greater_than 0, less_than_or_equal_to 15
		{attribute: "timeout_seconds", value: 1},
		{attribute: "timeout_seconds", value: 15},
		{attribute: "timeout_seconds", value: 0, wantError: true},
		{attribute: "timeout_seconds", value: 16, wantError: true},
		// interval_seconds: greater_than 0, with a plan-dependent floor the API owns
		{attribute: "interval_seconds", value: 1},
		{attribute: "interval_seconds", value: 300},
		{attribute: "interval_seconds", value: 0, wantError: true},
		{attribute: "interval_seconds", value: -60, wantError: true},
		// confirmation_count: greater_than 0
		{attribute: "confirmation_count", value: 1},
		{attribute: "confirmation_count", value: 0, wantError: true},
		// recovery_count: greater_than 0, less_than_or_equal_to 10
		{attribute: "recovery_count", value: 1},
		{attribute: "recovery_count", value: 10},
		{attribute: "recovery_count", value: 0, wantError: true},
		{attribute: "recovery_count", value: 11, wantError: true},
		// port: greater_than 0, less_than_or_equal_to 65535 (tcp only server-side,
		// but the schema bound is unconditional). Pre-dates this sweep; included so
		// the sweep is the one place every numeric bound on this resource is pinned.
		{attribute: "port", value: 1},
		{attribute: "port", value: 65535},
		{attribute: "port", value: 0, wantError: true},
		{attribute: "port", value: 65536, wantError: true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s=%d", tt.attribute, tt.value), func(t *testing.T) {
			validators := int64ValidatorsFor(t, tt.attribute)
			diags := validateInt64(t, validators, tt.attribute, types.Int64Value(tt.value))
			if diags.HasError() != tt.wantError {
				t.Errorf("expected error=%v, got %v", tt.wantError, diags.Errors())
			}
		})
	}

	// Null and unknown reach every one of them and must never error: an unset
	// attribute takes its default, and an unresolved reference is not a value yet.
	for _, name := range []string{"timeout_seconds", "interval_seconds", "confirmation_count", "recovery_count", "port"} {
		validators := int64ValidatorsFor(t, name)
		for label, value := range map[string]types.Int64{
			"null": types.Int64Null(), "unknown": types.Int64Unknown(),
		} {
			if diags := validateInt64(t, validators, name, value); diags.HasError() {
				t.Errorf("%s %s should be skipped, got %v", name, label, diags.Errors())
			}
		}
	}
}

func TestUptimeMonitorSchema_CustomHeadersValidators(t *testing.T) {
	s := uptimeMonitorSchema(t)
	attribute, ok := s.Attributes["custom_headers"].(rschema.MapAttribute)
	if !ok {
		t.Fatalf("expected custom_headers to be a MapAttribute, got %T", s.Attributes["custom_headers"])
	}
	validators := attribute.MapValidators()
	if len(validators) == 0 {
		t.Fatal("expected custom_headers to declare validators")
	}

	tooMany := make(map[string]string, 21)
	for i := 0; i < 21; i++ {
		tooMany[fmt.Sprintf("X-Header-%d", i)] = "v"
	}
	exactlyTwenty := make(map[string]string, 20)
	for i := 0; i < 20; i++ {
		exactlyTwenty[fmt.Sprintf("X-Header-%d", i)] = "v"
	}

	tests := []struct {
		name      string
		value     types.Map
		wantError bool
	}{
		{name: "null is skipped", value: types.MapNull(types.StringType)},
		{name: "unknown is skipped", value: types.MapUnknown(types.StringType)},
		{name: "empty map", value: stringMap(t, map[string]string{})},
		{name: "a normal header", value: stringMap(t, map[string]string{"Authorization": "Bearer t"})},
		{name: "hyphens and underscores", value: stringMap(t, map[string]string{"X-Trace_Id": "abc"})},
		{name: "value at the 4KB cap", value: stringMap(t, map[string]string{"X-Big": strings.Repeat("a", 4096)})},
		{name: "value over the 4KB cap", value: stringMap(t, map[string]string{"X-Big": strings.Repeat("a", 4097)}), wantError: true},
		// Bytes, matching the API's value_str.bytesize — unlike dns_expected_records,
		// which the server caps in characters. Pinning both sides here stops a
		// consistency "fix" to UTF8LengthAtMost from doubling the accepted size.
		{name: "multi-byte value at the byte cap", value: stringMap(t, map[string]string{"X-Big": strings.Repeat("é", 2048)})},
		{name: "multi-byte value over the byte cap", value: stringMap(t, map[string]string{"X-Big": strings.Repeat("é", 2049)}), wantError: true},
		// A CR or LF in a header value is a response-splitting vector, which is
		// why the API refuses it outright rather than escaping it.
		{name: "value with a line feed", value: stringMap(t, map[string]string{"X-Bad": "a\nb"}), wantError: true},
		{name: "value with a carriage return", value: stringMap(t, map[string]string{"X-Bad": "a\rb"}), wantError: true},
		{name: "value with a NUL byte", value: stringMap(t, map[string]string{"X-Bad": "a\x00b"}), wantError: true},
		{name: "name with a colon", value: stringMap(t, map[string]string{"X-Bad:": "v"}), wantError: true},
		{name: "name with a space", value: stringMap(t, map[string]string{"X Bad": "v"}), wantError: true},
		{name: "empty name", value: stringMap(t, map[string]string{"": "v"}), wantError: true},
		{name: "exactly at the header count cap", value: stringMap(t, exactlyTwenty)},
		{name: "over the header count cap", value: stringMap(t, tooMany), wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateMap(t, validators, "custom_headers", tt.value)
			if diags.HasError() != tt.wantError {
				t.Errorf("expected error=%v, got %v", tt.wantError, diags.Errors())
			}
		})
	}
}

func TestUptimeMonitorSchema_CustomBodyValidator(t *testing.T) {
	s := uptimeMonitorSchema(t)
	attribute, ok := s.Attributes["custom_body"].(rschema.StringAttribute)
	if !ok {
		t.Fatalf("expected custom_body to be a StringAttribute, got %T", s.Attributes["custom_body"])
	}
	validators := attribute.StringValidators()
	if len(validators) == 0 {
		t.Fatal("expected custom_body to declare validators")
	}

	tests := []struct {
		name      string
		value     types.String
		wantError bool
	}{
		{name: "null is skipped", value: types.StringNull()},
		{name: "unknown is skipped", value: types.StringUnknown()},
		{name: "a json body", value: types.StringValue(`{"ping":true}`)},
		{name: "at the 64KB cap", value: types.StringValue(strings.Repeat("a", 65536))},
		{name: "over the 64KB cap", value: types.StringValue(strings.Repeat("a", 65537)), wantError: true},
		// The API caps bytesize, so multi-byte characters count for what they
		// weigh on the wire rather than for one apiece.
		{name: "multi-byte over the cap in bytes", value: types.StringValue(strings.Repeat("é", 32769)), wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateString(t, validators, "custom_body", tt.value)
			if diags.HasError() != tt.wantError {
				t.Errorf("expected error=%v, got %v", tt.wantError, diags.Errors())
			}
		})
	}
}

// mixedStringList builds a string list from element values directly, so a case
// can put a null or an unknown inside an otherwise known list — something
// stringList, which takes plain Go strings, cannot express.
func mixedStringList(t *testing.T, elems ...attr.Value) types.List {
	t.Helper()
	return types.ListValueMust(types.StringType, elems)
}

// The joined cap is the whole reason this is a bespoke validator rather than
// another listvalidator, and the separators are the whole difference. The cases
// already on dns_expected_records straddle the cap by so much that the CRLF
// arithmetic could be deleted outright and they would all still pass, so this
// pins the boundary from both sides.
func TestUptimeMonitorSchema_DNSExpectedRecordsCountsCRLFSeparators(t *testing.T) {
	validators := listValidatorsFor(t, "dns_expected_records")

	// 5 x 1636 is 8180 characters of payload and 8188 once the four separators
	// are counted: inside the cap either way.
	if diags := validateList(t, validators, "dns_expected_records",
		stringList(t, joinedRecords(5, 1636)...)); diags.HasError() {
		t.Errorf("8188 joined characters must be accepted, got %v", diags.Errors())
	}
	// 5 x 1638 is 8190 of payload — under the cap on its own — and 8198 joined.
	// Only counting the separators rejects this one, and the API would 422 it.
	if diags := validateList(t, validators, "dns_expected_records",
		stringList(t, joinedRecords(5, 1638)...)); !diags.HasError() {
		t.Error("the four CRLF pairs must push an 8190 character payload over the 8192 cap")
	}
}

// CRLFJoinedLengthAtMost is the only validator on this resource that walks the
// elements itself instead of delegating to the framework, so its own null and
// unknown handling has to hold: a value Terraform has not resolved cannot be
// guessed at in either direction, and a null element must not stop the count.
func TestCRLFJoinedLengthAtMost(t *testing.T) {
	tests := []struct {
		name      string
		max       int
		value     types.List
		wantError bool
	}{
		{name: "null list is skipped", max: 1, value: types.ListNull(types.StringType)},
		{name: "unknown list is skipped", max: 1, value: types.ListUnknown(types.StringType)},
		{name: "empty list", max: 1, value: stringList(t)},
		// A single element carries no separator at all.
		{name: "one element at the cap", max: 10, value: stringList(t, "1234567890")},
		{name: "one element over the cap", max: 10, value: stringList(t, "12345678901"), wantError: true},
		// 5 + 2 + 5. Dropping the separator lands on exactly 10 and passes, so
		// this pair is what actually distinguishes the arithmetic.
		{name: "the separator tips two elements over", max: 10, value: stringList(t, "12345", "12345"), wantError: true},
		{name: "two elements that fit with their separator", max: 10, value: stringList(t, "1234", "1234")},
		// Runes, not bytes: the server caps a Ruby String#length, so an accented
		// character weighs one, not the two it takes on the wire.
		{name: "multi-byte characters count once each", max: 12, value: stringList(t, strings.Repeat("é", 5), strings.Repeat("é", 5))},
		{name: "multi-byte characters over the cap", max: 11, value: stringList(t, strings.Repeat("é", 5), strings.Repeat("é", 5)), wantError: true},
		{
			// An unresolved element makes the total unknowable. Erroring here
			// would reject a config that is very likely fine once it resolves.
			name:  "an unknown element abandons the check",
			max:   1,
			value: mixedStringList(t, types.StringValue("12345"), types.StringUnknown(), types.StringValue("12345")),
		},
		{
			// A null contributes nothing, but the elements after it still do: a
			// return here rather than a continue would silently stop counting.
			name:      "a null element is skipped without stopping the count",
			max:       10,
			value:     mixedStringList(t, types.StringNull(), types.StringValue("12345678901")),
			wantError: true,
		},
		{
			// An interior null carries no characters but still carries the CRLF
			// that precedes it, exactly as ["a", nil, "b"].join("\r\n") does
			// server-side: 1 + 2 + 2 + 1 = 6. Skipping its separator too would
			// score 4 and let this list through.
			name:      "an interior null still carries its separator",
			max:       5,
			value:     mixedStringList(t, types.StringValue("a"), types.StringNull(), types.StringValue("b")),
			wantError: true,
		},
		{
			name:  "the same list fits when the cap allows for the separators",
			max:   6,
			value: mixedStringList(t, types.StringValue("a"), types.StringNull(), types.StringValue("b")),
		},
		{
			// Defensive. Every wiring today is a StringType list, but a element
			// of another type has to abandon the check rather than panic.
			name: "a non-string element abandons the check", max: 1, value: int64List(t, 1, 2, 3),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateList(t, []validator.List{CRLFJoinedLengthAtMost(tt.max)},
				"dns_expected_records", tt.value)
			if diags.HasError() != tt.wantError {
				t.Errorf("expected error=%v, got %v", tt.wantError, diags.Errors())
			}
		})
	}
}

// The number in the message is the joined total, not the payload the reader can
// add up from their own HCL — quoting the payload back would send them looking
// for a limit their records appear to be under.
func TestCRLFJoinedLengthAtMost_Diagnostic(t *testing.T) {
	diags := validateList(t, []validator.List{CRLFJoinedLengthAtMost(10)},
		"dns_expected_records", stringList(t, "12345", "12345"))

	errs := diags.Errors()
	if len(errs) != 1 {
		t.Fatalf("expected exactly one error, got %v", errs)
	}
	if got := errs[0].Summary(); got != "List Too Long" {
		t.Errorf("unexpected summary: %q", got)
	}
	if got := errs[0].Detail(); !strings.Contains(got, "total 12 characters") {
		t.Errorf("detail %q should report the 12 character joined total, not the 10 of payload", got)
	}

	want := "must be at most 8192 characters once joined with CRLF line endings"
	v := CRLFJoinedLengthAtMost(8192)
	if got := v.Description(context.Background()); got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
	if got := v.MarkdownDescription(context.Background()); got != want {
		t.Errorf("MarkdownDescription() = %q, want %q", got, want)
	}
}

// --- Create / Update against a stub API ---

func ptrValue(v tftypes.Value) *tftypes.Value { return &v }

func monitorJSON(overrides map[string]interface{}) map[string]interface{} {
	monitor := map[string]interface{}{
		"id": "mon-uuid", "name": "API Health", "protocol": "https", "status": "up",
		"url": "https://example.com/health", "interval_seconds": 300,
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
	}
	for k, v := range overrides {
		monitor[k] = v
	}
	return map[string]interface{}{"uptime_monitor": monitor}
}

func newMonitorResource(t *testing.T, handler http.HandlerFunc) *uptimeMonitorResource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &uptimeMonitorResource{client: client.NewClient(srv.URL, "test-api-key")}
}

func TestUptimeMonitorResource_Create(t *testing.T) {
	tests := []struct {
		name        string
		paused      tftypes.Value
		pauseStatus int
		wantPaths   []string
		wantStatus  string
		wantPaused  bool
		wantError   bool
	}{
		{
			name:       "paused is not requested when unset",
			paused:     tftypes.NewValue(tftypes.Bool, nil),
			wantPaths:  []string{"/api/v1/uptime_monitors"},
			wantStatus: "up",
		},
		{
			// An unknown paused (Optional+Computed with no prior state) must not
			// be read as "false" and must not trigger a pause either.
			name:       "unknown paused is deferred",
			paused:     tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
			wantPaths:  []string{"/api/v1/uptime_monitors"},
			wantStatus: "up",
		},
		{
			name:       "paused false does not call pause",
			paused:     tftypes.NewValue(tftypes.Bool, false),
			wantPaths:  []string{"/api/v1/uptime_monitors"},
			wantStatus: "up",
		},
		{
			// The status must come from the pause response, not be assumed.
			name:        "paused true pauses after creation",
			paused:      tftypes.NewValue(tftypes.Bool, true),
			pauseStatus: http.StatusOK,
			wantPaths:   []string{"/api/v1/uptime_monitors", "/api/v1/uptime_monitors/mon-uuid/pause"},
			wantStatus:  "paused",
			wantPaused:  true,
		},
		{
			name:        "a failed pause still records the created monitor",
			paused:      tftypes.NewValue(tftypes.Bool, true),
			pauseStatus: http.StatusInternalServerError,
			wantPaths:   []string{"/api/v1/uptime_monitors", "/api/v1/uptime_monitors/mon-uuid/pause"},
			wantError:   true,
		},
	}

	ctx := context.Background()
	s := uptimeMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			r := newMonitorResource(t, func(w http.ResponseWriter, req *http.Request) {
				paths = append(paths, req.URL.Path)
				if req.URL.Path == "/api/v1/uptime_monitors" {
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(monitorJSON(nil))
					return
				}
				if tt.pauseStatus != http.StatusOK {
					w.WriteHeader(tt.pauseStatus)
					json.NewEncoder(w).Encode(map[string]interface{}{"error": "pause failed"})
					return
				}
				json.NewEncoder(w).Encode(monitorJSON(map[string]interface{}{"status": "paused"}))
			})

			plan := nullObjectValue(t, objType, map[string]tftypes.Value{
				"name":     tftypes.NewValue(tftypes.String, "API Health"),
				"protocol": tftypes.NewValue(tftypes.String, "https"),
				"url":      tftypes.NewValue(tftypes.String, "https://example.com/health"),
				"paused":   tt.paused,
			})
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
			r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}}, resp)

			if resp.Diagnostics.HasError() != tt.wantError {
				t.Fatalf("expected error=%v, got %v", tt.wantError, resp.Diagnostics.Errors())
			}
			if len(paths) != len(tt.wantPaths) {
				t.Fatalf("expected requests %v, got %v", tt.wantPaths, paths)
			}
			for i := range paths {
				if paths[i] != tt.wantPaths[i] {
					t.Errorf("expected requests %v, got %v", tt.wantPaths, paths)
				}
			}
			if tt.wantError {
				// The monitor exists server-side, so state must be written even
				// though Create failed; otherwise the next apply leaks it.
				if resp.State.Raw.IsNull() {
					t.Error("expected the created monitor to be recorded in state despite the error")
				}
				return
			}

			var out uptimeMonitorModel
			if diags := resp.State.Get(ctx, &out); diags.HasError() {
				t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
			}
			if out.ID.ValueString() != "mon-uuid" {
				t.Errorf("expected id mon-uuid, got %q", out.ID.ValueString())
			}
			if out.Status.ValueString() != tt.wantStatus {
				t.Errorf("expected status %q, got %q", tt.wantStatus, out.Status.ValueString())
			}
			if out.Paused.ValueBool() != tt.wantPaused {
				t.Errorf("expected paused %v, got %v", tt.wantPaused, out.Paused.ValueBool())
			}
		})
	}
}

func TestUptimeMonitorResource_Update(t *testing.T) {
	stringList := func(values ...string) tftypes.Value {
		elems := make([]tftypes.Value, 0, len(values))
		for _, v := range values {
			elems = append(elems, tftypes.NewValue(tftypes.String, v))
		}
		return tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, elems)
	}

	tests := []struct {
		name        string
		planPaused  tftypes.Value
		startStatus string
		actionPath  string
		actionMon   map[string]interface{}
		wantStatus  string
		wantPaused  bool
		planDNS     *tftypes.Value
		wantDNS     string // "": no assertion, "null", "empty", or a value
		actionFails bool
		wantErr     string
	}{
		{
			// Resuming must adopt whatever the API reports; the old code hardcoded
			// "active", which is not even a status the API can return.
			name:        "unpausing adopts the status from the resume response",
			planPaused:  tftypes.NewValue(tftypes.Bool, false),
			startStatus: "paused",
			actionPath:  "/api/v1/uptime_monitors/mon-uuid/resume",
			actionMon:   map[string]interface{}{"status": "recovering", "protocol": "tcp"},
			wantStatus:  "recovering",
		},
		{
			name:        "pausing adopts the status from the pause response",
			planPaused:  tftypes.NewValue(tftypes.Bool, true),
			startStatus: "up",
			actionPath:  "/api/v1/uptime_monitors/mon-uuid/pause",
			actionMon:   map[string]interface{}{"status": "paused", "protocol": "tcp"},
			wantStatus:  "paused",
			wantPaused:  true,
		},
		{
			name:        "already paused needs no extra call",
			planPaused:  tftypes.NewValue(tftypes.Bool, true),
			startStatus: "paused",
			wantStatus:  "paused",
			wantPaused:  true,
		},
		{
			name:        "unknown paused is left alone",
			planPaused:  tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
			startStatus: "paused",
			wantStatus:  "paused",
			wantPaused:  true,
		},
		{
			// The monitor was already updated, so a failed pause has to surface
			// as an error rather than be swallowed into a wrong state.
			name:        "a failed pause is reported",
			planPaused:  tftypes.NewValue(tftypes.Bool, true),
			startStatus: "up",
			actionPath:  "/api/v1/uptime_monitors/mon-uuid/pause",
			actionFails: true,
			wantErr:     "Error pausing uptime monitor",
		},
		{
			name:        "a failed resume is reported",
			planPaused:  tftypes.NewValue(tftypes.Bool, false),
			startStatus: "paused",
			actionPath:  "/api/v1/uptime_monitors/mon-uuid/resume",
			actionFails: true,
			wantErr:     "Error resuming uptime monitor",
		},
		{
			// The whole point of the *[]string field: an empty plan list has to
			// travel as [], which is the only way to clear a pinned expectation.
			// Omitting the key would leave the stored records in place.
			name:        "an empty dns_expected_records is sent as []",
			planPaused:  tftypes.NewValue(tftypes.Bool, nil),
			startStatus: "up",
			planDNS:     ptrValue(stringList()),
			wantDNS:     "empty",
			wantStatus:  "up",
		},
		{
			name:        "records are sent as given",
			planPaused:  tftypes.NewValue(tftypes.Bool, nil),
			startStatus: "up",
			planDNS:     ptrValue(stringList("1.2.3.4", "5.6.7.8")),
			wantDNS:     "1.2.3.4",
			wantStatus:  "up",
		},
		{
			// A null plan value means "clear", and for this key the clear is an
			// EMPTY ARRAY, not a null: strong params drops a null sent to a
			// collection key, so the null the provider used to send was accepted
			// and silently ignored, leaving the stored records in place.
			name:        "a null dns_expected_records sends an empty array, not a null",
			planPaused:  tftypes.NewValue(tftypes.Bool, nil),
			startStatus: "up",
			wantDNS:     "empty",
			wantStatus:  "up",
		},
	}

	ctx := context.Background()
	s := uptimeMonitorSchema(t)
	objType := s.Type().TerraformType(ctx)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patchBody map[string]interface{}
			var actionCalls int
			r := newMonitorResource(t, func(w http.ResponseWriter, req *http.Request) {
				switch {
				case req.Method == "GET":
					w.Header().Set("ETag", `"etag-1"`)
					json.NewEncoder(w).Encode(monitorJSON(map[string]interface{}{"status": tt.startStatus}))
				case req.Method == "PATCH":
					json.NewDecoder(req.Body).Decode(&patchBody)
					json.NewEncoder(w).Encode(monitorJSON(map[string]interface{}{
						"status": tt.startStatus, "protocol": "tcp",
						"hostname": "db.example.com", "port": 5432,
					}))
				default:
					actionCalls++
					if req.URL.Path != tt.actionPath {
						t.Errorf("expected %s, got %s", tt.actionPath, req.URL.Path)
					}
					if tt.actionFails {
						w.WriteHeader(http.StatusInternalServerError)
						json.NewEncoder(w).Encode(map[string]interface{}{"error": "boom"})
						return
					}
					json.NewEncoder(w).Encode(monitorJSON(tt.actionMon))
				}
			})

			// https -> tcp in place: the protocol has to travel in the PATCH body.
			overrides := map[string]tftypes.Value{
				"id":             tftypes.NewValue(tftypes.String, "mon-uuid"),
				"name":           tftypes.NewValue(tftypes.String, "API Health"),
				"protocol":       tftypes.NewValue(tftypes.String, "tcp"),
				"hostname":       tftypes.NewValue(tftypes.String, "db.example.com"),
				"port":           tftypes.NewValue(tftypes.Number, 5432),
				"keyword":        tftypes.NewValue(tftypes.String, "OK"),
				"recovery_count": tftypes.NewValue(tftypes.Number, 2),
				"paused":         tt.planPaused,
			}
			if tt.planDNS != nil {
				overrides["dns_expected_records"] = *tt.planDNS
			}
			plan := nullObjectValue(t, objType, overrides)
			state := nullObjectValue(t, objType, map[string]tftypes.Value{
				"id":       tftypes.NewValue(tftypes.String, "mon-uuid"),
				"protocol": tftypes.NewValue(tftypes.String, "https"),
			})
			resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
			r.Update(ctx, resource.UpdateRequest{
				Plan:  tfsdk.Plan{Schema: s, Raw: plan},
				State: tfsdk.State{Schema: s, Raw: state},
			}, resp)

			if tt.wantErr != "" {
				if !resp.Diagnostics.HasError() {
					t.Fatal("expected an error diagnostic")
				}
				if got := resp.Diagnostics.Errors()[0].Summary(); got != tt.wantErr {
					t.Errorf("expected summary %q, got %q", tt.wantErr, got)
				}
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
			}
			sent, ok := patchBody["uptime_monitor"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected a PATCH body, got %v", patchBody)
			}
			if sent["protocol"] != "tcp" {
				t.Errorf("expected protocol tcp in the update body, got %v", sent["protocol"])
			}
			// Set optional fields still travel as values, not as clearing nulls.
			if sent["keyword"] != "OK" {
				t.Errorf("expected keyword OK in the update body, got %v", sent["keyword"])
			}
			if sent["recovery_count"] != float64(2) {
				t.Errorf("expected recovery_count 2 in the update body, got %v", sent["recovery_count"])
			}
			// The other collection key follows the same rule: an unset
			// custom_headers clears with {}, never with a null the server drops.
			switch headers, present := sent["custom_headers"]; {
			case !present:
				t.Error("expected custom_headers in the update body, got nothing")
			case headers == nil:
				t.Error("expected custom_headers to be {}, got null — strong params drops a null on a collection key")
			default:
				if m, ok := headers.(map[string]interface{}); !ok || len(m) != 0 {
					t.Errorf("expected custom_headers to be {}, got %v", headers)
				}
			}
			switch records, present := sent["dns_expected_records"]; tt.wantDNS {
			case "":
			case "null":
				// The SCALAR protocol-scoped fields clear with an explicit null.
				// dns_expected_records is not one of them — see "empty".
				if !present || records != nil {
					t.Errorf("expected dns_expected_records to be null, got %v (present=%v)", records, present)
				}
			case "empty":
				list, ok := records.([]interface{})
				if !present || !ok || len(list) != 0 {
					t.Errorf("expected dns_expected_records to be [], got %v (present=%v)", records, present)
				}
			default:
				list, ok := records.([]interface{})
				if !ok || len(list) == 0 || list[0] != tt.wantDNS {
					t.Errorf("expected dns_expected_records starting with %q, got %v", tt.wantDNS, records)
				}
			}

			wantCalls := 0
			if tt.actionPath != "" {
				wantCalls = 1
			}
			if actionCalls != wantCalls {
				t.Errorf("expected %d pause/resume calls, got %d", wantCalls, actionCalls)
			}

			var out uptimeMonitorModel
			if diags := resp.State.Get(ctx, &out); diags.HasError() {
				t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
			}
			if out.Status.ValueString() != tt.wantStatus {
				t.Errorf("expected status %q, got %q", tt.wantStatus, out.Status.ValueString())
			}
			if out.Paused.ValueBool() != tt.wantPaused {
				t.Errorf("expected paused %v, got %v", tt.wantPaused, out.Paused.ValueBool())
			}
			if out.Protocol.ValueString() != "tcp" {
				t.Errorf("expected the protocol change to land in state, got %q", out.Protocol.ValueString())
			}
		})
	}
}
