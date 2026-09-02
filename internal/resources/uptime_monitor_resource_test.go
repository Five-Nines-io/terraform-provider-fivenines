package resources

import (
	"context"
	"encoding/json"
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
			name:      "dns without record type",
			config:    map[string]tftypes.Value{"protocol": tftypes.NewValue(tftypes.String, "dns")},
			wantPaths: []string{"dns_record_type"},
		},
		{
			name: "dns with record type",
			config: map[string]tftypes.Value{
				"protocol":        tftypes.NewValue(tftypes.String, "dns"),
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
		{name: "empty string record", value: stringList(t, "")},
		{name: "record at the length cap", value: stringList(t, strings.Repeat("a", 2048))},
		{name: "record over the length cap", value: stringList(t, strings.Repeat("a", 2049)), wantError: true},
		{name: "over the size cap", value: stringList(t, tooMany...), wantError: true},
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
			name:        "a null dns_expected_records sends an explicit null",
			planPaused:  tftypes.NewValue(tftypes.Bool, nil),
			startStatus: "up",
			wantDNS:     "null",
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
			switch records, present := sent["dns_expected_records"]; tt.wantDNS {
			case "":
			case "null":
				// Unset protocol-scoped fields are sent as an explicit null so the
				// server clears them when the protocol changes.
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
