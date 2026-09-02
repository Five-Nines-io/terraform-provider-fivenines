package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func workflowSchema(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewWorkflowResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func newWorkflowResource(t *testing.T, handler http.HandlerFunc) *workflowResource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &workflowResource{client: client.NewClient(srv.URL, "test-api-key")}
}

func workflowJSON(overrides map[string]interface{}) map[string]interface{} {
	workflow := map[string]interface{}{
		"id": 42, "name": "CPU Alert", "status": "draft",
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
	}
	for k, v := range overrides {
		workflow[k] = v
	}
	return map[string]interface{}{"workflow": workflow}
}

// versionJSON is the body of the version-detail endpoint that Read follows to
// recover the published graph.
func versionJSON(graph, canvas map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"version": map[string]interface{}{
			"id": 10, "version_number": 3,
			"execution_graph": graph, "canvas_data": canvas,
			"created_at": "2026-01-01T00:00:00Z",
		},
	}
}

// --- Schema ---

// execution_graph_json has to stay a semantic JSON type. As a plain string it
// diffs on whitespace and key ordering, which cuts a new workflow version every
// time the server hands back a re-serialised graph.
func TestWorkflowSchema_ExecutionGraphIsSemanticJSON(t *testing.T) {
	s := workflowSchema(t)

	attribute, ok := s.Attributes["execution_graph_json"]
	if !ok {
		t.Fatal("execution_graph_json is missing from the schema")
	}
	if _, ok := attribute.GetType().(jsontypes.NormalizedType); !ok {
		t.Errorf("expected jsontypes.NormalizedType, got %T", attribute.GetType())
	}
}

// Computed is what lets Read populate the graph from the published version: on
// an Optional-only attribute a value the configuration never set is rejected as
// "was null, but now cty.StringVal(...)", which is why import used to come back
// with no graph at all.
func TestWorkflowSchema_ExecutionGraphIsOptionalAndComputed(t *testing.T) {
	attribute, ok := workflowSchema(t).Attributes["execution_graph_json"]
	if !ok {
		t.Fatal("execution_graph_json is missing from the schema")
	}
	if !attribute.IsOptional() {
		t.Error("execution_graph_json must stay Optional: the configuration owns the graph it publishes")
	}
	if !attribute.IsComputed() {
		t.Error("execution_graph_json must be Computed, otherwise Read cannot report the published graph")
	}
}

// The mirror image: the canvas must NOT be Computed. The API generates a layout
// for every graph, and storing that generated value would leave every republish
// owing it an update the practitioner never wrote.
func TestWorkflowSchema_CanvasDataIsOptionalOnlySemanticJSON(t *testing.T) {
	attribute, ok := workflowSchema(t).Attributes["canvas_data_json"]
	if !ok {
		t.Fatal("canvas_data_json is missing from the schema")
	}
	if _, ok := attribute.GetType().(jsontypes.NormalizedType); !ok {
		t.Errorf("expected jsontypes.NormalizedType, got %T", attribute.GetType())
	}
	if !attribute.IsOptional() {
		t.Error("canvas_data_json must be Optional")
	}
	if attribute.IsComputed() {
		t.Error("canvas_data_json must not be Computed: an API-generated layout does not belong in state")
	}
}

// template_slug is only read when the workflow is created, so a changed slug has
// to replace the resource rather than silently do nothing.
func TestWorkflowSchema_TemplateSlugRequiresReplace(t *testing.T) {
	attribute, ok := workflowSchema(t).Attributes["template_slug"]
	if !ok {
		t.Fatal("template_slug is missing from the schema")
	}
	stringAttr, ok := attribute.(rschema.StringAttribute)
	if !ok {
		t.Fatalf("expected a StringAttribute, got %T", attribute)
	}
	if attribute.IsComputed() {
		t.Error("template_slug must not be Computed: the API never echoes it back")
	}

	// Run the attribute's own modifiers over a changed slug and see whether any
	// of them asks for a replacement. Checking behaviour rather than identity
	// means swapping RequiresReplace for a hand-rolled equivalent still passes,
	// and dropping it entirely still fails.
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)
	stored := nullObjectValue(t, objType, map[string]tftypes.Value{
		"template_slug": tftypes.NewValue(tftypes.String, "old-slug"),
	})
	planned := nullObjectValue(t, objType, map[string]tftypes.Value{
		"template_slug": tftypes.NewValue(tftypes.String, "new-slug"),
	})

	var requiresReplace bool
	for _, m := range stringAttr.PlanModifiers {
		resp := &planmodifier.StringResponse{PlanValue: types.StringValue("new-slug")}
		m.PlanModifyString(ctx, planmodifier.StringRequest{
			State:       tfsdk.State{Schema: s, Raw: stored},
			Plan:        tfsdk.Plan{Schema: s, Raw: planned},
			StateValue:  types.StringValue("old-slug"),
			PlanValue:   types.StringValue("new-slug"),
			ConfigValue: types.StringValue("new-slug"),
		}, resp)
		if resp.RequiresReplace {
			requiresReplace = true
		}
	}
	if !requiresReplace {
		t.Error("changing template_slug must replace the workflow: it is only read at creation time")
	}
}

// --- ConfigValidators ---

// A template arrives with its own published graph and canvas. Accepting either
// alongside it would publish twice and leave whichever ran last in charge.
func TestWorkflowConfigValidators_TemplateConflictsWithGraphAndCanvas(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)
	r := &workflowResource{}

	cases := []struct {
		name      string
		overrides map[string]tftypes.Value
		wantError bool
	}{
		{
			name: "template with a graph",
			overrides: map[string]tftypes.Value{
				"name":                 tftypes.NewValue(tftypes.String, "wf"),
				"template_slug":        tftypes.NewValue(tftypes.String, "high-cpu"),
				"execution_graph_json": tftypes.NewValue(tftypes.String, `{"nodes":[]}`),
			},
			wantError: true,
		},
		{
			name: "template with a canvas",
			overrides: map[string]tftypes.Value{
				"name":             tftypes.NewValue(tftypes.String, "wf"),
				"template_slug":    tftypes.NewValue(tftypes.String, "high-cpu"),
				"canvas_data_json": tftypes.NewValue(tftypes.String, `{"viewport":{}}`),
			},
			wantError: true,
		},
		{
			name: "a graph and a canvas together are fine",
			overrides: map[string]tftypes.Value{
				"name":                 tftypes.NewValue(tftypes.String, "wf"),
				"execution_graph_json": tftypes.NewValue(tftypes.String, `{"nodes":[]}`),
				"canvas_data_json":     tftypes.NewValue(tftypes.String, `{"viewport":{}}`),
			},
		},
		{
			name: "a template on its own is fine",
			overrides: map[string]tftypes.Value{
				"name":          tftypes.NewValue(tftypes.String, "wf"),
				"template_slug": tftypes.NewValue(tftypes.String, "high-cpu"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := tfsdk.Config{Schema: s, Raw: nullObjectValue(t, objType, tc.overrides)}
			var diags diag.Diagnostics
			for _, v := range r.ConfigValidators(ctx) {
				resp := &resource.ValidateConfigResponse{}
				v.ValidateResource(ctx, resource.ValidateConfigRequest{Config: config}, resp)
				diags.Append(resp.Diagnostics...)
			}
			if got := diags.HasError(); got != tc.wantError {
				t.Errorf("HasError() = %v, want %v (%v)", got, tc.wantError, diags)
			}
		})
	}
}

// --- shouldPublishGraph ---

// The republish gate is the reason execution_graph_json is a semantic JSON type:
// a reformatted graph must not cut a new published version.
func TestShouldPublishGraph(t *testing.T) {
	compact := jsontypes.NewNormalizedValue(`{"nodes":[{"id":"a"}],"edges":[]}`)

	tests := []struct {
		name    string
		planned jsontypes.Normalized
		stored  jsontypes.Normalized
		want    bool
	}{
		{
			name:    "no graph in the config publishes nothing",
			planned: jsontypes.NewNormalizedNull(),
			stored:  compact,
		},
		{
			name:    "an unknown graph waits for apply",
			planned: jsontypes.NewNormalizedUnknown(),
			stored:  compact,
		},
		{
			name:    "a graph that was never stored always publishes",
			planned: compact,
			stored:  jsontypes.NewNormalizedNull(),
			want:    true,
		},
		{
			name:    "reformatting publishes nothing",
			planned: jsontypes.NewNormalizedValue("{\n  \"edges\": [],\n  \"nodes\": [{ \"id\": \"a\" }]\n}"),
			stored:  compact,
		},
		{
			name:    "a real change publishes",
			planned: jsontypes.NewNormalizedValue(`{"nodes":[{"id":"b"}],"edges":[]}`),
			stored:  compact,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diags := shouldPublishGraph(context.Background(), tt.planned, tt.stored)
			if diags.HasError() {
				t.Fatalf("diagnostics: %v", diags)
			}
			if got != tt.want {
				t.Errorf("shouldPublishGraph = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- publishGraph ---

// Invalid JSON never reaches the API: the attribute type rejects it at plan
// time, and publishGraph refuses it again before the version call.
func TestPublishGraph_RejectsInvalidJSON(t *testing.T) {
	r := &workflowResource{}
	err := r.publishGraph(context.Background(), 1,
		jsontypes.NewNormalizedValue("{not json"), jsontypes.NewNormalizedNull())
	if err == nil {
		t.Fatal("expected invalid JSON to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid execution_graph_json") {
		t.Errorf("expected the error to name the attribute, got: %v", err)
	}
}

func TestPublishGraph_RejectsInvalidCanvasJSON(t *testing.T) {
	r := &workflowResource{}
	err := r.publishGraph(context.Background(), 1,
		jsontypes.NewNormalizedValue(`{"nodes":[]}`), jsontypes.NewNormalizedValue("[1,2]"))
	if err == nil {
		t.Fatal("expected a non-object canvas to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid canvas_data_json") {
		t.Errorf("expected the error to name the attribute, got: %v", err)
	}
}

// An unset canvas has to be omitted, not nulled: omitting it is what asks the
// API to lay the graph out, and the endpoint answers an explicit null with 400.
func TestPublishGraph_OmitsUnsetCanvas(t *testing.T) {
	var versionBody map[string]interface{}
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/versions") {
			json.NewDecoder(req.Body).Decode(&versionBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version": map[string]interface{}{"id": 10, "version_number": 1, "created_at": "x"},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	err := r.publishGraph(context.Background(), 42,
		jsontypes.NewNormalizedValue(`{"nodes":[]}`), jsontypes.NewNormalizedNull())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := versionBody["canvas_data"]; ok {
		t.Errorf("canvas_data must be omitted when unset, got %v", versionBody["canvas_data"])
	}
}

func TestPublishGraph_SendsConfiguredCanvas(t *testing.T) {
	var versionBody map[string]interface{}
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/versions") {
			json.NewDecoder(req.Body).Decode(&versionBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version": map[string]interface{}{"id": 10, "version_number": 1, "created_at": "x"},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	err := r.publishGraph(context.Background(), 42,
		jsontypes.NewNormalizedValue(`{"nodes":[]}`),
		jsontypes.NewNormalizedValue(`{"viewport":{"zoom":1}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	canvas, ok := versionBody["canvas_data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a canvas_data object, got %v", versionBody["canvas_data"])
	}
	if canvas["viewport"] == nil {
		t.Errorf("expected the configured viewport to be sent, got %v", canvas)
	}
}

// --- marshalGraph ---

func TestMarshalGraph_NilIsNull(t *testing.T) {
	got, err := marshalGraph(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsNull() {
		t.Errorf("expected null for an absent graph, got %q", got.ValueString())
	}
}

func TestMarshalGraph_EncodesGraph(t *testing.T) {
	got, err := marshalGraph(map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IsNull() {
		t.Fatal("expected a value, got null")
	}
	equal, diags := got.StringSemanticEquals(context.Background(),
		jsontypes.NewNormalizedValue(`{"edges":[],"nodes":[]}`))
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if !equal {
		t.Errorf("expected the graph to round-trip, got %q", got.ValueString())
	}
}

// --- activationErrorDetail ---

func TestActivationErrorDetail_ExplainsMissingVersion(t *testing.T) {
	detail := activationErrorDetail(&client.APIError{StatusCode: 422, Message: "cannot activate"})

	if !strings.Contains(detail, "published version") {
		t.Errorf("expected the 422 to explain the missing published version, got %q", detail)
	}
	if !strings.Contains(detail, "cannot activate") {
		t.Errorf("expected the API's own message to survive, got %q", detail)
	}
}

func TestActivationErrorDetail_PassesOtherErrorsThrough(t *testing.T) {
	err := &client.APIError{StatusCode: 500, Message: "boom"}

	if detail := activationErrorDetail(err); detail != err.Error() {
		t.Errorf("expected the raw error, got %q", detail)
	}
}

// --- workflowUpdateInput ---

// Every field goes through the same stringPtr/int64Ptr call site, so an unknown
// or null plan value becomes a nil the json tag then interprets.
func TestWorkflowUpdateInput_NilsUnsetFields(t *testing.T) {
	input := workflowUpdateInput(workflowModel{
		Name:            types.StringValue("CPU Alert"),
		Description:     types.StringNull(),
		IntervalSeconds: types.Int64Unknown(),
	})

	if input.Name == nil || *input.Name != "CPU Alert" {
		t.Errorf("expected the configured name, got %v", input.Name)
	}
	if input.Description != nil {
		t.Errorf("a null description must reach the tag as nil, got %q", *input.Description)
	}
	if input.IntervalSeconds != nil {
		t.Errorf("an unknown interval must reach the tag as nil, got %d", *input.IntervalSeconds)
	}
}

// --- Create ---

// The bug this branch fixes: activation used to be nested inside the
// "a graph was configured" block, so `active = true` with no graph never called
// activate at all and the workflow stayed a draft with no error.
func TestWorkflowCreate_ActivatesWithoutAConfiguredGraph(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var activated bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == "POST" && req.URL.Path == "/api/v1/workflows":
			// The create response predates the activation, so it still says draft.
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(workflowJSON(nil))
		case req.URL.Path == "/api/v1/workflows/42/activate":
			activated = true
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(req.URL.Path, "/api/v1/workflows/42/versions/"):
			json.NewEncoder(w).Encode(versionJSON(map[string]interface{}{"nodes": []interface{}{}}, nil))
		default:
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{
				"status": "active", "published_version_id": 10,
			}))
		}
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, true),
		"execution_graph_json": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, true),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !activated {
		t.Error("active = true must call activate even when no graph was configured")
	}
	// Activation happens after the create call returns, so only a fresh read can
	// report it. Trusting the create response would leave state saying "draft".
	var got workflowModel
	resp.State.Get(ctx, &got)
	if got.Status.ValueString() != "active" {
		t.Errorf("state must come from the read that follows activation, got status %q",
			got.Status.ValueString())
	}
	if !got.Active.ValueBool() {
		t.Error("active must be true once the workflow reports status active")
	}
}

// A layout pinned with no graph to publish it against used to be dropped in
// silence. It has to be an error, the same way activating without a published
// version is.
func TestWorkflowCreate_RejectsACanvasWithNoGraph(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var called bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(workflowJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"canvas_data_json":     tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":1}}`),
		"execution_graph_json": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":             tftypes.NewValue(tftypes.String, "CPU Alert"),
		"canvas_data_json": tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":1}}`),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a canvas with no graph must be rejected, not silently dropped")
	}
	if called {
		t.Error("the check must fail before anything is created, or it orphans a workflow")
	}
}

// Same rule on update, where the graph could also have gone missing from state.
func TestWorkflowUpdate_RejectsACanvasWithNoGraph(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var called bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		called = true
		json.NewEncoder(w).Encode(workflowJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.Number, 42),
		"name":             tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":           tftypes.NewValue(tftypes.Bool, false),
		"canvas_data_json": tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":1}}`),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.Number, 42),
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, false),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		State:  tfsdk.State{Schema: s, Raw: state},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a canvas with no graph must be rejected on update too")
	}
	if called {
		t.Error("the check must fail before the patch goes out")
	}
}

// A 422 from activate is the API saying there is no published version. The
// diagnostic has to say what to do about it rather than surfacing the bare code.
func TestWorkflowCreate_ExplainsActivationWithoutAPublishedVersion(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == "POST" && req.URL.Path == "/api/v1/workflows":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(workflowJSON(nil))
		case req.URL.Path == "/api/v1/workflows/42/activate":
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"message": "no published version"})
		default:
			json.NewEncoder(w).Encode(workflowJSON(nil))
		}
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, true),
		"execution_graph_json": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, true),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a 422 from activate to fail the create")
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "published version") {
		t.Errorf("expected the diagnostic to explain the missing version, got %q", detail)
	}
}

// Optional+Computed with no schema default means the plan holds unknown when the
// configuration sets no graph. An unknown left in state fails the apply with
// "Provider produced inconsistent result after apply", so a workflow with
// nothing published has to settle on null.
func TestWorkflowCreate_SettlesUnknownGraphToNullWithoutAPublishedVersion(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "POST" {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(workflowJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "CPU Alert"),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var got workflowModel
	resp.State.Get(ctx, &got)
	if got.ExecutionGraphJSON.IsUnknown() {
		t.Fatal("execution_graph_json must never stay unknown in state")
	}
	if !got.ExecutionGraphJSON.IsNull() {
		t.Errorf("expected null with nothing published, got %q", got.ExecutionGraphJSON.ValueString())
	}
}

// A template publishes its own graph, so the same unknown has to settle on what
// the published version actually holds.
func TestWorkflowCreate_FromTemplateReadsBackThePublishedGraph(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var templateBody, patchBody map[string]interface{}
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == "POST" && req.URL.Path == "/api/v1/workflows/templates":
			json.NewDecoder(req.Body).Decode(&templateBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{
				"name": "Template Default Name", "published_version_id": 10,
			}))
		case req.Method == "PATCH":
			json.NewDecoder(req.Body).Decode(&patchBody)
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{
				"name": "My Workflow", "published_version_id": 10,
			}))
		case strings.HasPrefix(req.URL.Path, "/api/v1/workflows/42/versions/"):
			json.NewEncoder(w).Encode(versionJSON(
				map[string]interface{}{"nodes": []interface{}{map[string]interface{}{"id": "n1"}}},
				map[string]interface{}{"viewport": map[string]interface{}{"zoom": 1}},
			))
		default:
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{
				"name": "My Workflow", "published_version_id": 10,
			}))
		}
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "My Workflow"),
		"template_slug":        tftypes.NewValue(tftypes.String, "high-cpu"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":          tftypes.NewValue(tftypes.String, "My Workflow"),
		"template_slug": tftypes.NewValue(tftypes.String, "high-cpu"),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if templateBody["slug"] != "high-cpu" {
		t.Errorf("expected the template to be instantiated by slug, got %v", templateBody)
	}
	// The template names the workflow; the configured name has to win.
	sent, _ := patchBody["workflow"].(map[string]interface{})
	if sent == nil || sent["name"] != "My Workflow" {
		t.Errorf("expected the configured name to be patched over the template's, got %v", patchBody)
	}

	var got workflowModel
	resp.State.Get(ctx, &got)
	if got.ExecutionGraphJSON.IsNull() || got.ExecutionGraphJSON.IsUnknown() {
		t.Fatal("a templated workflow must report the graph its published version holds")
	}
	if !strings.Contains(got.ExecutionGraphJSON.ValueString(), "n1") {
		t.Errorf("expected the published graph, got %q", got.ExecutionGraphJSON.ValueString())
	}
	// The canvas was never configured, so the generated layout stays out of state.
	if !got.CanvasDataJSON.IsNull() {
		t.Errorf("an unconfigured canvas must stay null, got %q", got.CanvasDataJSON.ValueString())
	}
	if got.TemplateSlug.ValueString() != "high-cpu" {
		t.Errorf("template_slug must survive the create, got %q", got.TemplateSlug.ValueString())
	}
}

// A configured graph keeps the value the plan promised. Writing the API's
// re-serialisation back would break the plan contract the moment the server
// normalises anything.
func TestWorkflowCreate_KeepsTheConfiguredGraphVerbatim(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	configured := "{\n  \"edges\": [],\n  \"nodes\": []\n}"
	var versionCalls int
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == "POST" && req.URL.Path == "/api/v1/workflows":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(workflowJSON(nil))
		case strings.HasSuffix(req.URL.Path, "/versions"):
			versionCalls++
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version": map[string]interface{}{"id": 10, "version_number": 1, "created_at": "x"},
			})
		case strings.HasSuffix(req.URL.Path, "/publish"):
			w.WriteHeader(http.StatusOK)
		default:
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"published_version_id": 10}))
		}
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, configured),
	})
	config := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"execution_graph_json": tftypes.NewValue(tftypes.String, configured),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if versionCalls != 1 {
		t.Errorf("expected exactly one version to be published, got %d", versionCalls)
	}
	var got workflowModel
	resp.State.Get(ctx, &got)
	if got.ExecutionGraphJSON.ValueString() != configured {
		t.Errorf("the planned graph must survive apply byte for byte, got %q", got.ExecutionGraphJSON.ValueString())
	}
}

// --- Read ---

// The read-back that makes import work: the graph lives on the published
// version, and Read is the only thing that can put it in state.
func TestWorkflowRead_PopulatesTheGraphFromThePublishedVersion(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var versionPath string
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/versions/") {
			versionPath = req.URL.Path
			json.NewEncoder(w).Encode(versionJSON(
				map[string]interface{}{"nodes": []interface{}{map[string]interface{}{"id": "n1"}}},
				map[string]interface{}{"viewport": map[string]interface{}{"zoom": 1}},
			))
			return
		}
		json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"published_version_id": 10}))
	})

	// An import arrives with nothing but the ID in state.
	prior := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.Number, 42),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: prior}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if versionPath != "/api/v1/workflows/42/versions/10" {
		t.Errorf("expected the published version to be fetched, got %q", versionPath)
	}
	var got workflowModel
	resp.State.Get(ctx, &got)
	if got.ExecutionGraphJSON.IsNull() {
		t.Fatal("import must recover the published graph")
	}
	if !strings.Contains(got.ExecutionGraphJSON.ValueString(), "n1") {
		t.Errorf("expected the published graph, got %q", got.ExecutionGraphJSON.ValueString())
	}
}

// A graph edited in the UI has to come back as drift, not silence.
func TestWorkflowRead_ReportsAGraphChangedOutOfBand(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/versions/") {
			json.NewEncoder(w).Encode(versionJSON(
				map[string]interface{}{"nodes": []interface{}{map[string]interface{}{"id": "edited-in-ui"}}}, nil))
			return
		}
		json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"published_version_id": 10}))
	})

	prior := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":                   tftypes.NewValue(tftypes.Number, 42),
		"execution_graph_json": tftypes.NewValue(tftypes.String, `{"nodes":[]}`),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: prior}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var got workflowModel
	resp.State.Get(ctx, &got)
	if !strings.Contains(got.ExecutionGraphJSON.ValueString(), "edited-in-ui") {
		t.Errorf("expected the server's graph to replace the stored one, got %q",
			got.ExecutionGraphJSON.ValueString())
	}
}

// canvas_data_json is Optional-only, so the layout the API generates for an
// unconfigured canvas must never reach state: Terraform rejects an apply that
// turns a null Optional attribute into a value.
func TestWorkflowRead_LeavesAnUnconfiguredCanvasNull(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/versions/") {
			json.NewEncoder(w).Encode(versionJSON(
				map[string]interface{}{"nodes": []interface{}{}},
				map[string]interface{}{"viewport": map[string]interface{}{"zoom": 1}},
			))
			return
		}
		json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"published_version_id": 10}))
	})

	prior := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.Number, 42),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: prior}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var got workflowModel
	resp.State.Get(ctx, &got)
	if !got.CanvasDataJSON.IsNull() {
		t.Errorf("an API-generated canvas must stay out of state, got %q", got.CanvasDataJSON.ValueString())
	}
}

// A canvas the configuration does pin is Terraform's to track, so it refreshes.
func TestWorkflowRead_RefreshesAConfiguredCanvas(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/versions/") {
			json.NewEncoder(w).Encode(versionJSON(
				map[string]interface{}{"nodes": []interface{}{}},
				map[string]interface{}{"viewport": map[string]interface{}{"zoom": 4}},
			))
			return
		}
		json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"published_version_id": 10}))
	})

	prior := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.Number, 42),
		"canvas_data_json": tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":1}}`),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: prior}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var got workflowModel
	resp.State.Get(ctx, &got)
	if !strings.Contains(got.CanvasDataJSON.ValueString(), "4") {
		t.Errorf("a tracked canvas must refresh from the server, got %q", got.CanvasDataJSON.ValueString())
	}
}

// Nothing published means nothing to report, whatever state used to hold.
func TestWorkflowRead_NullGraphWhenNothingIsPublished(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var versionFetched bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/versions/") {
			versionFetched = true
		}
		json.NewEncoder(w).Encode(workflowJSON(nil))
	})

	prior := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":                   tftypes.NewValue(tftypes.Number, 42),
		"execution_graph_json": tftypes.NewValue(tftypes.String, `{"nodes":[]}`),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: prior}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if versionFetched {
		t.Error("a workflow with no published version must not fetch one")
	}
	var got workflowModel
	resp.State.Get(ctx, &got)
	if !got.ExecutionGraphJSON.IsNull() {
		t.Errorf("expected null once nothing is published, got %q", got.ExecutionGraphJSON.ValueString())
	}
}

// --- Update ---

// Changing only the layout still has to cut a version: republishing the same
// graph under a new canvas is the only way a pinned layout ever changes.
func TestWorkflowUpdate_RepublishesOnACanvasOnlyChange(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	graph := `{"nodes":[],"edges":[]}`
	var versionBody map[string]interface{}
	var versionCalls int
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/versions"):
			versionCalls++
			json.NewDecoder(req.Body).Decode(&versionBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version": map[string]interface{}{"id": 11, "version_number": 2, "created_at": "x"},
			})
		case strings.HasSuffix(req.URL.Path, "/publish"):
			w.WriteHeader(http.StatusOK)
		default:
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"published_version_id": 11}))
		}
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":                   tftypes.NewValue(tftypes.Number, 42),
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, graph),
		"canvas_data_json":     tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":2}}`),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":                   tftypes.NewValue(tftypes.Number, 42),
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, graph),
		"canvas_data_json":     tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":1}}`),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		State:  tfsdk.State{Schema: s, Raw: state},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if versionCalls != 1 {
		t.Fatalf("expected the canvas change to publish one version, got %d", versionCalls)
	}
	canvas, _ := versionBody["canvas_data"].(map[string]interface{})
	viewport, _ := canvas["viewport"].(map[string]interface{})
	if viewport == nil || viewport["zoom"] != float64(2) {
		t.Errorf("expected the new layout to be published, got %v", versionBody["canvas_data"])
	}
}

// Reformatting either payload publishes nothing: the republish is the expensive
// part, and a whitespace change must not cost a workflow version.
func TestWorkflowUpdate_ReformattingPublishesNothing(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var versionCalls int
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/versions") {
			versionCalls++
		}
		json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"published_version_id": 10}))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":                   tftypes.NewValue(tftypes.Number, 42),
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, "{\n  \"edges\": [],\n  \"nodes\": []\n}"),
		"canvas_data_json":     tftypes.NewValue(tftypes.String, "{\n  \"viewport\": { \"zoom\": 1 }\n}"),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":                   tftypes.NewValue(tftypes.Number, 42),
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, `{"nodes":[],"edges":[]}`),
		"canvas_data_json":     tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":1}}`),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		State:  tfsdk.State{Schema: s, Raw: state},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if versionCalls != 0 {
		t.Errorf("a pure reformat must not publish a version, got %d version calls", versionCalls)
	}
}

// The workflow endpoints do not document If-Match, so Update must not spend a
// GET fetching an ETag it will never send.
func TestWorkflowUpdate_SendsNoIfMatchAndDoesNotPreReadForOne(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var ifMatch string
	var getsBeforePatch int
	var patched bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case "GET":
			if !patched {
				getsBeforePatch++
			}
		case "PATCH":
			patched = true
			ifMatch = req.Header.Get("If-Match")
		}
		json.NewEncoder(w).Encode(workflowJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.Number, 42),
		"name":   tftypes.NewValue(tftypes.String, "Renamed"),
		"active": tftypes.NewValue(tftypes.Bool, false),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.Number, 42),
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, false),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		State:  tfsdk.State{Schema: s, Raw: state},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if ifMatch != "" {
		t.Errorf("workflow updates must not send If-Match, got %q", ifMatch)
	}
	if getsBeforePatch != 0 {
		t.Errorf("expected no ETag pre-read before the patch, got %d GETs", getsBeforePatch)
	}
}

// active = false on a running workflow has to pause it; the transition is the
// only thing that moves a workflow out of the active state.
func TestWorkflowUpdate_PausesAnActiveWorkflow(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var paused, activated bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/pause"):
			paused = true
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(req.URL.Path, "/activate"):
			activated = true
			w.WriteHeader(http.StatusOK)
		default:
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"status": "active"}))
		}
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.Number, 42),
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, false),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.Number, 42),
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, true),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		State:  tfsdk.State{Schema: s, Raw: state},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !paused {
		t.Error("active = false on an active workflow must pause it")
	}
	if activated {
		t.Error("pausing must not also activate")
	}
}

// The mirror: active = true on a paused workflow has to activate it.
func TestWorkflowUpdate_ActivatesAPausedWorkflow(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var activated, paused bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/activate"):
			activated = true
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(req.URL.Path, "/pause"):
			paused = true
			w.WriteHeader(http.StatusOK)
		default:
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"status": "paused"}))
		}
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.Number, 42),
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, true),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.Number, 42),
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, false),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		State:  tfsdk.State{Schema: s, Raw: state},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !activated {
		t.Error("active = true on a paused workflow must activate it")
	}
	if paused {
		t.Error("activating must not also pause")
	}
}

// Dropping a pinned layout has to republish the graph without a canvas, which is
// how the API is asked to lay it out again. Leaving the published version alone
// while state forgets the canvas hides a stale layout forever: Read skips an
// unconfigured canvas, so nothing would ever surface it again.
func TestWorkflowUpdate_RepublishesWhenAPinnedCanvasIsDropped(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	graph := `{"nodes":[],"edges":[]}`
	var versionBody map[string]interface{}
	var versionCalls int
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/versions"):
			versionCalls++
			json.NewDecoder(req.Body).Decode(&versionBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version": map[string]interface{}{"id": 11, "version_number": 2, "created_at": "x"},
			})
		case strings.HasSuffix(req.URL.Path, "/publish"):
			w.WriteHeader(http.StatusOK)
		default:
			json.NewEncoder(w).Encode(workflowJSON(map[string]interface{}{"published_version_id": 11}))
		}
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":                   tftypes.NewValue(tftypes.Number, 42),
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, graph),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":                   tftypes.NewValue(tftypes.Number, 42),
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, graph),
		"canvas_data_json":     tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":1}}`),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		State:  tfsdk.State{Schema: s, Raw: state},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if versionCalls != 1 {
		t.Fatalf("dropping a pinned canvas must publish a new version, got %d", versionCalls)
	}
	if _, ok := versionBody["canvas_data"]; ok {
		t.Errorf("the republish must omit canvas_data so the API lays the graph out, got %v",
			versionBody["canvas_data"])
	}
}

// The same drop on a workflow that has no graph has nothing to republish, and
// must not try: publishing needs a graph to send.
func TestWorkflowUpdate_DroppedCanvasWithNoGraphPublishesNothing(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var versionCalls int
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/versions") {
			versionCalls++
		}
		json.NewEncoder(w).Encode(workflowJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.Number, 42),
		"name":   tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active": tftypes.NewValue(tftypes.Bool, false),
	})
	state := nullObjectValue(t, objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.Number, 42),
		"name":             tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":           tftypes.NewValue(tftypes.Bool, false),
		"canvas_data_json": tftypes.NewValue(tftypes.String, `{"viewport":{"zoom":1}}`),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		State:  tfsdk.State{Schema: s, Raw: state},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if versionCalls != 0 {
		t.Errorf("there is no graph to republish, got %d version calls", versionCalls)
	}
}

// --- shouldRepublishCanvas ---

func TestShouldRepublishCanvas(t *testing.T) {
	pinned := jsontypes.NewNormalizedValue(`{"viewport":{"zoom":1}}`)

	tests := []struct {
		name            string
		planned, stored jsontypes.Normalized
		want            bool
	}{
		{"never pinned", jsontypes.NewNormalizedNull(), jsontypes.NewNormalizedNull(), false},
		{"newly pinned", pinned, jsontypes.NewNormalizedNull(), true},
		{"dropped", jsontypes.NewNormalizedNull(), pinned, true},
		{"reformatted", jsontypes.NewNormalizedValue("{\n  \"viewport\": { \"zoom\": 1 }\n}"), pinned, false},
		{"moved", jsontypes.NewNormalizedValue(`{"viewport":{"zoom":4}}`), pinned, true},
		{"unknown waits", jsontypes.NewNormalizedUnknown(), pinned, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diags := shouldRepublishCanvas(context.Background(), tt.planned, tt.stored)
			if diags.HasError() {
				t.Fatalf("diagnostics: %v", diags)
			}
			if got != tt.want {
				t.Errorf("shouldRepublishCanvas = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- unresolved creation-time inputs ---

// An unknown template_slug used to fall out of the template branch and quietly
// create a plain workflow instead. Failing loudly beats creating the wrong thing.
func TestWorkflowCreate_RejectsAnUnknownTemplateSlug(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var called bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(workflowJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"template_slug":        tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"execution_graph_json": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("an unknown template_slug must fail rather than create a plain workflow")
	}
	if called {
		t.Error("nothing must be created before the slug is known")
	}
}

// An unknown canvas would be omitted from the published version and then written
// into state as unknown, which fails the apply with a much vaguer message.
func TestWorkflowCreate_RejectsAnUnknownCanvas(t *testing.T) {
	ctx := context.Background()
	s := workflowSchema(t)
	objType := s.Type().TerraformType(ctx)

	var called bool
	r := newWorkflowResource(t, func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(workflowJSON(nil))
	})

	plan := nullObjectValue(t, objType, map[string]tftypes.Value{
		"name":                 tftypes.NewValue(tftypes.String, "CPU Alert"),
		"active":               tftypes.NewValue(tftypes.Bool, false),
		"execution_graph_json": tftypes.NewValue(tftypes.String, `{"nodes":[]}`),
		"canvas_data_json":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("an unknown canvas must fail rather than be dropped from the version")
	}
	if called {
		t.Error("nothing must be created before the canvas is known")
	}
}

// --- mapWorkflowToState ---

func TestMapWorkflowToState_LeavesGraphAttributesAlone(t *testing.T) {
	graph := jsontypes.NewNormalizedValue(`{"nodes":[]}`)
	state := &workflowModel{
		ExecutionGraphJSON: graph,
		CanvasDataJSON:     jsontypes.NewNormalizedValue(`{"viewport":{}}`),
		TemplateSlug:       types.StringValue("high-cpu"),
	}

	mapWorkflowToState(&client.Workflow{
		ID: 42, Name: "CPU Alert", Status: "draft",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}, state)

	if state.ExecutionGraphJSON.ValueString() != graph.ValueString() {
		t.Errorf("the graph lives on the version, not the workflow: %q", state.ExecutionGraphJSON.ValueString())
	}
	if state.CanvasDataJSON.IsNull() {
		t.Error("the canvas must survive a workflow mapping")
	}
	if state.TemplateSlug.ValueString() != "high-cpu" {
		t.Error("template_slug is a creation-time input the API never echoes; it must survive")
	}
}
