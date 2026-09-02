package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func networkDeviceSchema(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	(&networkDeviceResource{}).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func newDeviceResource(t *testing.T, handler http.HandlerFunc) *networkDeviceResource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &networkDeviceResource{client: client.NewClient(srv.URL, "test-api-key")}
}

// deviceJSON is the API's device body, with the reachability fields a freshly
// created device reports before its first poll.
func deviceJSON(overrides map[string]interface{}) map[string]interface{} {
	device := map[string]interface{}{
		"id": "dev-uuid", "name": "Core Switch", "ip_address": "192.0.2.1",
		"device_type": "switch", "polling_interval": 60, "snmp_version": "v2c",
		"snmp_security_level": "no_auth_no_priv", "snmp_auth_protocol": "md5",
		"snmp_priv_protocol": "des", "maintenance_mode": false,
		"status": "unknown", "consecutive_failures": 0,
		"last_error_type": nil, "last_error_message": nil, "last_polled_at": nil,
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
	}
	for k, v := range overrides {
		device[k] = v
	}
	return map[string]interface{}{"network_device": device}
}

// The API requires only name and ip_address (NetworkDeviceInput in the public
// swagger), so snmp_version is Optional+Computed rather than Required: a device
// may be created without it and take whatever the API reports back.
func TestNetworkDeviceSchema_SNMPVersionIsOptional(t *testing.T) {
	attr, ok := networkDeviceSchema(t).Attributes["snmp_version"]
	if !ok {
		t.Fatal("snmp_version is missing from the schema")
	}
	if attr.IsRequired() {
		t.Error("snmp_version must not be Required: the API requires only name and ip_address")
	}
	if !attr.IsOptional() {
		t.Error("snmp_version must be Optional so it can be omitted")
	}
	if !attr.IsComputed() {
		t.Error("snmp_version must be Computed so the API's value can settle into state")
	}
}

// An unconfigured snmp_version plans as unknown; sending it as "" would fail the
// API's enum, so the create body must omit the key entirely.
func TestNetworkDeviceCreate_OmitsUnknownSNMPVersion(t *testing.T) {
	ctx := context.Background()
	s := networkDeviceSchema(t)
	objType := s.Type().TerraformType(ctx)

	var body map[string]interface{}
	r := newDeviceResource(t, func(w http.ResponseWriter, req *http.Request) {
		json.NewDecoder(req.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(deviceJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":             tftypes.NewValue(tftypes.String, "Core Switch"),
		"ip_address":       tftypes.NewValue(tftypes.String, "192.0.2.1"),
		"snmp_version":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"maintenance_mode": tftypes.NewValue(tftypes.Bool, false),
	})
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent, ok := body["network_device"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a create body, got %v", body)
	}
	if v, present := sent["snmp_version"]; present {
		t.Errorf("expected snmp_version to be omitted when unknown, got %v", v)
	}

	// And the unknown must not survive into state.
	var got networkDeviceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	if got.SNMPVersion.IsUnknown() {
		t.Error("snmp_version left unknown in state after create")
	}
	if got.SNMPVersion.ValueString() != "v2c" {
		t.Errorf("expected the API's v2c to settle into state, got %q", got.SNMPVersion.ValueString())
	}
}

// stringPtr maps an unknown plan value to nil, and nil + omitempty omits the
// key. A hand-rolled guard would hand over a pointer to "" instead, which
// omitempty does NOT drop, putting `snmp_version: ""` on the wire against an
// enum that accepts only v2c and v3.
func TestNetworkDeviceUpdate_OmitsUnknownSNMPVersion(t *testing.T) {
	ctx := context.Background()
	s := networkDeviceSchema(t)
	objType := s.Type().TerraformType(ctx)

	var body map[string]interface{}
	r := newDeviceResource(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			w.Header().Set("ETag", `"dev-etag"`)
			json.NewEncoder(w).Encode(deviceJSON(nil))
			return
		}
		json.NewDecoder(req.Body).Decode(&body)
		json.NewEncoder(w).Encode(deviceJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, "dev-uuid"),
		"name":             tftypes.NewValue(tftypes.String, "Core Switch"),
		"ip_address":       tftypes.NewValue(tftypes.String, "192.0.2.1"),
		"snmp_version":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"maintenance_mode": tftypes.NewValue(tftypes.Bool, false),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, "dev-uuid"),
		"maintenance_mode": tftypes.NewValue(tftypes.Bool, false),
	})
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: s, Raw: plan},
		State: tfsdk.State{Schema: s, Raw: state},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent, ok := body["network_device"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a PATCH body, got %v", body)
	}
	if v, present := sent["snmp_version"]; present {
		t.Errorf("expected snmp_version omitted when unknown, got %#v", v)
	}
}

// enter_maintenance and exit_maintenance return the updated device, so the
// provider reads its final state out of that response instead of spending a
// follow-up GET on it. These assert the request sequence (no trailing GET) and
// that the recorded state is the one the maintenance call returned — ignoring
// the returned body would silently record the pre-maintenance device.
func TestNetworkDeviceCreate_MaintenanceResponseIsTheFinalState(t *testing.T) {
	ctx := context.Background()
	s := networkDeviceSchema(t)
	objType := s.Type().TerraformType(ctx)

	var paths []string
	r := newDeviceResource(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.Method+" "+req.URL.Path)
		if req.Method == http.MethodPost && req.URL.Path == "/api/v1/network_devices" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(deviceJSON(nil))
			return
		}
		// The maintenance response carries values found on the first poll, which
		// only reach state if the provider actually parses this body.
		json.NewEncoder(w).Encode(deviceJSON(map[string]interface{}{
			"maintenance_mode": true, "status": "unreachable",
			"consecutive_failures": 3, "last_error_type": "timeout",
			"last_error_message": "No SNMP response from 192.0.2.1:161",
			"vendor":             "Cisco",
		}))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":             tftypes.NewValue(tftypes.String, "Core Switch"),
		"ip_address":       tftypes.NewValue(tftypes.String, "192.0.2.1"),
		"snmp_community":   tftypes.NewValue(tftypes.String, "public"),
		"maintenance_mode": tftypes.NewValue(tftypes.Bool, true),
	})
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	want := []string{
		"POST /api/v1/network_devices",
		"POST /api/v1/network_devices/dev-uuid/enter_maintenance",
	}
	if len(paths) != len(want) {
		t.Fatalf("expected requests %v, got %v", want, paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("expected requests %v, got %v", want, paths)
		}
	}

	var got networkDeviceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	if !got.MaintenanceMode.ValueBool() {
		t.Error("expected maintenance_mode true from the enter_maintenance body")
	}
	if got.Status.ValueString() != "unreachable" {
		t.Errorf("expected status unreachable from the enter_maintenance body, got %q", got.Status.ValueString())
	}
	if got.ConsecutiveFailures.ValueInt64() != 3 {
		t.Errorf("expected consecutive_failures 3 from the enter_maintenance body, got %d", got.ConsecutiveFailures.ValueInt64())
	}
	if got.LastErrorType.ValueString() != "timeout" {
		t.Errorf("expected last_error_type timeout from the enter_maintenance body, got %q", got.LastErrorType.ValueString())
	}
}

func TestNetworkDeviceUpdate_MaintenanceResponseIsTheFinalState(t *testing.T) {
	tests := []struct {
		name        string
		planMode    bool
		currentMode bool
		wantAction  string
	}{
		{name: "entering maintenance", planMode: true, currentMode: false, wantAction: "enter_maintenance"},
		{name: "exiting maintenance", planMode: false, currentMode: true, wantAction: "exit_maintenance"},
	}

	ctx := context.Background()
	s := networkDeviceSchema(t)
	objType := s.Type().TerraformType(ctx)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			r := newDeviceResource(t, func(w http.ResponseWriter, req *http.Request) {
				paths = append(paths, req.Method+" "+req.URL.Path)
				switch {
				case req.Method == http.MethodGet:
					w.Header().Set("ETag", `"dev-etag"`)
					json.NewEncoder(w).Encode(deviceJSON(map[string]interface{}{
						"maintenance_mode": tt.currentMode,
					}))
				case req.Method == http.MethodPatch:
					// The PATCH response still reports the pre-transition mode:
					// only the maintenance call knows the final one.
					json.NewEncoder(w).Encode(deviceJSON(map[string]interface{}{
						"maintenance_mode": tt.currentMode, "status": "up",
					}))
				default:
					json.NewEncoder(w).Encode(deviceJSON(map[string]interface{}{
						"maintenance_mode": tt.planMode, "status": "unreachable",
						"consecutive_failures": 3, "last_error_type": "timeout",
					}))
				}
			})

			plan := nullObjectValue(t, objType, map[string]tftypes.Value{
				"id":               tftypes.NewValue(tftypes.String, "dev-uuid"),
				"name":             tftypes.NewValue(tftypes.String, "Core Switch"),
				"ip_address":       tftypes.NewValue(tftypes.String, "192.0.2.1"),
				"maintenance_mode": tftypes.NewValue(tftypes.Bool, tt.planMode),
			})
			state := nullObjectValue(t, objType, map[string]tftypes.Value{
				"id":               tftypes.NewValue(tftypes.String, "dev-uuid"),
				"maintenance_mode": tftypes.NewValue(tftypes.Bool, tt.currentMode),
			})
			resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
			r.Update(ctx, resource.UpdateRequest{
				Plan:  tfsdk.Plan{Schema: s, Raw: plan},
				State: tfsdk.State{Schema: s, Raw: state},
			}, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
			}
			// GET for the ETag, PATCH, then the maintenance transition. Nothing
			// after it: the transition's own response is the final state.
			want := []string{
				"GET /api/v1/network_devices/dev-uuid",
				"PATCH /api/v1/network_devices/dev-uuid",
				"POST /api/v1/network_devices/dev-uuid/" + tt.wantAction,
			}
			if len(paths) != len(want) {
				t.Fatalf("expected requests %v, got %v", want, paths)
			}
			for i := range want {
				if paths[i] != want[i] {
					t.Fatalf("expected requests %v, got %v", want, paths)
				}
			}

			var got networkDeviceModel
			if diags := resp.State.Get(ctx, &got); diags.HasError() {
				t.Fatalf("reading state: %v", diags.Errors())
			}
			if got.MaintenanceMode.ValueBool() != tt.planMode {
				t.Errorf("expected maintenance_mode %v from the %s body, got %v",
					tt.planMode, tt.wantAction, got.MaintenanceMode.ValueBool())
			}
			if got.Status.ValueString() != "unreachable" {
				t.Errorf("expected status from the %s body, got %q", tt.wantAction, got.Status.ValueString())
			}
			if got.ConsecutiveFailures.ValueInt64() != 3 {
				t.Errorf("expected consecutive_failures 3 from the %s body, got %d",
					tt.wantAction, got.ConsecutiveFailures.ValueInt64())
			}
		})
	}
}

// No maintenance transition means the PATCH response is already the final
// state, so the update must not spend a second GET re-reading it.
func TestNetworkDeviceUpdate_NoMaintenanceTransitionSkipsTheRefetch(t *testing.T) {
	ctx := context.Background()
	s := networkDeviceSchema(t)
	objType := s.Type().TerraformType(ctx)

	var paths []string
	r := newDeviceResource(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.Method+" "+req.URL.Path)
		if req.Method == http.MethodGet {
			w.Header().Set("ETag", `"dev-etag"`)
		}
		json.NewEncoder(w).Encode(deviceJSON(map[string]interface{}{
			"name": "Renamed Switch", "status": "up", "consecutive_failures": 0,
		}))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, "dev-uuid"),
		"name":             tftypes.NewValue(tftypes.String, "Renamed Switch"),
		"ip_address":       tftypes.NewValue(tftypes.String, "192.0.2.1"),
		"maintenance_mode": tftypes.NewValue(tftypes.Bool, false),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, "dev-uuid"),
		"maintenance_mode": tftypes.NewValue(tftypes.Bool, false),
	})
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: s, Raw: plan},
		State: tfsdk.State{Schema: s, Raw: state},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	want := []string{
		"GET /api/v1/network_devices/dev-uuid",
		"PATCH /api/v1/network_devices/dev-uuid",
	}
	if len(paths) != len(want) {
		t.Fatalf("expected requests %v, got %v", want, paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("expected requests %v, got %v", want, paths)
		}
	}

	var got networkDeviceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	if got.Name.ValueString() != "Renamed Switch" {
		t.Errorf("expected the PATCH response to become state, got name %q", got.Name.ValueString())
	}
}

// Destroying a device goes through the shared async-deletion poll: the API
// answers 202 and the provider polls GET until it 404s. The reachability work
// on this resource must compose with that, not bypass it.
func TestNetworkDeviceDelete_PollsUntilGone(t *testing.T) {
	ctx := context.Background()
	s := networkDeviceSchema(t)
	objType := s.Type().TerraformType(ctx)

	var paths []string
	gets := 0
	r := newDeviceResource(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.Method+" "+req.URL.Path)
		if req.Method == http.MethodDelete {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		gets++
		if gets < 2 {
			json.NewEncoder(w).Encode(deviceJSON(nil)) // still alive
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "dev-uuid"),
	})
	resp := &resource.DeleteResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: state}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if len(paths) < 2 || paths[0] != "DELETE /api/v1/network_devices/dev-uuid" {
		t.Fatalf("expected a DELETE followed by polling GETs, got %v", paths)
	}
	if gets < 2 {
		t.Errorf("expected the provider to poll past a still-alive device, got %d GETs (%v)", gets, paths)
	}
}
