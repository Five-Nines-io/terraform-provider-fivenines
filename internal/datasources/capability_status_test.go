package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestInstanceCapabilityStatusDataSource_Read(t *testing.T) {
	tests := []struct {
		name         string
		httpStatus   int
		body         map[string]interface{}
		wantReported bool
		wantCaps     map[string]bool
		wantPending  []string
		wantReasons  map[string]string
		wantNullTime bool
		wantErr      string
	}{
		{
			name: "a reporting agent",
			body: map[string]interface{}{
				"capabilities": map[string]interface{}{"docker": true, "zfs": false},
				"pending":      []interface{}{"zfs"},
				"reasons":      map[string]interface{}{"zfs": "zpool not found in PATH"},
				"updated_at":   "2026-02-01T00:00:00Z",
			},
			wantReported: true,
			wantCaps:     map[string]bool{"docker": true, "zfs": false},
			wantPending:  []string{"zfs"},
			wantReasons:  map[string]string{"zfs": "zpool not found in PATH"},
		},
		{
			// THE HONESTY RULE: an empty map is "not reported", never "nothing
			// is supported" -- and the timestamp is fresh, because the server
			// stamps it on every check-in whether or not the agent sent a
			// capability block. `reported` is the only thing that says so.
			name: "an agent that does not speak the capability protocol",
			body: map[string]interface{}{
				"capabilities": map[string]interface{}{},
				"pending":      []interface{}{},
				"reasons":      map[string]interface{}{},
				"updated_at":   "2026-02-01T00:00:00Z",
			},
			wantReported: false,
			wantCaps:     map[string]bool{},
			wantPending:  []string{},
			wantReasons:  map[string]string{},
		},
		{
			// A host that has never posted anything: null timestamp, and the
			// three collections absent from the payload entirely.
			name:         "a host that has never checked in",
			body:         map[string]interface{}{"capabilities": nil, "pending": nil, "reasons": nil, "updated_at": nil},
			wantReported: false,
			wantCaps:     map[string]bool{},
			wantPending:  []string{},
			wantReasons:  map[string]string{},
			wantNullTime: true,
		},
		{
			name:       "a 404 surfaces as a diagnostic",
			httpStatus: http.StatusNotFound,
			body:       map[string]interface{}{"error": "Not Found"},
			wantErr:    "Error reading instance capability status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				if tt.httpStatus != 0 {
					w.WriteHeader(tt.httpStatus)
					json.NewEncoder(w).Encode(tt.body)
					return
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"capability_status": tt.body})
			})

			state, resp := readDataSource(t, &instanceCapabilityStatusDataSource{client: c}, map[string]tftypes.Value{
				"instance_id": tftypes.NewValue(tftypes.String, "host-uuid"),
			})
			if gotPath != "/api/v1/instances/host-uuid/capability_status" {
				t.Errorf("unexpected request path: %s", gotPath)
			}
			if tt.wantErr != "" {
				if !resp.Diagnostics.HasError() {
					t.Fatal("expected an error diagnostic")
				}
				if got := resp.Diagnostics.Errors()[0].Summary(); got != tt.wantErr {
					t.Errorf("expected summary %q, got %q", tt.wantErr, got)
				}
				if !state.Raw.IsNull() {
					t.Errorf("expected state to be left null, got %v", state.Raw)
				}
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
			}

			var out instanceCapabilityStatusModel
			if diags := state.Get(context.Background(), &out); diags.HasError() {
				t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
			}
			if out.InstanceID.ValueString() != "host-uuid" {
				t.Errorf("expected the configured instance_id to be echoed, got %q", out.InstanceID.ValueString())
			}
			if out.Reported.ValueBool() != tt.wantReported {
				t.Errorf("expected reported=%v, got %v", tt.wantReported, out.Reported.ValueBool())
			}
			if out.UpdatedAt.IsNull() != tt.wantNullTime {
				t.Errorf("expected updated_at null=%v, got %v", tt.wantNullTime, out.UpdatedAt)
			}

			// The three collections must never come back null: a null map or
			// list breaks lookup()/length()/for_each, and "the agent reported
			// none" is the normal case `reported` already covers.
			if out.Capabilities.IsNull() || out.Reasons.IsNull() || out.Pending.IsNull() {
				t.Fatalf("expected non-null collections, got %+v", out)
			}

			caps := map[string]bool{}
			for name, v := range out.Capabilities.Elements() {
				b, ok := v.(types.Bool)
				if !ok {
					t.Fatalf("expected a bool element, got %T", v)
				}
				caps[name] = b.ValueBool()
			}
			if len(caps) != len(tt.wantCaps) {
				t.Errorf("expected %d capabilities, got %d", len(tt.wantCaps), len(caps))
			}
			for name, want := range tt.wantCaps {
				if caps[name] != want {
					t.Errorf("expected capability %s=%v, got %v", name, want, caps[name])
				}
			}

			reasons := map[string]string{}
			for name, v := range out.Reasons.Elements() {
				s, ok := v.(types.String)
				if !ok {
					t.Fatalf("expected a string element, got %T", v)
				}
				reasons[name] = s.ValueString()
			}
			for name, want := range tt.wantReasons {
				if reasons[name] != want {
					t.Errorf("expected reason %s=%q, got %q", name, want, reasons[name])
				}
			}

			// The ELEMENTS, not just the count: `pending` carries capability
			// NAMES in the order the agent sent them. Populating it from the
			// keys of `reasons` (also one element, also "zfs") or emitting the
			// reason strings instead would pass a length-only assertion.
			pending := make([]string, 0, len(out.Pending.Elements()))
			for _, v := range out.Pending.Elements() {
				s, ok := v.(types.String)
				if !ok {
					t.Fatalf("expected a string element, got %T", v)
				}
				pending = append(pending, s.ValueString())
			}
			if !reflect.DeepEqual(pending, tt.wantPending) {
				t.Errorf("expected pending %v, got %v", tt.wantPending, pending)
			}
		})
	}
}

// --- status_page_subscribers ---

func TestStatusPageSubscribersDataSource_Read(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subscribers": []interface{}{
				map[string]interface{}{
					"id": 42, "email": "ops@customer.example", "status": "confirmed",
					"confirmed_at": "2026-01-02T00:00:00Z",
					"created_at":   "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				},
				map[string]interface{}{
					"id": 43, "email": "pending@customer.example", "status": "pending",
					"confirmed_at": nil,
					"created_at":   "2026-01-03T00:00:00Z", "updated_at": "2026-01-03T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &statusPageSubscribersDataSource{client: c}, map[string]tftypes.Value{
		"status_page_id": tftypes.NewValue(tftypes.Number, 7),
		"query":          tftypes.NewValue(tftypes.String, "customer"),
		"status":         tftypes.NewValue(tftypes.String, "confirmed"),
		"updated_since":  tftypes.NewValue(tftypes.String, "2026-01-01T00:00:00Z"),
		"order":          tftypes.NewValue(tftypes.String, "email"),
		"direction":      tftypes.NewValue(tftypes.String, "asc"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if gotPath != "/api/v1/status_pages/7/subscribers" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	for key, want := range map[string]string{
		"q": "customer", "status": "confirmed", "updated_since": "2026-01-01T00:00:00Z",
		"order": "email", "direction": "asc",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q, got %q", key, want, got)
		}
	}

	var out statusPageSubscribersModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.Subscribers) != 2 {
		t.Fatalf("expected 2 subscribers, got %d", len(out.Subscribers))
	}
	if out.Subscribers[0].Email.ValueString() != "ops@customer.example" {
		t.Errorf("unexpected first subscriber: %+v", out.Subscribers[0])
	}
	// A pending address has no confirmation timestamp; null, never "".
	if !out.Subscribers[1].ConfirmedAt.IsNull() {
		t.Errorf("expected a null confirmed_at while pending, got %v", out.Subscribers[1].ConfirmedAt)
	}
	if out.Subscribers[1].Status.ValueString() != "pending" {
		t.Errorf("unexpected status: %v", out.Subscribers[1].Status)
	}
}

// A 403 here means the token lacks the `status_pages: update` permission that
// reading PII requires. It must never read as "nobody is subscribed".
func TestStatusPageSubscribersDataSource_Read_Forbidden(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Forbidden"})
	})

	state, resp := readDataSource(t, &statusPageSubscribersDataSource{client: c}, map[string]tftypes.Value{
		"status_page_id": tftypes.NewValue(tftypes.Number, 7),
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic for a 403, not an empty list")
	}
	got := resp.Diagnostics.Errors()[0].Summary()
	if got != "Reading status page subscribers requires the status_pages update permission" {
		t.Errorf("expected the permission-specific diagnostic, got %q", got)
	}
	if !state.Raw.IsNull() {
		t.Errorf("expected state to be left null, got %v", state.Raw)
	}
}

func TestStatusPageSubscribersDataSource_Read_NoFiltersAndNoResults(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subscribers": []interface{}{},
			"meta":        map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &statusPageSubscribersDataSource{client: c}, map[string]tftypes.Value{
		"status_page_id": tftypes.NewValue(tftypes.Number, 7),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	for _, key := range []string{"q", "status", "updated_since", "order", "direction"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %s to be omitted when unset", key)
		}
	}

	var out statusPageSubscribersModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if out.Subscribers == nil || len(out.Subscribers) != 0 {
		t.Errorf("expected an empty non-nil slice, got %v", out.Subscribers)
	}
}
