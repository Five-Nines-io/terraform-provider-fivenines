package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Both data sources must be wired for Configure, otherwise d.client stays nil
// and Read panics on the first apply.
var (
	_ datasource.DataSourceWithConfigure = &uptimeMonitorStatusDataSource{}
	_ datasource.DataSourceWithConfigure = &uptimeMonitorsDataSource{}
	_ datasource.DataSourceWithConfigure = &workflowTemplatesDataSource{}
	_ datasource.DataSourceWithConfigure = &workflowNodeTypesDataSource{}
)

// newTestServer creates a test HTTP server with the given handler.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *client.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, client.NewClient(srv.URL, "test-api-key")
}

// dataSourceSchema returns the live schema of d.
func dataSourceSchema(t *testing.T, d datasource.DataSource) dschema.Schema {
	t.Helper()
	resp := &datasource.SchemaResponse{}
	d.Schema(context.Background(), datasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// nullObjectValue builds an object value of typ with every attribute null,
// then applies overrides.
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

// readDataSource drives a data source Read the way the framework does: a config
// built from the live schema in, a state value out.
func readDataSource(t *testing.T, d datasource.DataSource, config map[string]tftypes.Value) (tfsdk.State, *datasource.ReadResponse) {
	t.Helper()
	ctx := context.Background()
	s := dataSourceSchema(t, d)
	objType := s.Type().TerraformType(ctx)

	req := datasource.ReadRequest{
		Config: tfsdk.Config{Schema: s, Raw: nullObjectValue(t, objType, config)},
	}
	resp := &datasource.ReadResponse{
		State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)},
	}
	d.Read(ctx, req, resp)
	return resp.State, resp
}

// --- helpers ---

func TestOptionalString(t *testing.T) {
	if got := optionalString(nil); !got.IsNull() {
		t.Errorf("expected null for a nil pointer, got %v", got)
	}
	v := "2026-01-02T00:00:00Z"
	if got := optionalString(&v); got.ValueString() != v {
		t.Errorf("expected %q, got %q", v, got.ValueString())
	}
	empty := ""
	// An empty string from the API is a value, not an absence.
	if got := optionalString(&empty); got.IsNull() || got.ValueString() != "" {
		t.Errorf("expected an empty string value, got %v", got)
	}
}

// --- metadata & configure ---

func TestUptimeMonitorDataSources_MetadataAndConfigure(t *testing.T) {
	for _, tt := range []struct {
		ds   datasource.DataSource
		want string
	}{
		{NewUptimeMonitorStatusDataSource(), "fivenines_uptime_monitor_status"},
		{NewUptimeMonitorsDataSource(), "fivenines_uptime_monitors"},
	} {
		resp := &datasource.MetadataResponse{}
		tt.ds.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "fivenines"}, resp)
		if resp.TypeName != tt.want {
			t.Errorf("expected type name %q, got %q", tt.want, resp.TypeName)
		}
	}

	c := client.NewClient("https://example.com", "key")

	tests := []struct {
		name         string
		providerData interface{}
		wantError    bool
	}{
		// Terraform calls Configure with nil data before the provider is
		// configured; that must be a quiet no-op, not an error.
		{name: "nil provider data", providerData: nil},
		{name: "correct client", providerData: c},
		{name: "wrong type", providerData: "not a client", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, ds := range []datasource.DataSourceWithConfigure{
				&uptimeMonitorStatusDataSource{},
				&uptimeMonitorsDataSource{},
			} {
				resp := &datasource.ConfigureResponse{}
				ds.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: tt.providerData}, resp)
				if resp.Diagnostics.HasError() != tt.wantError {
					t.Errorf("%T: expected error=%v, got %v", ds, tt.wantError, resp.Diagnostics.Errors())
				}
			}
		})
	}
}

// --- uptime_monitor_status data source ---

func TestUptimeMonitorStatusDataSource_Read(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
		body       map[string]interface{}
		wantStatus string
		wantPaused bool
		wantNulls  []string
		wantErr    string
	}{
		{
			name: "up with every field populated",
			body: map[string]interface{}{
				"id": "mon-uuid", "status": "up",
				"last_check_at": "2026-01-02T00:00:00Z", "next_check_at": "2026-01-02T00:01:00Z",
				"last_error": nil, "ssl_expires_at": "2026-10-11T17:06:46Z",
			},
			wantStatus: "up",
			wantNulls:  []string{"last_error"},
		},
		{
			name:       "paused derives the paused flag",
			body:       map[string]interface{}{"id": "mon-uuid", "status": "paused"},
			wantStatus: "paused",
			wantPaused: true,
			wantNulls:  []string{"last_check_at", "next_check_at", "last_error", "ssl_expires_at"},
		},
		{
			// Every non-paused status must read as paused=false, including the
			// ones a naive "not up" check would get wrong.
			name:       "down is not paused",
			body:       map[string]interface{}{"id": "mon-uuid", "status": "down", "last_error": "connection refused"},
			wantStatus: "down",
		},
		{
			name:       "recovering is not paused",
			body:       map[string]interface{}{"id": "mon-uuid", "status": "recovering"},
			wantStatus: "recovering",
		},
		{
			name:       "a 404 surfaces as a diagnostic",
			httpStatus: http.StatusNotFound,
			body:       map[string]interface{}{"error": "Not Found"},
			wantErr:    "Error reading uptime monitor status",
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
				json.NewEncoder(w).Encode(map[string]interface{}{"uptime_monitor_status": tt.body})
			})

			state, resp := readDataSource(t, &uptimeMonitorStatusDataSource{client: c}, map[string]tftypes.Value{
				"id": tftypes.NewValue(tftypes.String, "mon-uuid"),
			})
			if gotPath != "/api/v1/uptime_monitors/mon-uuid/status" {
				t.Errorf("unexpected request path: %s", gotPath)
			}
			if tt.wantErr != "" {
				if !resp.Diagnostics.HasError() {
					t.Fatal("expected an error diagnostic")
				}
				if got := resp.Diagnostics.Errors()[0].Summary(); got != tt.wantErr {
					t.Errorf("expected summary %q, got %q", tt.wantErr, got)
				}
				// State must stay untouched so Terraform records nothing partial.
				if !state.Raw.IsNull() {
					t.Errorf("expected state to be left null, got %v", state.Raw)
				}
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
			}

			var out uptimeMonitorStatusModel
			if diags := state.Get(context.Background(), &out); diags.HasError() {
				t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
			}
			if out.ID.ValueString() != "mon-uuid" {
				t.Errorf("expected the configured id to be echoed, got %q", out.ID.ValueString())
			}
			if out.Status.ValueString() != tt.wantStatus {
				t.Errorf("expected status %q, got %q", tt.wantStatus, out.Status.ValueString())
			}
			if out.Paused.ValueBool() != tt.wantPaused {
				t.Errorf("expected paused %v, got %v", tt.wantPaused, out.Paused.ValueBool())
			}

			nullable := map[string]bool{
				"last_check_at":  out.LastCheckAt.IsNull(),
				"next_check_at":  out.NextCheckAt.IsNull(),
				"last_error":     out.LastError.IsNull(),
				"ssl_expires_at": out.SSLExpiresAt.IsNull(),
			}
			for _, name := range tt.wantNulls {
				if !nullable[name] {
					t.Errorf("expected %s to be null", name)
				}
			}
		})
	}
}

// --- uptime_monitors data source ---

func TestUptimeMonitorsDataSource_Read(t *testing.T) {
	var gotQuery url.Values
	port := 5432
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitors": []interface{}{
				map[string]interface{}{
					"id": "mon-1", "name": "API Health", "protocol": "https", "status": "paused",
					"url": "https://example.com/health", "interval_seconds": 60,
					"last_check_at": "2026-01-02T00:00:00Z", "ssl_expires_at": "2026-10-11T17:06:46Z",
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				},
				map[string]interface{}{
					"id": "mon-2", "name": "DB", "protocol": "tcp", "status": "up",
					"hostname": "db.example.com", "port": port, "interval_seconds": 300,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &uptimeMonitorsDataSource{client: c}, map[string]tftypes.Value{
		"status":        tftypes.NewValue(tftypes.String, "up"),
		"protocol":      tftypes.NewValue(tftypes.String, "https"),
		"query":         tftypes.NewValue(tftypes.String, "api"),
		"updated_since": tftypes.NewValue(tftypes.String, "2026-01-01T00:00:00Z"),
		"order":         tftypes.NewValue(tftypes.String, "name"),
		"direction":     tftypes.NewValue(tftypes.String, "asc"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}

	// Every schema filter has to reach the API, including the query -> q rename.
	for key, want := range map[string]string{
		"status": "up", "protocol": "https", "q": "api",
		"updated_since": "2026-01-01T00:00:00Z", "order": "name", "direction": "asc",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q in the request, got %q", key, want, got)
		}
	}

	var out uptimeMonitorsModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.Monitors) != 2 {
		t.Fatalf("expected 2 monitors, got %d", len(out.Monitors))
	}
	// Filters are echoed back so the data source round-trips its own config.
	if out.Query.ValueString() != "api" {
		t.Errorf("expected query to be preserved, got %q", out.Query.ValueString())
	}

	first := out.Monitors[0]
	if first.ID.ValueString() != "mon-1" || first.Name.ValueString() != "API Health" {
		t.Errorf("unexpected first monitor: %+v", first)
	}
	if !first.Paused.ValueBool() {
		t.Error("expected the paused monitor to map to paused=true")
	}
	if first.IntervalSeconds.ValueInt64() != 60 {
		t.Errorf("expected interval_seconds 60, got %d", first.IntervalSeconds.ValueInt64())
	}
	if !first.Port.IsNull() {
		t.Errorf("expected a null port for an https monitor, got %v", first.Port)
	}
	if !first.LastError.IsNull() {
		t.Errorf("expected a null last_error, got %v", first.LastError)
	}

	second := out.Monitors[1]
	if second.Paused.ValueBool() {
		t.Error("expected an up monitor to map to paused=false")
	}
	if second.Port.ValueInt64() != int64(port) {
		t.Errorf("expected port %d, got %d", port, second.Port.ValueInt64())
	}
	if second.Hostname.ValueString() != "db.example.com" {
		t.Errorf("unexpected hostname: %q", second.Hostname.ValueString())
	}
}

func TestUptimeMonitorsDataSource_Read_NoFiltersAndNoResults(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitors": []interface{}{},
			"meta":            map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &uptimeMonitorsDataSource{client: c}, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// Null config attributes must not become empty-string filters.
	for _, key := range []string{"status", "protocol", "q", "updated_since", "order", "direction"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %s to be omitted when unset", key)
		}
	}

	var out uptimeMonitorsModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.Monitors) != 0 {
		t.Errorf("expected no monitors, got %d", len(out.Monitors))
	}
	// Zero matches must serialise as [] and not null: length()/for_each/toset
	// over a null list fail, and zero matches is normal for a filtered read.
	var monitors types.List
	if diags := state.GetAttribute(context.Background(), path.Root("monitors"), &monitors); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if monitors.IsNull() {
		t.Error("expected an empty list, got null")
	}
}

func TestUptimeMonitorsDataSource_Read_Error(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "boom"})
	})

	state, resp := readDataSource(t, &uptimeMonitorsDataSource{client: c}, nil)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic for a 500")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); got != "Error listing uptime monitors" {
		t.Errorf("unexpected summary: %q", got)
	}
	if !state.Raw.IsNull() {
		t.Errorf("expected state to be left null, got %v", state.Raw)
	}
}
