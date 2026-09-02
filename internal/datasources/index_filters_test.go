package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The filtered data sources must be wired for Configure, otherwise d.client
// stays nil and Read panics on the first apply.
var (
	_ datasource.DataSourceWithConfigure = &incidentsDataSource{}
	_ datasource.DataSourceWithConfigure = &integrationsDataSource{}
	_ datasource.DataSourceWithConfigure = &workflowRunsDataSource{}
	_ datasource.DataSourceWithConfigure = &workflowRunDataSource{}
)

// configure attaches a client pointed at srv so Read reaches the fake API.
func configure(t *testing.T, d datasource.DataSource, c *client.Client) {
	t.Helper()
	withCfg, ok := d.(datasource.DataSourceWithConfigure)
	if !ok {
		t.Fatalf("%T does not implement DataSourceWithConfigure", d)
	}
	resp := &datasource.ConfigureResponse{}
	withCfg.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: c}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("configure failed: %v", resp.Diagnostics.Errors())
	}
}

// --- helpers (#21) ---

func TestOptionalInt64(t *testing.T) {
	if got := optionalInt64(nil); !got.IsNull() {
		t.Errorf("expected null for a nil pointer, got %v", got)
	}
	v := int64(42)
	if got := optionalInt64(&v); got.IsNull() || got.ValueInt64() != 42 {
		t.Errorf("expected 42, got %v", got)
	}
	// 0 from the API is a value, not an absence — a run that started and
	// finished inside the same second really did take 0 seconds.
	zero := int64(0)
	if got := optionalInt64(&zero); got.IsNull() || got.ValueInt64() != 0 {
		t.Errorf("expected a 0 value, got %v", got)
	}
}

func TestOptionalFloat64(t *testing.T) {
	if got := optionalFloat64(nil); !got.IsNull() {
		t.Errorf("expected null for a nil pointer, got %v", got)
	}
	v := 1.25
	if got := optionalFloat64(&v); got.IsNull() || got.ValueFloat64() != 1.25 {
		t.Errorf("expected 1.25, got %v", got)
	}
}

func TestJSONString(t *testing.T) {
	if got := jsonString(nil); !got.IsNull() {
		t.Errorf("expected null for a nil map, got %v", got)
	}
	// An empty object is a value: the node ran and produced nothing, which is
	// not the same as the field being absent.
	if got := jsonString(map[string]interface{}{}); got.IsNull() || got.ValueString() != "{}" {
		t.Errorf("expected {}, got %v", got)
	}
	got := jsonString(map[string]interface{}{"value": float64(91)})
	if got.IsNull() {
		t.Fatal("expected a value, got null")
	}
	var round map[string]interface{}
	if err := json.Unmarshal([]byte(got.ValueString()), &round); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if round["value"] != float64(91) {
		t.Errorf("expected value 91 to round-trip, got %v", round)
	}
}

// --- config -> ListOptions ---
//
// The data sources' whole job on the request side is turning configured
// arguments into an allowlisted query. These drive Read against a fake API and
// assert on the query string it received, which is the only place the mapping
// is observable.

func TestIncidentsDataSource_SendsConfiguredFilters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	d := NewIncidentsDataSource()
	configure(t, d, c)

	_, resp := readDataSource(t, d, map[string]tftypes.Value{
		"status":            tftypes.NewValue(tftypes.String, "open"),
		"q":                 tftypes.NewValue(tftypes.String, "CPU"),
		"host_id":           tftypes.NewValue(tftypes.String, "3cac0e44-0000-4000-8000-000000000001"),
		"task_id":           tftypes.NewValue(tftypes.String, "3cac0e44-0000-4000-8000-000000000002"),
		"uptime_monitor_id": tftypes.NewValue(tftypes.String, "3cac0e44-0000-4000-8000-000000000003"),
		"workflow_id":       tftypes.NewValue(tftypes.Number, 7),
		"from":              tftypes.NewValue(tftypes.String, "2026-08-29T00:00:00Z"),
		"to":                tftypes.NewValue(tftypes.String, "2026-08-30T00:00:00Z"),
		"updated_since":     tftypes.NewValue(tftypes.String, "2026-08-28T00:00:00Z"),
		"order":             tftypes.NewValue(tftypes.String, "updated_at"),
		"direction":         tftypes.NewValue(tftypes.String, "asc"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}

	want := map[string]string{
		"status": "open", "q": "CPU",
		"host_id":           "3cac0e44-0000-4000-8000-000000000001",
		"task_id":           "3cac0e44-0000-4000-8000-000000000002",
		"uptime_monitor_id": "3cac0e44-0000-4000-8000-000000000003",
		"workflow_id":       "7",
		"from":              "2026-08-29T00:00:00Z", "to": "2026-08-30T00:00:00Z",
		"updated_since": "2026-08-28T00:00:00Z",
		"order":         "updated_at", "direction": "asc",
		"page": "1", "per_page": "100",
	}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
	if len(gotQuery) != len(want) {
		t.Errorf("expected exactly %d query params, got %d: %v", len(want), len(gotQuery), gotQuery)
	}
}

// An unset argument must stay out of the query. Sending it empty is not
// equivalent: the API rejects an unknown or malformed parameter with a 400.
func TestIncidentsDataSource_UnsetFiltersAreNotSent(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	d := NewIncidentsDataSource()
	configure(t, d, c)

	_, resp := readDataSource(t, d, map[string]tftypes.Value{})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}
	if len(gotQuery) != 2 || gotQuery.Get("page") != "1" || gotQuery.Get("per_page") != "100" {
		t.Errorf("expected only page/per_page, got %v", gotQuery)
	}
}

func TestIncidentsDataSource_MapsNewFields(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{
				{
					"id": 1, "title": "High CPU", "summary": "over 90%", "status": "open",
					"public":            true,
					"host_id":           "3cac0e44-0000-4000-8000-000000000001",
					"uptime_monitor_id": "3cac0e44-0000-4000-8000-000000000003",
					"workflow_id":       7,
					"duration_seconds":  3600,
					"created_at":        "2026-01-01T00:00:00Z",
					"updated_at":        "2026-01-01T00:00:00Z",
				},
				{
					"id": 2, "title": "Disk", "summary": "", "status": "resolved",
					"public": false, "created_at": "2026-01-01T00:00:00Z",
					"updated_at": "2026-01-01T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	d := NewIncidentsDataSource()
	configure(t, d, c)

	state, resp := readDataSource(t, d, map[string]tftypes.Value{})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}

	var out incidentsModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("state.Get failed: %v", diags.Errors())
	}
	if len(out.Incidents) != 2 {
		t.Fatalf("expected 2 incidents, got %d", len(out.Incidents))
	}
	if !out.Incidents[0].Public.ValueBool() {
		t.Error("expected incident 1 to be public")
	}
	if out.Incidents[1].Public.ValueBool() {
		t.Error("expected incident 2 to not be public")
	}
	if got := out.Incidents[0].UptimeMonitorID.ValueString(); got != "3cac0e44-0000-4000-8000-000000000003" {
		t.Errorf("uptime_monitor_id = %q, want the mapped uuid", got)
	}
	// A null association stays null rather than collapsing to "".
	if !out.Incidents[1].UptimeMonitorID.IsNull() {
		t.Errorf("expected null uptime_monitor_id, got %v", out.Incidents[1].UptimeMonitorID)
	}
	if got := out.Incidents[0].WorkflowID.ValueString(); got != "7" {
		t.Errorf("workflow_id = %q, want \"7\"", got)
	}
	if got := out.Incidents[0].DurationSeconds.ValueInt64(); got != 3600 {
		t.Errorf("duration_seconds = %d, want 3600", got)
	}
}

// A filter that matches nothing has to read back as an empty list. A null one
// makes `for inc in data.fivenines_incidents.x.incidents` fail at plan time,
// which is the common shape now that filters exist.
func TestIncidentsDataSource_EmptyResultIsEmptyListNotNull(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	d := NewIncidentsDataSource()
	configure(t, d, c)

	state, resp := readDataSource(t, d, map[string]tftypes.Value{
		"status": tftypes.NewValue(tftypes.String, "muted"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}

	var out incidentsModel
	if d := state.Get(context.Background(), &out); d.HasError() {
		t.Fatalf("state.Get failed: %v", d.Errors())
	}
	if out.Incidents == nil {
		t.Fatal("expected an empty, non-nil incidents slice")
	}
	if len(out.Incidents) != 0 {
		t.Fatalf("expected 0 incidents, got %d", len(out.Incidents))
	}
}

func TestIntegrationsDataSource_SendsConfiguredFilters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integrations": []map[string]interface{}{},
			"meta":         map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	d := NewIntegrationsDataSource()
	configure(t, d, c)

	_, resp := readDataSource(t, d, map[string]tftypes.Value{
		"type":          tftypes.NewValue(tftypes.String, "SlackIntegration"),
		"enabled":       tftypes.NewValue(tftypes.Bool, true),
		"q":             tftypes.NewValue(tftypes.String, "ops"),
		"updated_since": tftypes.NewValue(tftypes.String, "2026-08-30T12:00:00Z"),
		"order":         tftypes.NewValue(tftypes.String, "type"),
		"direction":     tftypes.NewValue(tftypes.String, "asc"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}

	want := map[string]string{
		"type": "SlackIntegration", "enabled": "true", "q": "ops",
		"updated_since": "2026-08-30T12:00:00Z",
		"order":         "type", "direction": "asc",
		"page": "1", "per_page": "100",
	}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
	if len(gotQuery) != len(want) {
		t.Errorf("expected exactly %d query params, got %d: %v", len(want), len(gotQuery), gotQuery)
	}
}

// enabled = false is a real filter, not "unset". Collapsing the two would
// silently widen every disabled-channel lookup to the whole index.
func TestIntegrationsDataSource_EnabledFalseIsSent(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integrations": []map[string]interface{}{},
			"meta":         map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	d := NewIntegrationsDataSource()
	configure(t, d, c)

	_, resp := readDataSource(t, d, map[string]tftypes.Value{
		"enabled": tftypes.NewValue(tftypes.Bool, false),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}
	if got := gotQuery.Get("enabled"); got != "false" {
		t.Errorf("expected enabled=false to be sent, got %q (query: %v)", got, gotQuery)
	}
}

func TestIntegrationsDataSource_UnsetEnabledIsNotSent(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integrations": []map[string]interface{}{},
			"meta":         map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	d := NewIntegrationsDataSource()
	configure(t, d, c)

	_, resp := readDataSource(t, d, map[string]tftypes.Value{})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}
	if _, ok := gotQuery["enabled"]; ok {
		t.Errorf("expected no enabled param when unset, got %v", gotQuery)
	}
	if len(gotQuery) != 2 {
		t.Errorf("expected only page/per_page, got %v", gotQuery)
	}
}

func TestWorkflowRunsDataSource_SendsConfiguredFilters(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"runs": []map[string]interface{}{},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	d := NewWorkflowRunsDataSource()
	configure(t, d, c)

	_, resp := readDataSource(t, d, map[string]tftypes.Value{
		"workflow_id":   tftypes.NewValue(tftypes.Number, 3),
		"status":        tftypes.NewValue(tftypes.String, "failed"),
		"updated_since": tftypes.NewValue(tftypes.String, "2026-01-01T00:00:00Z"),
		"order":         tftypes.NewValue(tftypes.String, "completed_at"),
		"direction":     tftypes.NewValue(tftypes.String, "asc"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}

	// workflow_id addresses the endpoint; it is never a query parameter.
	if gotPath != "/api/v1/workflows/3/runs" {
		t.Errorf("path = %q, want /api/v1/workflows/3/runs", gotPath)
	}
	want := map[string]string{
		"status": "failed", "updated_since": "2026-01-01T00:00:00Z",
		"order": "completed_at", "direction": "asc",
		"page": "1", "per_page": "100",
	}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
	if len(gotQuery) != len(want) {
		t.Errorf("expected exactly %d query params, got %d: %v", len(want), len(gotQuery), gotQuery)
	}
}

func TestWorkflowRunsDataSource_MapsNewFields(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"runs": []map[string]interface{}{{
				"id": 10, "status": "running", "resource_key": "web-1",
				"workflow_id": 3, "workflow_version_id": 9,
				"started_at": "2026-01-01T00:00:00Z", "completed_at": nil,
				"duration_seconds": 42,
				"created_at":       "2026-01-01T00:00:00Z",
				"updated_at":       "2026-01-01T00:00:10Z",
			}, {
				// A workflow that dispatches once: resource_key is null.
				"id": 11, "status": "completed", "resource_key": nil,
				"workflow_id": 3, "workflow_version_id": 9,
				"started_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:00:05Z",
				"duration_seconds": 5,
				"created_at":       "2026-01-01T00:00:00Z",
				"updated_at":       "2026-01-01T00:00:05Z",
			}},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	d := NewWorkflowRunsDataSource()
	configure(t, d, c)

	state, resp := readDataSource(t, d, map[string]tftypes.Value{
		"workflow_id": tftypes.NewValue(tftypes.Number, 3),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}

	var out workflowRunsModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("state.Get failed: %v", diags.Errors())
	}
	if len(out.Runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(out.Runs))
	}
	run := out.Runs[0]
	// Both sides of the nullability: a fan-out run carries its subject, a
	// dispatch-once run reads back null rather than "".
	if run.ResourceKey.ValueString() != "web-1" {
		t.Errorf("resource_key = %v, want web-1", run.ResourceKey)
	}
	if !out.Runs[1].ResourceKey.IsNull() {
		t.Errorf("expected null resource_key on the dispatch-once run, got %v", out.Runs[1].ResourceKey)
	}
	if run.WorkflowID.ValueInt64() != 3 {
		t.Errorf("workflow_id = %d, want 3", run.WorkflowID.ValueInt64())
	}
	if run.WorkflowVersionID.ValueInt64() != 9 {
		t.Errorf("workflow_version_id = %d, want 9", run.WorkflowVersionID.ValueInt64())
	}
	// Elapsed-so-far on a running run, not a final duration.
	if run.DurationSeconds.ValueInt64() != 42 {
		t.Errorf("duration_seconds = %d, want 42", run.DurationSeconds.ValueInt64())
	}
	if run.UpdatedAt.ValueString() != "2026-01-01T00:00:10Z" {
		t.Errorf("updated_at = %q, want the mapped timestamp", run.UpdatedAt.ValueString())
	}
	if !run.CompletedAt.IsNull() {
		t.Errorf("expected null completed_at on a running run, got %v", run.CompletedAt)
	}
}

func TestWorkflowRunDataSource_MapsStepsAndJSON(t *testing.T) {
	var gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"run": map[string]interface{}{
				"id": 10, "status": "failed", "resource_key": nil,
				"workflow_id": 3, "workflow_version_id": 9,
				"started_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:01:00Z",
				"duration_seconds": 60,
				"created_at":       "2026-01-01T00:00:00Z",
				"updated_at":       "2026-01-01T00:01:00Z",
				"error":            "trigger raised",
				"trigger_output":   map[string]interface{}{"value": 91},
				"steps": []map[string]interface{}{
					{
						"id": 1, "node_id": "email_1", "node_type": "email_alert",
						"status": "failed", "error_message": "smtp timeout",
						"output_data":      map[string]interface{}{"delivered": false},
						"started_at":       "2026-01-01T00:00:30Z",
						"completed_at":     "2026-01-01T00:00:31Z",
						"duration_seconds": 1.25,
						"created_at":       "2026-01-01T00:00:30Z",
					},
					{
						"id": 2, "node_id": "wait_1", "node_type": "delay",
						"status": "running", "error_message": nil,
						"output_data":      map[string]interface{}{},
						"started_at":       "2026-01-01T00:00:31Z",
						"completed_at":     nil,
						"duration_seconds": nil,
						"created_at":       "2026-01-01T00:00:31Z",
					},
				},
			},
		})
	})

	d := NewWorkflowRunDataSource()
	configure(t, d, c)

	state, resp := readDataSource(t, d, map[string]tftypes.Value{
		"workflow_id": tftypes.NewValue(tftypes.Number, 3),
		"run_id":      tftypes.NewValue(tftypes.Number, 10),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("read failed: %v", resp.Diagnostics.Errors())
	}
	if gotPath != "/api/v1/workflows/3/runs/10" {
		t.Errorf("path = %q, want /api/v1/workflows/3/runs/10", gotPath)
	}

	var out workflowRunModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("state.Get failed: %v", diags.Errors())
	}
	if out.Status.ValueString() != "failed" {
		t.Errorf("status = %q, want failed", out.Status.ValueString())
	}
	if out.Error.ValueString() != "trigger raised" {
		t.Errorf("error = %q, want the run-level message", out.Error.ValueString())
	}
	if out.WorkflowVersionID.ValueInt64() != 9 {
		t.Errorf("workflow_version_id = %d, want 9", out.WorkflowVersionID.ValueInt64())
	}
	if out.DurationSeconds.ValueInt64() != 60 {
		t.Errorf("duration_seconds = %d, want 60", out.DurationSeconds.ValueInt64())
	}
	if out.StartedAt.ValueString() != "2026-01-01T00:00:00Z" {
		t.Errorf("started_at = %q", out.StartedAt.ValueString())
	}
	if out.CompletedAt.ValueString() != "2026-01-01T00:01:00Z" {
		t.Errorf("completed_at = %q", out.CompletedAt.ValueString())
	}
	if out.CreatedAt.ValueString() != "2026-01-01T00:00:00Z" {
		t.Errorf("created_at = %q", out.CreatedAt.ValueString())
	}
	if out.UpdatedAt.ValueString() != "2026-01-01T00:01:00Z" {
		t.Errorf("updated_at = %q", out.UpdatedAt.ValueString())
	}
	// Null, not "": this fixture is a workflow that dispatches once, and a
	// config has to be able to tell that from a fan-out over an empty key.
	if !out.ResourceKey.IsNull() {
		t.Errorf("expected null resource_key on a non-fan-out run, got %v", out.ResourceKey)
	}
	// The JSON passthrough has to be decodable, not just non-empty.
	var trigger map[string]interface{}
	if err := json.Unmarshal([]byte(out.TriggerOutputJSON.ValueString()), &trigger); err != nil {
		t.Fatalf("trigger_output_json is not valid JSON: %v", err)
	}
	if trigger["value"] != float64(91) {
		t.Errorf("trigger_output_json = %v, want value 91", trigger)
	}

	if len(out.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(out.Steps))
	}
	if out.Steps[0].ErrorMessage.ValueString() != "smtp timeout" {
		t.Errorf("step error_message = %q", out.Steps[0].ErrorMessage.ValueString())
	}
	if out.Steps[0].DurationSeconds.ValueFloat64() != 1.25 {
		t.Errorf("step duration = %v, want 1.25", out.Steps[0].DurationSeconds)
	}
	// Null, never 0, while a step has not both started and finished.
	if !out.Steps[1].DurationSeconds.IsNull() {
		t.Errorf("expected null duration on the unfinished step, got %v", out.Steps[1].DurationSeconds)
	}
	if !out.Steps[1].ErrorMessage.IsNull() {
		t.Errorf("expected null error_message on the running step, got %v", out.Steps[1].ErrorMessage)
	}
	var output map[string]interface{}
	if err := json.Unmarshal([]byte(out.Steps[0].OutputDataJSON.ValueString()), &output); err != nil {
		t.Fatalf("output_data_json is not valid JSON: %v", err)
	}
	if output["delivered"] != false {
		t.Errorf("output_data_json = %v, want delivered false", output)
	}
}

// A run id that belongs to another workflow is a 404, and that has to reach the
// practitioner as an error rather than an empty run.
func TestWorkflowRunDataSource_NotFoundIsAnError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})

	d := NewWorkflowRunDataSource()
	configure(t, d, c)

	_, resp := readDataSource(t, d, map[string]tftypes.Value{
		"workflow_id": tftypes.NewValue(tftypes.Number, 3),
		"run_id":      tftypes.NewValue(tftypes.Number, 999),
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic for a run on another workflow")
	}
}
