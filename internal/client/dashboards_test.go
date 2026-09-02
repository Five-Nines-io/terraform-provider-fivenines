package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// --- Dashboards ---

func TestClient_GetDashboard(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/dashboards/12" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("ETag", `"dash-1-gzip"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"dashboard": map[string]interface{}{
				"id":                  12,
				"name":                "Fleet health",
				"description":         nil,
				"shared":              true,
				"share_url":           "https://fivenines.io/share/dashboard/abc",
				"section_count":       2,
				"visualization_count": 5,
				"sections": []interface{}{
					map[string]interface{}{"id": 41, "name": "Compute", "position": 0, "collapsed": false},
					map[string]interface{}{"id": 42, "name": "Storage", "position": 1, "collapsed": true},
				},
				"visualizations": []interface{}{
					map[string]interface{}{"id": 88, "metric": "cpu_usage", "chart_type": "line"},
				},
				"created_at": "2026-01-15T10:00:00Z",
				"updated_at": "2026-01-15T10:00:00Z",
			},
		})
	})

	dashboard, etag, err := c.GetDashboard(context.Background(), 12)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if etag != `"dash-1"` {
		t.Errorf("expected the -gzip suffix stripped, got %q", etag)
	}
	if dashboard.ID != 12 || dashboard.Name == nil || *dashboard.Name != "Fleet health" {
		t.Errorf("unexpected dashboard: %+v", dashboard)
	}
	if dashboard.Description != nil {
		t.Errorf("expected a null description, got %q", *dashboard.Description)
	}
	if !dashboard.Shared || dashboard.ShareURL == nil {
		t.Error("expected a shared dashboard carrying its share_url")
	}
	if len(dashboard.Sections) != 2 || dashboard.Sections[1].Name != "Storage" {
		t.Errorf("unexpected sections: %+v", dashboard.Sections)
	}
	if len(dashboard.Visualizations) != 1 || dashboard.Visualizations[0].Metric != "cpu_usage" {
		t.Errorf("unexpected visualizations: %+v", dashboard.Visualizations)
	}
}

func TestClient_CreateDashboard_OmitsAbsentDescription(t *testing.T) {
	var body map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/dashboards" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"dashboard": map[string]interface{}{"id": 12, "name": "Fleet health"},
		})
	})

	dashboard, err := c.CreateDashboard(context.Background(), CreateDashboardInput{Name: "Fleet health"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if dashboard.ID != 12 {
		t.Errorf("expected id 12, got %d", dashboard.ID)
	}

	// There is nothing to clear on a dashboard that does not exist yet, so an
	// absent description is omitted rather than sent as an explicit null.
	if _, present := body["dashboard"]["description"]; present {
		t.Errorf("expected an absent description to be omitted on create, got %v", body["dashboard"])
	}
}

func TestClient_UpdateDashboard_ClearsAbsentDescription(t *testing.T) {
	var body map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"dashboard": map[string]interface{}{"id": 12, "name": "Fleet health"},
		})
	})

	_, err := c.UpdateDashboard(context.Background(), 12, "", UpdateDashboardInput{Name: "Fleet health"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// The other half of the split: dropping `description` from a configuration
	// has to clear it, so update sends the key with an explicit null.
	value, present := body["dashboard"]["description"]
	if !present {
		t.Fatalf("expected description to be sent explicitly on update, got %v", body["dashboard"])
	}
	if value != nil {
		t.Errorf("expected a null description, got %v", value)
	}
}

func TestClient_UpdateDashboard_SendsIfMatch(t *testing.T) {
	var gotIfMatch string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotIfMatch = r.Header.Get("If-Match")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"dashboard": map[string]interface{}{"id": 12, "name": "Renamed"},
		})
	})

	_, err := c.UpdateDashboard(context.Background(), 12, `"etag-1"`, UpdateDashboardInput{Name: "Renamed"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if gotIfMatch != `"etag-1"` {
		t.Errorf("expected If-Match %q, got %q", `"etag-1"`, gotIfMatch)
	}
}

func TestClient_DeleteDashboard_LastOneRefused(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "You cannot delete your last dashboard",
		})
	})

	err := c.DeleteDashboard(context.Background(), 12)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected an *APIError, got %T", err)
	}
	if apiErr.StatusCode != 422 {
		t.Errorf("expected 422, got %d", apiErr.StatusCode)
	}
}

// --- Sections ---

func TestClient_CreateDashboardSection(t *testing.T) {
	var body map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/dashboards/12/sections" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"section": map[string]interface{}{"id": 41, "name": "Compute", "position": 2, "collapsed": false},
		})
	})

	collapsed := false
	section, err := c.CreateDashboardSection(context.Background(), 12, DashboardSectionInput{
		Name:      "Compute",
		Collapsed: &collapsed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if section.ID != 41 || section.Position != 2 {
		t.Errorf("unexpected section: %+v", section)
	}
	// An unset position must not be sent: the API reads it as "move here", and
	// sending one would reorder the dashboard on every apply.
	if _, present := body["section"]["position"]; present {
		t.Error("expected an unset position to be omitted")
	}
}

func TestClient_UpdateDashboardSection_SendsPosition(t *testing.T) {
	var body map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/dashboards/12/sections/41" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"section": map[string]interface{}{"id": 41, "name": "Compute", "position": 0, "collapsed": true},
		})
	})

	position := int64(0)
	collapsed := true
	section, err := c.UpdateDashboardSection(context.Background(), 12, 41, DashboardSectionInput{
		Name:      "Compute",
		Collapsed: &collapsed,
		Position:  &position,
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !section.Collapsed {
		t.Error("expected the section to come back collapsed")
	}
	// Zero is a real position, not an absent one.
	if got := body["section"]["position"]; got != float64(0) {
		t.Errorf("expected position 0 to be sent, got %v", got)
	}
}

func TestClient_DeleteDashboardSection(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/dashboards/12/sections/41" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteDashboardSection(context.Background(), 12, 41); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

// --- Visualizations ---

func TestClient_GetVisualization(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/dashboards/12/visualizations/88" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("ETag", `"panel-1"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"visualization": map[string]interface{}{
				"id":          88,
				"title":       "CPU usage",
				"description": nil,
				"metric":      "cpu_usage",
				"target_kind": "hosts",
				"chart_type":  "line",
				"section":     "Compute",
				"layout":      map[string]interface{}{"x": 0, "y": 0, "w": 12, "h": 6},
				"targets": map[string]interface{}{
					"hosts":           []string{"host-uuid-1"},
					"uptime_monitors": []string{},
					"tasks":           []string{},
					"network_devices": []string{},
					"ceph_clusters":   []string{},
				},
				"options": map[string]interface{}{
					"reducer": "avg", "group_by": nil, "dimensions": nil, "limit": nil,
					"stacked": false, "incident_overlay": true, "sparkline": nil, "max": nil,
				},
				"query_resources": []string{"cpu_usage"},
				"created_at":      "2026-01-15T10:00:00Z",
				"updated_at":      "2026-01-15T10:00:00Z",
			},
		})
	})

	panel, etag, err := c.GetVisualization(context.Background(), 12, 88)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if etag != `"panel-1"` {
		t.Errorf("unexpected etag %q", etag)
	}
	if panel.TargetKind != "hosts" || len(panel.Targets.Hosts) != 1 {
		t.Errorf("unexpected targets: %+v", panel.Targets)
	}
	if panel.Layout.W == nil || *panel.Layout.W != 12 {
		t.Errorf("unexpected layout: %+v", panel.Layout)
	}
	if panel.Options.Reducer == nil || *panel.Options.Reducer != "avg" {
		t.Errorf("expected reducer avg, got %+v", panel.Options.Reducer)
	}
	if panel.Options.GroupBy != nil {
		t.Error("expected an unstored group_by to read back as null")
	}
	if panel.Options.Stacked == nil || *panel.Options.Stacked {
		t.Error("expected stacked=false to survive as a real false, not a null")
	}
}

func TestClient_CreateVisualization_TargetAndOptionEncoding(t *testing.T) {
	var body map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"visualization": map[string]interface{}{"id": 88, "metric": "cpu_usage"},
		})
	})

	hosts := []string{"host-uuid-1"}
	reducer := "avg"
	_, err := c.CreateVisualization(context.Background(), 12, VisualizationInput{
		Metric:    "cpu_usage",
		ChartType: "line",
		Targets:   &VisualizationTargetsInput{Hosts: &hosts},
		Options:   &VisualizationOptions{Reducer: &reducer},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	panel := body["visualization"]

	// A target kind that was not mentioned must be omitted, not sent empty: on
	// PATCH the API replaces what it is given and leaves out what it is not.
	targets, _ := panel["targets"].(map[string]interface{})
	if _, present := targets["uptime_monitors"]; present {
		t.Error("expected an unmentioned target kind to be omitted")
	}
	if hostsSent, _ := targets["hosts"].([]interface{}); len(hostsSent) != 1 {
		t.Errorf("expected one host, got %v", targets["hosts"])
	}

	// Every option key must be present, because an explicit null is what clears
	// a setting back to the metric's default.
	options, _ := panel["options"].(map[string]interface{})
	for _, key := range []string{"reducer", "group_by", "dimensions", "limit", "stacked", "incident_overlay", "sparkline", "max"} {
		if _, present := options[key]; !present {
			t.Errorf("expected option %q to be sent", key)
		}
	}
	if options["group_by"] != nil {
		t.Errorf("expected group_by to be null, got %v", options["group_by"])
	}

	// An omitted layout must stay omitted so the dashboard places the panel.
	if _, present := panel["layout"]; present {
		t.Error("expected an unset layout to be omitted")
	}
}

func TestClient_UpdateVisualization_ClearsTargetKindWithEmptyArray(t *testing.T) {
	var body map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"visualization": map[string]interface{}{"id": 88, "metric": "cpu_usage"},
		})
	})

	empty := []string{}
	_, err := c.UpdateVisualization(context.Background(), 12, 88, "", VisualizationInput{
		Metric:  "cpu_usage",
		Targets: &VisualizationTargetsInput{Hosts: &empty},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	targets, _ := body["visualization"]["targets"].(map[string]interface{})
	hosts, present := targets["hosts"]
	if !present {
		t.Fatal("expected an explicitly emptied kind to be sent")
	}
	if list, _ := hosts.([]interface{}); len(list) != 0 {
		t.Errorf("expected an empty array, got %v", hosts)
	}
}

func TestClient_DeleteVisualization(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/dashboards/12/visualizations/88" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteVisualization(context.Background(), 12, 88); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

// --- Templates ---

func TestClient_ListDashboardTemplates(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/dashboards/templates" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"templates": []interface{}{
				map[string]interface{}{
					"slug": "postgresql", "name": "PostgreSQL", "category": "Databases",
					"target_kinds": []string{"hosts"}, "panel_count": 12, "section_count": 3,
					"available": false, "unavailable_reason": "no instance runs PostgreSQL yet",
				},
			},
			"meta": map[string]interface{}{"total": 1},
		})
	})

	templates, err := c.ListDashboardTemplates(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(templates) != 1 || templates[0].Slug != "postgresql" {
		t.Fatalf("unexpected templates: %+v", templates)
	}
	if templates[0].Available {
		t.Error("expected the template to be unavailable")
	}
	if templates[0].UnavailableReason == nil {
		t.Error("expected a reason for an unavailable template")
	}
}

func TestClient_InstantiateDashboardTemplate(t *testing.T) {
	var body map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/dashboards/templates" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"dashboard":     map[string]interface{}{"id": 12, "name": "PostgreSQL", "visualization_count": 9},
			"section":       nil,
			"created_count": 9,
			"skipped": []interface{}{
				map[string]interface{}{"title": "Replication lag", "reason": "no instance runs PostgreSQL yet"},
			},
			"skip_summary": []string{"3 panels skipped, no instance runs PostgreSQL yet"},
		})
	})

	result, err := c.InstantiateDashboardTemplate(context.Background(), InstantiateDashboardTemplateInput{
		Slug: "postgresql",
		Name: "PostgreSQL",
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if result.Dashboard.ID != 12 || result.CreatedCount != 9 {
		t.Errorf("unexpected result: %+v", result)
	}
	if len(result.Skipped) != 1 || len(result.SkipSummary) != 1 {
		t.Errorf("expected the skipped panels to be reported, got %+v", result)
	}
	if result.Section != nil {
		t.Error("expected no section on the new-dashboard path")
	}
	// The template body is not wrapped in a container, unlike every other write.
	if body["slug"] != "postgresql" {
		t.Errorf("expected an unwrapped slug, got %v", body)
	}
	if _, present := body["dashboard_id"]; present {
		t.Error("expected dashboard_id to be omitted when building a new dashboard")
	}
}

func TestClient_UpdateVisualization_SendsIfMatch(t *testing.T) {
	// Optimistic concurrency on a panel is invisible when it breaks: the write
	// still succeeds, it just stops being refused when someone else got there
	// first. Nothing else in the suite would notice the header going missing.
	var gotIfMatch string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotIfMatch = r.Header.Get("If-Match")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"visualization": map[string]interface{}{"id": 88, "metric": "cpu_usage"},
		})
	})

	_, err := c.UpdateVisualization(context.Background(), 12, 88, `"panel-1"`, VisualizationInput{Metric: "cpu_usage"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if gotIfMatch != `"panel-1"` {
		t.Errorf("expected If-Match %q, got %q", `"panel-1"`, gotIfMatch)
	}
}

func TestClient_UpdateVisualization_OmitsIfMatchWhenEtagIsEmpty(t *testing.T) {
	// The other half: a proxy that strips ETag leaves the caller with "", and an
	// `If-Match: ""` would be a permanent 412 rather than an unconditional write.
	sawHeader := true
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, sawHeader = r.Header["If-Match"]
		json.NewEncoder(w).Encode(map[string]interface{}{
			"visualization": map[string]interface{}{"id": 88, "metric": "cpu_usage"},
		})
	})

	if _, err := c.UpdateVisualization(context.Background(), 12, 88, "", VisualizationInput{Metric: "cpu_usage"}); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if sawHeader {
		t.Error("expected no If-Match header when the ETag is empty")
	}
}

func TestClient_GetVisualization_SanitizesETag(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"panel-1-gzip"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"visualization": map[string]interface{}{"id": 88, "metric": "cpu_usage"},
		})
	})

	_, etag, err := c.GetVisualization(context.Background(), 12, 88)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if etag != `"panel-1"` {
		t.Errorf("expected the -gzip suffix stripped, got %q", etag)
	}
}
