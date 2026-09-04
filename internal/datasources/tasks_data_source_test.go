package datasources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Must be wired for Configure, otherwise d.client stays nil and Read panics on
// the first apply.
var _ datasource.DataSourceWithConfigure = &tasksDataSource{}

func TestTasksDataSource_MetadataAndConfigure(t *testing.T) {
	resp := &datasource.MetadataResponse{}
	NewTasksDataSource().Metadata(context.Background(),
		datasource.MetadataRequest{ProviderTypeName: "fivenines"}, resp)
	if resp.TypeName != "fivenines_tasks" {
		t.Errorf("expected type name %q, got %q", "fivenines_tasks", resp.TypeName)
	}

	for _, tt := range []struct {
		name         string
		providerData interface{}
		wantError    bool
	}{
		// Terraform calls Configure with nil data before the provider is
		// configured; that must be a quiet no-op, not an error.
		{name: "nil provider data", providerData: nil},
		{name: "correct client", providerData: client.NewClient("https://example.com", "key")},
		{name: "wrong type", providerData: "not a client", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := &tasksDataSource{}
			resp := &datasource.ConfigureResponse{}
			d.Configure(context.Background(),
				datasource.ConfigureRequest{ProviderData: tt.providerData}, resp)
			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Fatalf("expected error %v, got diagnostics %v", tt.wantError, resp.Diagnostics)
			}
			if !tt.wantError && tt.providerData != nil && d.client == nil {
				t.Error("expected the client to be stored")
			}
		})
	}
}

// This data source copies the ping key of EVERY matching task into state,
// including tasks the configuration does not manage, so the two secret columns
// must carry Sensitive — otherwise they reach plan output and CI logs. Nothing
// else in the row may be marked Sensitive without being listed here, which is
// what keeps the README's state-secrets table honest.
func TestTasksDataSourceSchema_PingFieldsAreSensitive(t *testing.T) {
	s := dataSourceSchema(t, NewTasksDataSource())

	tasks, ok := s.Attributes["tasks"].(dschema.ListNestedAttribute)
	if !ok {
		t.Fatalf("expected tasks to be a ListNestedAttribute, got %T", s.Attributes["tasks"])
	}

	secrets := map[string]bool{"ping_key": true, "ping_url": true}
	// Through the Attribute interface, not a StringAttribute type assertion: the
	// latter silently skips every Int64 and Bool in the row, so the "nothing else
	// may be Sensitive" half of this check would not actually hold.
	for name, attr := range tasks.NestedObject.Attributes {
		switch {
		case secrets[name] && !attr.IsSensitive():
			t.Errorf("%s must be Sensitive — it authenticates heartbeats for the task", name)
		case !secrets[name] && attr.IsSensitive():
			t.Errorf("%s is marked Sensitive but is not in the documented secret set", name)
		}
	}
	for name := range secrets {
		if _, ok := tasks.NestedObject.Attributes[name]; !ok {
			t.Errorf("expected the schema to carry %s", name)
		}
	}
}

// The cursor guard. `updated_since` is INCLUSIVE, so capping the result can stop
// the poll advancing entirely: with limit = 1 the same task returns forever, and
// with any limit it stalls once that many tasks share the boundary timestamp.
// Sorting ascending does not rescue it — a bounded cursor needs an exclusive
// (updated_at, id) tie-break the API does not offer.
func TestValidateTasksCursor(t *testing.T) {
	str := func(v string) types.String { return types.StringValue(v) }

	for _, tt := range []struct {
		name      string
		config    tasksModel
		wantError bool
	}{
		{
			name:   "limit alone is fine",
			config: tasksModel{Limit: types.Int64Value(10)},
		},
		{
			name:   "updated_since alone is fine",
			config: tasksModel{UpdatedSince: str("2026-01-01T00:00:00Z")},
		},
		{
			name: "both is refused under the API default sort",
			config: tasksModel{
				Limit: types.Int64Value(10), UpdatedSince: str("2026-01-01T00:00:00Z"),
			},
			wantError: true,
		},
		{
			// The plausible-looking pairing, and the reason this guard is not a
			// conditional one: ascending order does not make an inclusive cursor
			// safe to truncate.
			name: "both is refused even sorted oldest-update-first",
			config: tasksModel{
				Limit: types.Int64Value(10), UpdatedSince: str("2026-01-01T00:00:00Z"),
				Order: str("updated_at"), Direction: str("asc"),
			},
			wantError: true,
		},
		{
			name: "limit 1 is the guaranteed stall",
			config: tasksModel{
				Limit: types.Int64Value(1), UpdatedSince: str("2026-01-01T00:00:00Z"),
				Order: str("updated_at"), Direction: str("asc"),
			},
			wantError: true,
		},
		{
			// Unknowns only resolve at apply time. Read re-runs the check then,
			// so deferring here is safe rather than a hole.
			name: "an unknown limit is left to apply time",
			config: tasksModel{
				Limit: types.Int64Unknown(), UpdatedSince: str("2026-01-01T00:00:00Z"),
			},
		},
		{
			name: "an unknown updated_since is left to apply time",
			config: tasksModel{
				Limit: types.Int64Value(10), UpdatedSince: types.StringUnknown(),
			},
		},
		{
			// filterString drops a blank, so no cursor reaches the API and this
			// is an ordinary bounded snapshot. `updated_since = var.cursor` with
			// an unset variable is the shape that lands here.
			name: "a blank updated_since is not a cursor",
			config: tasksModel{
				Limit: types.Int64Value(10), UpdatedSince: str(""),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateTasksCursor(tt.config)
			if got := diags.HasError(); got != tt.wantError {
				t.Fatalf("expected error %v, got diagnostics %v", tt.wantError, diags)
			}
		})
	}
}

// The schema validator only ever sees a value that was known at plan time. A
// limit wired from another resource's output is unknown then and resolved later,
// so the range check has to exist somewhere Read can call it.
func TestValidateTasksLimit(t *testing.T) {
	for _, tt := range []struct {
		name      string
		limit     types.Int64
		wantError bool
	}{
		{name: "unset", limit: types.Int64Null()},
		{name: "unknown", limit: types.Int64Unknown()},
		{name: "valid", limit: types.Int64Value(50)},
		{name: "at the ceiling", limit: types.Int64Value(maxListLimit)},
		// 0 must be an ERROR, not the client's unbounded sentinel: a practitioner
		// who computed a cap of 0 would otherwise get the whole index, secrets
		// included.
		{name: "zero", limit: types.Int64Value(0), wantError: true},
		{name: "negative", limit: types.Int64Value(-1), wantError: true},
		{name: "over the ceiling", limit: types.Int64Value(maxListLimit + 1), wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateTasksLimit(tasksModel{Limit: tt.limit})
			if got := diags.HasError(); got != tt.wantError {
				t.Fatalf("expected error %v, got diagnostics %v", tt.wantError, diags)
			}
		})
	}
}

func TestFilterLimit_ClampsInsteadOfTruncating(t *testing.T) {
	if got := filterLimit(types.Int64Null()); got != 0 {
		t.Errorf("expected an unset limit to read as unbounded (0), got %d", got)
	}
	if got := filterLimit(types.Int64Unknown()); got != 0 {
		t.Errorf("expected an unknown limit to read as unbounded (0), got %d", got)
	}
	if got := filterLimit(types.Int64Value(25)); got != 25 {
		t.Errorf("expected 25, got %d", got)
	}
	// A bare int(...) turns this into 0 on a 32-bit build, and 0 means UNBOUNDED
	// to the walkers — the practitioner's cap silently becoming a full index walk.
	if got := filterLimit(types.Int64Value(4294967296)); got != maxListLimit {
		t.Errorf("expected an oversized limit clamped to %d, got %d", maxListLimit, got)
	}
	// Never hand a negative through: the walker rejects it, and 0 would read as
	// unbounded, so neither is a safe rendering of a bad value.
	if got := filterLimit(types.Int64Value(-1)); got != 0 {
		t.Errorf("expected a negative limit to read as unbounded (0), got %d", got)
	}
}

func TestTasksDataSource_Read(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id": "3cac0e44-0000-4000-8000-000000000001", "name": "nightly backup",
					"schedule_type": "cron", "schedule": "0 3 * * *", "interval_seconds": nil,
					"time_zone": "Europe/Paris", "grace_period_minutes": 10,
					"status": "active", "monitoring_status": "ok",
					"ping_key":         "8f14e45f-ceea-467a-9f3a-0000000000a1",
					"ping_url":         "https://app.fivenines.io/ping/8f14e45f-ceea-467a-9f3a-0000000000a1",
					"host_id":          "3cac0e44-0000-4000-8000-0000000000ff",
					"expected_ping_at": "2026-01-02T03:00:00Z", "last_ping_at": "2026-01-01T03:00:04Z",
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				},
				map[string]interface{}{
					"id": "3cac0e44-0000-4000-8000-000000000002", "name": "queue drain",
					"schedule_type": "interval", "schedule": "", "interval_seconds": 300,
					"time_zone": "UTC", "grace_period_minutes": 5,
					"status": "paused", "monitoring_status": "paused",
					"ping_key":         "8f14e45f-ceea-467a-9f3a-0000000000a2",
					"ping_url":         "https://app.fivenines.io/ping/8f14e45f-ceea-467a-9f3a-0000000000a2",
					"host_id":          nil,
					"expected_ping_at": nil, "last_ping_at": nil,
					"created_at": "2026-01-03T00:00:00Z", "updated_at": "2026-01-04T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &tasksDataSource{client: c}, map[string]tftypes.Value{
		"status":        tftypes.NewValue(tftypes.String, "active"),
		"schedule_type": tftypes.NewValue(tftypes.String, "cron"),
		"query":         tftypes.NewValue(tftypes.String, "backup"),
		"updated_since": tftypes.NewValue(tftypes.String, "2026-01-01T00:00:00Z"),
		"order":         tftypes.NewValue(tftypes.String, "name"),
		"direction":     tftypes.NewValue(tftypes.String, "asc"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}

	// Every schema filter has to reach the API, including the query -> q rename.
	// The fixture above deliberately answers with rows these filters would have
	// excluded: this test asserts what the provider SENDS, and the response half is
	// there to exercise the mapping of both schedule types in one read. Whether the
	// server actually honoured the filters is a separate, unclosed gap — TODOS.md
	// tracks it under "Index filters are trusted without verification".
	for key, want := range map[string]string{
		"status": "active", "schedule_type": "cron", "q": "backup",
		"updated_since": "2026-01-01T00:00:00Z", "order": "name", "direction": "asc",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q in the request, got %q", key, want, got)
		}
	}

	var out tasksModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(out.Tasks))
	}
	// Filters are echoed back so the data source round-trips its own config.
	if out.Query.ValueString() != "backup" || out.ScheduleType.ValueString() != "cron" {
		t.Errorf("expected the filters to be preserved, got query=%q schedule_type=%q",
			out.Query.ValueString(), out.ScheduleType.ValueString())
	}

	cron := out.Tasks[0]
	if cron.ID.ValueString() != "3cac0e44-0000-4000-8000-000000000001" || cron.Name.ValueString() != "nightly backup" {
		t.Errorf("unexpected first task: %+v", cron)
	}
	if cron.Schedule.ValueString() != "0 3 * * *" {
		t.Errorf("unexpected schedule: %q", cron.Schedule.ValueString())
	}
	if !cron.IntervalSeconds.IsNull() {
		t.Errorf("expected a null interval_seconds on a cron task, got %v", cron.IntervalSeconds)
	}
	if cron.TimeZone.ValueString() != "Europe/Paris" || cron.GracePeriodMinutes.ValueInt64() != 10 {
		t.Errorf("unexpected schedule fields: %+v", cron)
	}
	if cron.Paused.ValueBool() {
		t.Error("expected an active task to read as not paused")
	}
	if cron.PingKey.ValueString() != "8f14e45f-ceea-467a-9f3a-0000000000a1" ||
		cron.PingURL.ValueString() != "https://app.fivenines.io/ping/8f14e45f-ceea-467a-9f3a-0000000000a1" {
		t.Errorf("unexpected ping fields: %+v", cron)
	}
	if cron.HostID.ValueString() != "3cac0e44-0000-4000-8000-0000000000ff" {
		t.Errorf("unexpected host_id: %q", cron.HostID.ValueString())
	}
	if cron.ExpectedPingAt.ValueString() != "2026-01-02T03:00:00Z" || cron.LastPingAt.ValueString() != "2026-01-01T03:00:04Z" {
		t.Errorf("unexpected ping timestamps: %+v", cron)
	}

	interval := out.Tasks[1]
	// paused is derived from status, the same way fivenines_task derives it, so a
	// config can filter on one and branch on the other without a second lookup.
	if interval.Status.ValueString() != "paused" || !interval.Paused.ValueBool() {
		t.Errorf("expected a paused task to read as paused, got %+v", interval)
	}
	if interval.IntervalSeconds.ValueInt64() != 300 {
		t.Errorf("expected interval_seconds 300, got %v", interval.IntervalSeconds)
	}
	// The API answers "" for the schedule of an interval task as well as null;
	// both mean "no cron expression", and fivenines_task folds them the same way.
	if !interval.Schedule.IsNull() {
		t.Errorf("expected an empty schedule to read as null, got %q", interval.Schedule.ValueString())
	}
	// A null host_id is "not tied to a host", not the empty string.
	if !interval.HostID.IsNull() {
		t.Errorf("expected a null host_id, got %q", interval.HostID.ValueString())
	}
	if !interval.ExpectedPingAt.IsNull() || !interval.LastPingAt.IsNull() {
		t.Errorf("expected null ping timestamps on a task that never pinged, got %+v", interval)
	}
}

// limit is a client-side cap on the walk, not a query parameter: the API has no
// `limit` key and would 400 on one. It must bound the result without ever
// reaching the wire.
func TestTasksDataSource_Read_LimitBoundsWithoutReachingTheAPI(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		rows := make([]map[string]interface{}, 0, 3)
		for i := 0; i < 3; i++ {
			rows = append(rows, map[string]interface{}{
				"id": fmt.Sprintf("task-%d", i), "name": "backup",
				"schedule_type": "cron", "status": "active", "monitoring_status": "ok",
			})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": rows,
			"meta":  map[string]int{"current_page": 1, "total_pages": 1, "total_count": 3, "per_page": 2},
		})
	})

	state, resp := readDataSource(t, &tasksDataSource{client: c}, map[string]tftypes.Value{
		"limit": tftypes.NewValue(tftypes.Number, 2),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// The API rejects unknown query keys with a 400, so a limit that leaked into
	// the query string would fail every read.
	if _, ok := gotQuery["limit"]; ok {
		t.Error("limit must not be sent as a query parameter")
	}
	if got := gotQuery.Get("per_page"); got != "2" {
		t.Errorf("expected the limit to shrink per_page to 2, got %q", got)
	}

	var out tasksModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.Tasks) != 2 {
		t.Errorf("expected the result capped at 2, got %d", len(out.Tasks))
	}
	if out.Limit.ValueInt64() != 2 {
		t.Errorf("expected limit to round-trip into state, got %v", out.Limit)
	}
}

func TestTasksDataSource_Read_NoFiltersAndNoResults(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []interface{}{},
			"meta":  map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &tasksDataSource{client: c}, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// Null config attributes must not become empty-string filters: "status=" is
	// not what "any status" means, and whether the server reads a blank as absent
	// or refuses it is its choice per filter, not something to rely on here.
	for _, key := range []string{"status", "schedule_type", "q", "updated_since", "order", "direction"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %s to be omitted when unset", key)
		}
	}

	var out tasksModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.Tasks) != 0 {
		t.Errorf("expected no tasks, got %d", len(out.Tasks))
	}
	// An unset limit is "no cap". If it collapsed to a 0 cap, every unbounded
	// read would come back empty — which reads as "you have no tasks".
	if !out.Limit.IsNull() {
		t.Errorf("expected an unset limit to stay null, got %v", out.Limit)
	}
	// Zero matches must serialise as [] and not null: length()/for_each/toset
	// over a null list fail, and zero matches is normal for a filtered read.
	var tasks types.List
	if diags := state.GetAttribute(context.Background(), path.Root("tasks"), &tasks); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if tasks.IsNull() {
		t.Error("expected an empty list, got null")
	}
}

// query and updated_since carry no OneOf validator on purpose — TODOS.md records
// the decision — so a malformed value is a mid-refresh 400 and the diagnostic
// detail is the practitioner's only clue about what the server objected to.
func TestTasksDataSource_Read_ForwardsAPIRejection(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []string{"updated_since is not a valid ISO 8601 timestamp"},
		})
	})

	_, resp := readDataSource(t, &tasksDataSource{client: c}, map[string]tftypes.Value{
		"updated_since": tftypes.NewValue(tftypes.String, "yesterday"),
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic for a 400")
	}
	if got := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(got, "valid ISO 8601") {
		t.Errorf("expected the server's reason to survive into the diagnostic, got %q", got)
	}
}

func TestTasksDataSource_Read_Error(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "boom"})
	})

	state, resp := readDataSource(t, &tasksDataSource{client: c}, nil)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic for a 500")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); got != "Error listing tasks" {
		t.Errorf("unexpected diagnostic summary: %q", got)
	}
	// State must be left null on a failed read. Writing a partial state before
	// checking err would publish an empty task list, which reads as "you have
	// no tasks" rather than "the read failed".
	if !state.Raw.IsNull() {
		t.Error("expected state to be left null when the read fails")
	}
}
