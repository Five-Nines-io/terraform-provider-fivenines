package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// newTestServer creates a test HTTP server with the given handler.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test-api-key")
	return srv, c
}

// --- Auth & Headers ---

func TestClient_AuthHeader(t *testing.T) {
	var gotAuth string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"instances": []interface{}{}, "meta": map[string]int{"count": 0, "total": 0, "offset": 0}})
	})

	c.ListInstances(context.Background())
	if gotAuth != "Bearer test-api-key" {
		t.Errorf("expected Authorization 'Bearer test-api-key', got %q", gotAuth)
	}
}

func TestClient_UserAgent(t *testing.T) {
	var gotUA string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"instances": []interface{}{}, "meta": map[string]int{"count": 0, "total": 0, "offset": 0}})
	})

	c.ListInstances(context.Background())
	if gotUA != userAgent {
		t.Errorf("expected User-Agent %q, got %q", userAgent, gotUA)
	}
}

// --- Instances ---

func TestClient_GetInstance(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/instances/abc-123" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("ETag", `"etag-1"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{
				"id":           "abc-123",
				"display_name": "web-1",
				"hostname":     "web-1.example.com",
				"enabled":      true,
				"created_at":   "2026-01-01T00:00:00Z",
				"updated_at":   "2026-01-01T00:00:00Z",
			},
		})
	})

	inst, etag, err := c.GetInstance(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"etag-1"` {
		t.Errorf("expected etag %q, got %q", `"etag-1"`, etag)
	}
	if inst.ID != "abc-123" {
		t.Errorf("expected ID abc-123, got %s", inst.ID)
	}
	if inst.DisplayName != "web-1" {
		t.Errorf("expected display_name web-1, got %s", inst.DisplayName)
	}
}

func TestClient_CreateInstance(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{
				"id":           "new-uuid",
				"display_name": "db-1",
				"enabled":      true,
				"created_at":   "2026-01-01T00:00:00Z",
				"updated_at":   "2026-01-01T00:00:00Z",
			},
		})
	})

	enabled := true
	inst, err := c.CreateInstance(context.Background(), CreateInstanceInput{
		DisplayName: "db-1",
		Enabled:     &enabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.ID != "new-uuid" {
		t.Errorf("expected ID new-uuid, got %s", inst.ID)
	}
	// Verify request body wrapping
	if gotBody["instance"] == nil {
		t.Fatal("expected request body to have 'instance' key")
	}
}

func TestClient_UpdateInstance_ETag(t *testing.T) {
	var gotIfMatch string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotIfMatch = r.Header.Get("If-Match")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{
				"id":         "abc-123",
				"display_name": "updated",
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	name := "updated"
	c.UpdateInstance(context.Background(), "abc-123", `"etag-1"`, UpdateInstanceInput{DisplayName: &name})
	if gotIfMatch != `"etag-1"` {
		t.Errorf("expected If-Match %q, got %q", `"etag-1"`, gotIfMatch)
	}
}

func TestClient_DeleteInstance_202(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	err := c.DeleteInstance(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("expected no error for 202, got: %v", err)
	}
}

func TestClient_DeleteInstance_204(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	err := c.DeleteInstance(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("expected no error for 204, got: %v", err)
	}
}

func TestClient_ListInstances_Pagination(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&requestCount, 1)
		if page == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"instances": []map[string]interface{}{
					{"id": "a", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
				},
				"meta": map[string]int{"count": 1, "total": 2, "offset": 0},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"instances": []map[string]interface{}{
					{"id": "b", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
				},
				"meta": map[string]int{"count": 1, "total": 2, "offset": 1},
			})
		}
	})

	instances, err := c.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Errorf("expected 2 instances, got %d", len(instances))
	}
}

// --- Tasks ---

func TestClient_GetTask(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/task-uuid" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"task-etag"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task": map[string]interface{}{
				"id":            "task-uuid",
				"name":          "nightly-backup",
				"schedule_type": "cron",
				"schedule":      "0 2 * * *",
				"status":        "active",
				"ping_key":      "pk_abc123",
				"ping_url":      "https://fivenines.io/ping/pk_abc123",
				"created_at":    "2026-01-01T00:00:00Z",
				"updated_at":    "2026-01-01T00:00:00Z",
			},
		})
	})

	task, etag, err := c.GetTask(context.Background(), "task-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"task-etag"` {
		t.Errorf("expected etag %q, got %q", `"task-etag"`, etag)
	}
	if task.Name != "nightly-backup" {
		t.Errorf("expected name nightly-backup, got %s", task.Name)
	}
	if task.PingKey != "pk_abc123" {
		t.Errorf("expected ping_key pk_abc123, got %s", task.PingKey)
	}
}

func TestClient_CreateTask(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task": map[string]interface{}{
				"id":            "new-task",
				"name":          "health-check",
				"schedule_type": "interval",
				"status":        "active",
				"created_at":    "2026-01-01T00:00:00Z",
				"updated_at":    "2026-01-01T00:00:00Z",
			},
		})
	})

	interval := int64(300)
	task, err := c.CreateTask(context.Background(), CreateTaskInput{
		Name:            "health-check",
		ScheduleType:    "interval",
		IntervalSeconds: &interval,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ID != "new-task" {
		t.Errorf("expected ID new-task, got %s", task.ID)
	}
}

func TestClient_PauseTask(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/tasks/task-uuid/pause" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	err := c.PauseTask(context.Background(), "task-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Workflows ---

func TestClient_GetWorkflow_WithVersions(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"wf-etag"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflow": map[string]interface{}{
				"id":          42,
				"name":        "CPU Alert",
				"status":      "active",
				"description": "Alerts on high CPU",
				"created_at":  "2026-01-01T00:00:00Z",
				"updated_at":  "2026-01-01T00:00:00Z",
			},
			"versions": []map[string]interface{}{
				{"id": 1, "version_number": 1, "created_at": "2026-01-01T00:00:00Z"},
				{"id": 2, "version_number": 2, "created_at": "2026-01-02T00:00:00Z"},
			},
		})
	})

	wf, _, err := c.GetWorkflow(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "CPU Alert" {
		t.Errorf("expected name CPU Alert, got %s", wf.Name)
	}
	if len(wf.Versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(wf.Versions))
	}
}

func TestClient_ListWorkflows_FiltersArchived(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []map[string]interface{}{
				{"id": 1, "name": "active-wf", "status": "active", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
				{"id": 2, "name": "archived-wf", "status": "archived", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
			},
			"meta": map[string]int{"count": 2, "total": 2, "offset": 0},
		})
	})

	workflows, err := c.ListWorkflows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workflows) != 1 {
		t.Fatalf("expected 1 workflow (archived filtered), got %d", len(workflows))
	}
	if workflows[0].Name != "active-wf" {
		t.Errorf("expected active-wf, got %s", workflows[0].Name)
	}
}

func TestClient_CreateWorkflowVersion(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/workflows/42/versions" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version": map[string]interface{}{
				"id":              10,
				"version_number":  3,
				"execution_graph": map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}},
				"created_at":      "2026-01-01T00:00:00Z",
			},
		})
	})

	ver, err := c.CreateWorkflowVersion(context.Background(), 42, CreateWorkflowVersionInput{
		ExecutionGraph: map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver.VersionNumber != 3 {
		t.Errorf("expected version_number 3, got %d", ver.VersionNumber)
	}
}

func TestClient_PublishWorkflowVersion(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/workflows/42/publish" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["version_id"] != float64(10) {
			t.Errorf("expected version_id 10, got %v", body["version_id"])
		}
		w.WriteHeader(http.StatusOK)
	})

	err := c.PublishWorkflowVersion(context.Background(), 42, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Uptime Monitors ---

func TestClient_GetUptimeMonitor(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/uptime_monitors/mon-uuid" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"mon-etag"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{
				"id":                    "mon-uuid",
				"name":                  "API Health",
				"protocol":              "https",
				"status":                "up",
				"url":                   "https://api.example.com",
				"interval_seconds":      60,
				"timeout_seconds":       15,
				"confirmation_count":    1,
				"follow_redirects":      true,
				"expected_status_codes": []int{200},
				"probe_region_ids":      []int{1, 2},
				"dns_record_type":       "",
				"dns_expected_records":  []string{},
				"custom_headers":        map[string]string{},
				"custom_body":           "",
				"content_type":          "",
				"recovery_count":        1,
				"created_at":            "2026-01-01T00:00:00Z",
				"updated_at":            "2026-01-01T00:00:00Z",
			},
		})
	})

	mon, etag, err := c.GetUptimeMonitor(context.Background(), "mon-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"mon-etag"` {
		t.Errorf("expected etag %q, got %q", `"mon-etag"`, etag)
	}
	if mon.Name != "API Health" {
		t.Errorf("expected name API Health, got %s", mon.Name)
	}
	if mon.RecoveryCount != 1 {
		t.Errorf("expected recovery_count 1, got %d", mon.RecoveryCount)
	}
}

func TestClient_CreateUptimeMonitor_DNS(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{
				"id":                   "dns-uuid",
				"name":                 "DNS Check",
				"protocol":             "dns",
				"status":               "unknown",
				"hostname":             "example.com",
				"dns_record_type":      "A",
				"dns_expected_records": []string{"1.2.3.4"},
				"recovery_count":       1,
				"created_at":           "2026-01-01T00:00:00Z",
				"updated_at":           "2026-01-01T00:00:00Z",
			},
		})
	})

	mon, err := c.CreateUptimeMonitor(context.Background(), CreateUptimeMonitorInput{
		Name:               "DNS Check",
		Protocol:           "dns",
		Hostname:           "example.com",
		DNSRecordType:      "A",
		DNSExpectedRecords: []string{"1.2.3.4"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mon.DNSRecordType != "A" {
		t.Errorf("expected dns_record_type A, got %s", mon.DNSRecordType)
	}
	// Verify body includes DNS fields
	monitor := gotBody["uptime_monitor"].(map[string]interface{})
	if monitor["dns_record_type"] != "A" {
		t.Errorf("expected dns_record_type in body, got %v", monitor["dns_record_type"])
	}
}

// --- Probe Regions ---

func TestClient_ListProbeRegions(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/probe_regions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"probe_regions": []map[string]interface{}{
				{"id": 1, "name": "US East", "slug": "us-east", "status": "active"},
				{"id": 2, "name": "EU West", "slug": "eu-west", "status": "active"},
			},
		})
	})

	regions, err := c.ListProbeRegions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(regions) != 2 {
		t.Errorf("expected 2 regions, got %d", len(regions))
	}
}

// --- Integrations ---

func TestClient_ListIntegrations(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integrations": []map[string]interface{}{
				{"id": 1, "type": "SlackIntegration", "name": "Slack", "provider": "slack", "enabled": true, "verified": true, "created_at": "2026-01-01T00:00:00Z"},
			},
		})
	})

	integrations, err := c.ListIntegrations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(integrations) != 1 {
		t.Errorf("expected 1 integration, got %d", len(integrations))
	}
	if integrations[0].Provider != "slack" {
		t.Errorf("expected provider slack, got %s", integrations[0].Provider)
	}
}

// --- Error Handling ---

func TestClient_APIError_404(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})

	_, _, err := c.GetInstance(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", apiErr.StatusCode)
	}
}

func TestClient_APIError_422(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"Name can't be blank"}})
	})

	_, err := c.CreateInstance(context.Background(), CreateInstanceInput{})
	if err == nil {
		t.Fatal("expected error for 422")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 422 {
		t.Errorf("expected status 422, got %d", apiErr.StatusCode)
	}
	if len(apiErr.Errors) != 1 {
		t.Errorf("expected 1 validation error, got %d", len(apiErr.Errors))
	}
}

// --- Rate Limiting ---

func TestClient_RateLimit_Retry(t *testing.T) {
	var attempts int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&attempts, 1)
		if attempt == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"probe_regions": []map[string]interface{}{},
		})
	})

	regions, err := c.ListProbeRegions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if regions == nil {
		t.Error("expected non-nil regions after retry")
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts (1 retry), got %d", atomic.LoadInt32(&attempts))
	}
}

// --- ETag 412 Precondition Failed ---

func TestClient_IsPreconditionFailed(t *testing.T) {
	if IsPreconditionFailed(nil) {
		t.Error("expected false for nil error")
	}
	if IsPreconditionFailed(fmt.Errorf("some error")) {
		t.Error("expected false for non-API error")
	}
	if !IsPreconditionFailed(&APIError{StatusCode: 412}) {
		t.Error("expected true for 412 error")
	}
	if IsPreconditionFailed(&APIError{StatusCode: 409}) {
		t.Error("expected false for 409 error")
	}
}

func TestClient_Update_412_Retry(t *testing.T) {
	var attempts int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, atomic.LoadInt32(&attempts)))
			json.NewEncoder(w).Encode(map[string]interface{}{
				"instance": map[string]interface{}{
					"id": "abc-123", "display_name": "test",
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
				},
			})
			return
		}
		attempt := atomic.AddInt32(&attempts, 1)
		if attempt == 1 {
			w.WriteHeader(http.StatusPreconditionFailed)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "stale ETag"})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{
				"id": "abc-123", "display_name": "updated",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	// First update will get 412, caller should retry with fresh ETag
	name := "updated"
	_, err := c.UpdateInstance(context.Background(), "abc-123", `"stale"`, UpdateInstanceInput{DisplayName: &name})
	// This returns the 412 error — the retry logic is in the resource layer
	if err == nil {
		t.Fatal("expected 412 error from client (retry is at resource layer)")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != 412 {
		t.Fatalf("expected 412 error, got: %v", err)
	}
}

// --- Incidents ---

func TestClient_ListIncidents(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{
				{
					"id": 1, "title": "High CPU", "status": "triggered",
					"summary": "CPU above 90%", "created_at": "2026-01-01T00:00:00Z",
					"updated_at": "2026-01-01T00:00:00Z",
				},
				{
					"id": 2, "title": "Disk Full", "status": "resolved",
					"summary": "Disk at 95%", "created_at": "2026-01-01T00:00:00Z",
					"updated_at": "2026-01-02T00:00:00Z",
				},
			},
			"meta": map[string]int{"count": 2, "total": 2, "offset": 0},
		})
	})

	incidents, err := c.ListIncidents(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 2 {
		t.Errorf("expected 2 incidents, got %d", len(incidents))
	}
	if incidents[0].Title != "High CPU" {
		t.Errorf("expected title 'High CPU', got %s", incidents[0].Title)
	}
	if incidents[1].Status != "resolved" {
		t.Errorf("expected status 'resolved', got %s", incidents[1].Status)
	}
}

func TestClient_GetIncident(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/incidents/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incident": map[string]interface{}{
				"id": 42, "title": "High CPU", "status": "acknowledged",
				"summary": "CPU above 90%", "host_id": "host-uuid",
				"workflow_id": 10, "duration_seconds": 3600,
				"started_at": "2026-01-01T00:00:00Z",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	inc, err := c.GetIncident(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inc.Title != "High CPU" {
		t.Errorf("expected title 'High CPU', got %s", inc.Title)
	}
	if inc.HostID == nil || *inc.HostID != "host-uuid" {
		t.Errorf("expected host_id 'host-uuid', got %v", inc.HostID)
	}
	if inc.DurationSeconds == nil || *inc.DurationSeconds != 3600 {
		t.Errorf("expected duration_seconds 3600, got %v", inc.DurationSeconds)
	}
}

func TestClient_RateLimit_ContextCancellation(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := c.ListProbeRegions(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
