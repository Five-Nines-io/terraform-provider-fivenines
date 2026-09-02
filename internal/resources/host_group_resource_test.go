package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func hostGroupSchema(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewHostGroupResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func newHostGroupResource(t *testing.T, handler http.HandlerFunc) *hostGroupResource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &hostGroupResource{client: client.NewClient(srv.URL, "test-api-key")}
}

func hostGroupJSON(overrides map[string]interface{}) map[string]interface{} {
	group := map[string]interface{}{
		"id": 7, "name": "Production", "position": 1,
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
	}
	for k, v := range overrides {
		group[k] = v
	}
	return map[string]interface{}{"host_group": group}
}

// --- mapHostGroupToState ---

func TestMapHostGroupToState(t *testing.T) {
	group := &client.HostGroup{
		ID:        7,
		Name:      "Production",
		Position:  2,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-02T00:00:00Z",
	}

	state := &hostGroupModel{}
	mapHostGroupToState(group, state)

	if state.ID.ValueInt64() != 7 {
		t.Errorf("expected ID 7, got %d", state.ID.ValueInt64())
	}
	if state.Name.ValueString() != "Production" {
		t.Errorf("expected name Production, got %s", state.Name.ValueString())
	}
	if state.Position.ValueInt64() != 2 {
		t.Errorf("expected position 2, got %d", state.Position.ValueInt64())
	}
	if state.CreatedAt.ValueString() != "2026-01-01T00:00:00Z" {
		t.Errorf("expected created_at 2026-01-01T00:00:00Z, got %s", state.CreatedAt.ValueString())
	}
	if state.UpdatedAt.ValueString() != "2026-01-02T00:00:00Z" {
		t.Errorf("expected updated_at 2026-01-02T00:00:00Z, got %s", state.UpdatedAt.ValueString())
	}
}

// Groups that were never explicitly positioned come back with position 0. That
// has to land in state as a known 0: mapping it to null would make an
// Optional+Computed attribute flip to null on every read and drift forever.
func TestMapHostGroupToState_UnpositionedGroup(t *testing.T) {
	group := &client.HostGroup{
		ID:        1,
		Name:      "Legacy",
		Position:  0,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	state := &hostGroupModel{}
	mapHostGroupToState(group, state)

	if state.Position.IsNull() {
		t.Fatal("expected position to be known")
	}
	if state.Position.ValueInt64() != 0 {
		t.Errorf("expected position 0, got %d", state.Position.ValueInt64())
	}
}

// --- unknownOnPositionChange ---

var hostGroupObjectType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{"position": tftypes.Number},
}

// hostGroupRaw builds the raw plan/state value the plan modifier inspects. A nil
// position stands for the whole object being absent (create or destroy).
func hostGroupRaw(present bool) tftypes.Value {
	if !present {
		return tftypes.NewValue(hostGroupObjectType, nil)
	}
	return tftypes.NewValue(hostGroupObjectType, map[string]tftypes.Value{
		"position": tftypes.NewValue(tftypes.Number, 1),
	})
}

// The two "stays known" cells are negative cases: a no-op body is a correct
// implementation of them in isolation, so deleting the function body does not
// fail them. They are not vacuous — they are what catches the modifier being
// over-applied (see the M5/M6 mutations in the PR body): marking position
// unknown on a no-op plan puts "known after apply" in front of the practitioner
// on every plan, and doing it on a destroy plan corrupts the destroy.
func TestUnknownOnPositionChange(t *testing.T) {
	tests := []struct {
		name         string
		planPresent  bool
		statePresent bool
		planValue    types.Int64
		stateValue   types.Int64
		wantUnknown  bool
	}{
		{
			name:         "create with an explicit position defers to the API",
			planPresent:  true,
			statePresent: false,
			planValue:    types.Int64Value(2),
			stateValue:   types.Int64Null(),
			wantUnknown:  true,
		},
		{
			name:         "update that moves the group defers to the API",
			planPresent:  true,
			statePresent: true,
			planValue:    types.Int64Value(3),
			stateValue:   types.Int64Value(1),
			wantUnknown:  true,
		},
		{
			name:         "unchanged position stays known",
			planPresent:  true,
			statePresent: true,
			planValue:    types.Int64Value(1),
			stateValue:   types.Int64Value(1),
			wantUnknown:  false,
		},
		{
			name:         "destroy plan is left alone",
			planPresent:  false,
			statePresent: true,
			planValue:    types.Int64Null(),
			stateValue:   types.Int64Value(1),
			wantUnknown:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := planmodifier.Int64Request{
				Plan:       tfsdk.Plan{Raw: hostGroupRaw(tt.planPresent)},
				State:      tfsdk.State{Raw: hostGroupRaw(tt.statePresent)},
				PlanValue:  tt.planValue,
				StateValue: tt.stateValue,
			}
			resp := &planmodifier.Int64Response{PlanValue: tt.planValue}

			unknownOnPositionChange{}.PlanModifyInt64(context.Background(), req, resp)

			if got := resp.PlanValue.IsUnknown(); got != tt.wantUnknown {
				t.Errorf("expected unknown=%v, got plan value %v", tt.wantUnknown, resp.PlanValue)
			}
		})
	}
}

// --- schema ---

func TestHostGroupSchema_PositionIsOptionalComputed(t *testing.T) {
	attr, ok := hostGroupSchema(t).Attributes["position"]
	if !ok {
		t.Fatal("position is missing from the schema")
	}
	if attr.IsRequired() {
		t.Error("position must not be Required: the API assigns one when it is omitted")
	}
	if !attr.IsOptional() {
		t.Error("position must be Optional so a practitioner can pin the ordering")
	}
	if !attr.IsComputed() {
		t.Error("position must be Computed so the API's value can settle into state")
	}
}

// An immutable creation timestamp must not plan as "(known after apply)" on
// every update.
func TestHostGroupSchema_CreatedAtUsesStateForUnknown(t *testing.T) {
	attr, ok := hostGroupSchema(t).Attributes["created_at"]
	if !ok {
		t.Fatal("created_at is missing from the schema")
	}
	stringAttr, ok := attr.(rschema.StringAttribute)
	if !ok {
		t.Fatalf("expected created_at to be a StringAttribute, got %T", attr)
	}
	if len(stringAttr.PlanModifiers) == 0 {
		t.Error("created_at needs UseStateForUnknown: a creation time never changes")
	}
}

// The API's limit is 50 CHARACTERS, and stringvalidator.LengthBetween counts
// bytes — upstream's own doc comment says to use UTF8LengthBetween for
// multi-byte characters. With the byte validator a 50-character accented name is
// rejected at plan time against a visibly 50-character string.
func TestHostGroupSchema_NameLimitCountsCharactersNotBytes(t *testing.T) {
	ctx := context.Background()
	attr, ok := hostGroupSchema(t).Attributes["name"]
	if !ok {
		t.Fatal("name is missing from the schema")
	}
	stringAttr, ok := attr.(rschema.StringAttribute)
	if !ok {
		t.Fatalf("expected name to be a StringAttribute, got %T", attr)
	}
	if len(stringAttr.Validators) == 0 {
		t.Fatal("name has no validators")
	}

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		// 50 two-byte characters: 50 characters, 100 bytes.
		{name: "50 accented characters are accepted", value: strings.Repeat("é", 50)},
		{name: "51 accented characters are rejected", value: strings.Repeat("é", 51), wantError: true},
		{name: "50 ascii characters are accepted", value: strings.Repeat("a", 50)},
		{name: "51 ascii characters are rejected", value: strings.Repeat("a", 51), wantError: true},
		{name: "empty is rejected", value: "", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var diags diag.Diagnostics
			for _, v := range stringAttr.Validators {
				resp := &validator.StringResponse{}
				v.ValidateString(ctx, validator.StringRequest{
					Path:        path.Root("name"),
					ConfigValue: types.StringValue(tt.value),
				}, resp)
				diags.Append(resp.Diagnostics...)
			}
			if got := diags.HasError(); got != tt.wantError {
				t.Errorf("%d-character name: error=%v, want %v (%v)",
					utf8.RuneCountInString(tt.value), got, tt.wantError, diags.Errors())
			}
		})
	}
}

// --- Create ---

// The regression this file exists for. unknownOnPositionChange deliberately
// plans position as unknown whenever it changes, which on create is always. A
// Create that read the position off the PLAN would therefore read unknown, send
// nothing, and silently drop a position the practitioner asked for — with every
// other test in the package still green. The value has to come off the CONFIG.
func TestHostGroupCreate_SendsConfiguredPositionDespiteUnknownPlan(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	var body map[string]interface{}
	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		json.NewDecoder(req.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(hostGroupJSON(map[string]interface{}{"position": 5}))
	})

	// Exactly what the framework hands Create once the plan modifier has run.
	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, 5),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent, ok := body["host_group"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a create body, got %v", body)
	}
	if sent["position"] != float64(5) {
		t.Errorf("create body must carry the configured position 5, got %v", sent["position"])
	}
}

// An unconfigured position must not be sent at all: the API reads an omitted
// position as "put this group on top", and there is no zero value that means
// the same thing.
func TestHostGroupCreate_OmitsUnconfiguredPosition(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	var body map[string]interface{}
	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		json.NewDecoder(req.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(hostGroupJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Production"),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent := body["host_group"].(map[string]interface{})
	if _, present := sent["position"]; present {
		t.Errorf("create body must omit an unconfigured position, got %v", sent["position"])
	}
}

// The whole reason position is planned as unknown: the API clamps a position
// beyond the number of existing groups, so the value that comes back is not the
// one that was asked for. State must record what the API actually did — with a
// known planned value this apply is the "inconsistent result after apply" error.
func TestHostGroupCreate_StoresTheAPIsClampedPosition(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusCreated)
		// Asked for 99, only one group exists, so the API clamps to 1.
		json.NewEncoder(w).Encode(hostGroupJSON(map[string]interface{}{"position": 1}))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, 99),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var state hostGroupModel
	resp.State.Get(ctx, &state)
	if state.Position.IsUnknown() {
		t.Fatal("position must be resolved in state, not left unknown")
	}
	if state.Position.ValueInt64() != 1 {
		t.Errorf("state must record the API's clamped position 1, got %d", state.Position.ValueInt64())
	}
}

// --- Read ---

// The API deletes a group the moment its last instance leaves it, so a 404 on
// read is an expected lifecycle event, not a failure. Read has to drop it from
// state so the next apply recreates it.
func TestHostGroupRead_RemovesVanishedGroup(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":       tftypes.NewValue(tftypes.Number, 7),
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, 1),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: state}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a 404 must not raise: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Error("expected the vanished group to be removed from state")
	}
}

// Any other error is a real failure and must surface rather than silently
// dropping a live group out of state.
func TestHostGroupRead_KeepsStateOnServerError(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":       tftypes.NewValue(tftypes.Number, 7),
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, 1),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: state}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a 500 must raise a diagnostic")
	}
	if resp.State.Raw.IsNull() {
		t.Error("a 500 must not remove the resource from state")
	}
}

// --- Update ---

func hostGroupUpdateRequest(t *testing.T, s rschema.Schema, objType tftypes.Type, configPosition *int64) resource.UpdateRequest {
	t.Helper()
	return hostGroupUpdateRequestWithPlan(t, s, objType, configPosition, tftypes.NewValue(tftypes.Number, tftypes.UnknownValue))
}

func hostGroupUpdateRequestWithPlan(t *testing.T, s rschema.Schema, objType tftypes.Type, configPosition *int64, planPosition tftypes.Value) resource.UpdateRequest {
	t.Helper()
	planAttrs := map[string]tftypes.Value{
		"id":       tftypes.NewValue(tftypes.Number, 7),
		"name":     tftypes.NewValue(tftypes.String, "Prod EU"),
		"position": planPosition,
	}
	stateAttrs := map[string]tftypes.Value{
		"id":       tftypes.NewValue(tftypes.Number, 7),
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, 1),
	}
	configAttrs := map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Prod EU"),
	}
	if configPosition != nil {
		configAttrs["position"] = tftypes.NewValue(tftypes.Number, *configPosition)
	}
	return resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: nullObjectValue(t, objType, planAttrs)},
		State:  tfsdk.State{Schema: s, Raw: nullObjectValue(t, objType, stateAttrs)},
		Config: tfsdk.Config{Schema: s, Raw: nullObjectValue(t, objType, configAttrs)},
	}
}

// Same trap as create, on the update path: the planned position is unknown
// whenever it changes, so a configured move has to be read off the config.
func TestHostGroupUpdate_SendsConfiguredPosition(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	var patchBody map[string]interface{}
	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch {
			json.NewDecoder(req.Body).Decode(&patchBody)
		}
		w.Header().Set("ETag", `"hg-etag"`)
		json.NewEncoder(w).Encode(hostGroupJSON(map[string]interface{}{"name": "Prod EU", "position": 3}))
	})

	want := int64(3)
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, hostGroupUpdateRequest(t, s, objType, &want), resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent := patchBody["host_group"].(map[string]interface{})
	if sent["position"] != float64(3) {
		t.Errorf("patch body must carry the configured position 3, got %v", sent["position"])
	}
}

// Dropping position from the configuration means "leave the group where it is",
// so the key must be omitted rather than sent as the state's value or as null.
func TestHostGroupUpdate_OmitsUnconfiguredPosition(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	var patchBody map[string]interface{}
	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch {
			json.NewDecoder(req.Body).Decode(&patchBody)
		}
		w.Header().Set("ETag", `"hg-etag"`)
		json.NewEncoder(w).Encode(hostGroupJSON(map[string]interface{}{"name": "Prod EU"}))
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, hostGroupUpdateRequest(t, s, objType, nil), resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent := patchBody["host_group"].(map[string]interface{})
	if _, present := sent["position"]; present {
		t.Errorf("patch body must omit an unconfigured position, got %v", sent["position"])
	}
}

// The ETag is harvested from a GET immediately before the PATCH, so a 412 means
// something moved in between. One retry re-reads and re-sends.
func TestHostGroupUpdate_RetriesOnETagMismatch(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	var patches int32
	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch {
			if atomic.AddInt32(&patches, 1) == 1 {
				w.WriteHeader(http.StatusPreconditionFailed)
				json.NewEncoder(w).Encode(map[string]string{"error": "Precondition Failed"})
				return
			}
		}
		w.Header().Set("ETag", `"hg-etag"`)
		json.NewEncoder(w).Encode(hostGroupJSON(map[string]interface{}{"name": "Prod EU"}))
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, hostGroupUpdateRequest(t, s, objType, nil), resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("the retry should have succeeded: %v", resp.Diagnostics.Errors())
	}
	if got := atomic.LoadInt32(&patches); got != 2 {
		t.Errorf("expected 2 PATCH attempts, got %d", got)
	}
}

// A group that was auto-deleted between refresh and apply cannot be updated.
// Terraform has no "turn this into a create" affordance, so the least confusing
// thing available is a diagnostic that names the auto-delete behaviour instead
// of a bare "API error 404".
func TestHostGroupUpdate_ExplainsVanishedGroup(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, hostGroupUpdateRequest(t, s, objType, nil), resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic when the group is gone")
	}
	summary := resp.Diagnostics.Errors()[0].Summary()
	if !strings.Contains(summary, "no longer exists") {
		t.Errorf("expected a diagnostic explaining the group is gone, got %q", summary)
	}
}

// Position is renumbered by the server whenever a NEIGHBOUR moves, so an update
// that does not touch position can still come back carrying a different one. The
// plan modifier cannot help here: the config does not move this group, so the
// planned position equals state and stays KNOWN. Writing the API's value into
// state against a known plan is exactly what Terraform rejects with "Provider
// produced inconsistent result after apply" — and two managed groups in one
// apply, where the other one moves, is enough to trigger it. Keep the planned
// value; the next refresh reports the server's truth and plans the correction.
func TestHostGroupUpdate_KeepsPlannedPositionWhenRenumberedConcurrently(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("ETag", `"hg-etag"`)
		// A sibling group moved into this slot mid-apply, so the group this
		// update touches came back renumbered from 3 to 4.
		json.NewEncoder(w).Encode(hostGroupJSON(map[string]interface{}{
			"name": "Prod EU", "position": 4,
		}))
	})

	req := hostGroupUpdateRequestWithPlan(t, s, objType, nil, tftypes.NewValue(tftypes.Number, 3))
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var got hostGroupModel
	resp.State.Get(ctx, &got)
	if got.Position.ValueInt64() != 3 {
		t.Errorf("state must keep the planned position 3 so the apply stays consistent, got %d",
			got.Position.ValueInt64())
	}
}

// The mirror of the case above: when the plan is unknown the API is the only
// source of truth, so its value must land in state rather than a stale one.
func TestHostGroupUpdate_TakesAPIPositionWhenPlanIsUnknown(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("ETag", `"hg-etag"`)
		json.NewEncoder(w).Encode(hostGroupJSON(map[string]interface{}{
			"name": "Prod EU", "position": 2,
		}))
	})

	want := int64(9)
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, hostGroupUpdateRequest(t, s, objType, &want), resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var got hostGroupModel
	resp.State.Get(ctx, &got)
	if got.Position.ValueInt64() != 2 {
		t.Errorf("an unknown plan must take the API's clamped position 2, got %d",
			got.Position.ValueInt64())
	}
}

// A configured position the group ALREADY occupies is not a move. Re-sending it
// still counts as one server-side, which renumbers every other group in the
// organisation — so a rename must not carry a position along with it.
func TestHostGroupUpdate_OmitsUnchangedPosition(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	var patchBody map[string]interface{}
	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch {
			json.NewDecoder(req.Body).Decode(&patchBody)
		}
		w.Header().Set("ETag", `"hg-etag"`)
		json.NewEncoder(w).Encode(hostGroupJSON(map[string]interface{}{"name": "Prod EU"}))
	})

	// The helper's state position is 1, so configuring 1 is a no-op move.
	same := int64(1)
	req := hostGroupUpdateRequestWithPlan(t, s, objType, &same, tftypes.NewValue(tftypes.Number, 1))
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	sent := patchBody["host_group"].(map[string]interface{})
	if _, present := sent["position"]; present {
		t.Errorf("a rename must not re-send the position it already has, got %v", sent["position"])
	}
}

// The group can also vanish between the ETag GET and the PATCH, which lands in a
// different branch from the GET's own 404.
func TestHostGroupUpdate_ExplainsGroupVanishingDuringPatch(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
			return
		}
		w.Header().Set("ETag", `"hg-etag"`)
		json.NewEncoder(w).Encode(hostGroupJSON(nil))
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, hostGroupUpdateRequest(t, s, objType, nil), resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic when the group vanishes mid-update")
	}
	if summary := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(summary, "no longer exists") {
		t.Errorf("a 404 on the PATCH must explain the group is gone, got %q", summary)
	}
}

// Exhausting the retries must say the update collided, not just "API error 412".
// Any group's move invalidates every other group's ETag, so a wide reordering
// applied in parallel reaches this path without anything being broken.
func TestHostGroupUpdate_ExplainsExhaustedETagRetries(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	var patches int32
	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch {
			atomic.AddInt32(&patches, 1)
			w.WriteHeader(http.StatusPreconditionFailed)
			json.NewEncoder(w).Encode(map[string]string{"error": "Precondition Failed"})
			return
		}
		w.Header().Set("ETag", `"hg-etag"`)
		json.NewEncoder(w).Encode(hostGroupJSON(nil))
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Update(ctx, hostGroupUpdateRequest(t, s, objType, nil), resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic once the retries are exhausted")
	}
	if got := atomic.LoadInt32(&patches); got != 3 {
		t.Errorf("expected 3 PATCH attempts before giving up, got %d", got)
	}
	if summary := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(summary, "modified concurrently") {
		t.Errorf("expected a concurrency diagnostic, got %q", summary)
	}
}

// --- Delete ---

// Delete races the same auto-deletion, so a 404 means the desired end state has
// already been reached.
func TestHostGroupDelete_ToleratesAlreadyDeleted(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":       tftypes.NewValue(tftypes.Number, 7),
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, 1),
	})
	resp := &resource.DeleteResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: state}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a 404 on delete must not raise: %v", resp.Diagnostics.Errors())
	}
}

func TestHostGroupDelete_SurfacesServerError(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newHostGroupResource(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
	})

	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":       tftypes.NewValue(tftypes.Number, 7),
		"name":     tftypes.NewValue(tftypes.String, "Production"),
		"position": tftypes.NewValue(tftypes.Number, 1),
	})
	resp := &resource.DeleteResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: state}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Error("a 500 on delete must raise")
	}
}

// --- ImportState ---

func TestHostGroupImportState(t *testing.T) {
	ctx := context.Background()
	s := hostGroupSchema(t)
	objType := s.Type().TerraformType(ctx)

	tests := []struct {
		name    string
		id      string
		wantErr bool
		wantID  int64
	}{
		{name: "numeric id", id: "7", wantID: 7},
		{name: "uuid is rejected", id: "3f1a-not-a-number", wantErr: true},
		{name: "empty is rejected", id: "", wantErr: true},
		{name: "zero is rejected", id: "0", wantErr: true},
		{name: "negative is rejected", id: "-1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &resource.ImportStateResponse{
				State: tfsdk.State{Schema: s, Raw: nullObjectValue(t, objType, nil)},
			}
			NewHostGroupResource().(*hostGroupResource).ImportState(
				ctx, resource.ImportStateRequest{ID: tt.id}, resp)

			if tt.wantErr {
				if !resp.Diagnostics.HasError() {
					t.Fatalf("expected %q to be rejected", tt.id)
				}
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
			}
			var state hostGroupModel
			resp.State.Get(ctx, &state)
			if state.ID.ValueInt64() != tt.wantID {
				t.Errorf("expected imported id %d, got %d", tt.wantID, state.ID.ValueInt64())
			}
		})
	}
}
