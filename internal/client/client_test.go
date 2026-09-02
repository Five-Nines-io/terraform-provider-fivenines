package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestSanitizeETag(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`"abc123"`, `"abc123"`},          // normal ETag, no change
		{`"abc123-gzip"`, `"abc123"`},     // Nginx gzip suffix stripped
		{"", ""},                          // empty
		{`abc123`, `abc123`},              // no quotes, no change
		{`W/"abc123"`, `W/"abc123"`},      // weak ETag, no change
		{`"abc-gzip-gzip"`, `"abc-gzip"`}, // only last -gzip" stripped
	}
	for _, tt := range tests {
		got := sanitizeETag(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeETag(%q) = %q, want %q", tt.input, got, tt.want)
		}
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
				"id":           "abc-123",
				"display_name": "updated",
				"created_at":   "2026-01-01T00:00:00Z",
				"updated_at":   "2026-01-01T00:00:00Z",
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

func TestClient_UpdateTask_ScheduleType(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/tasks/task-uuid" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task": map[string]interface{}{
				"id":               "task-uuid",
				"name":             "nightly-backup",
				"schedule_type":    "interval",
				"interval_seconds": 300,
				"status":           "active",
				"created_at":       "2026-01-01T00:00:00Z",
				"updated_at":       "2026-01-01T00:00:00Z",
			},
		})
	})

	scheduleType := "interval"
	interval := int64(300)
	task, err := c.UpdateTask(context.Background(), "task-uuid", `"task-etag"`, UpdateTaskInput{
		ScheduleType:    &scheduleType,
		IntervalSeconds: &interval,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gotBody["task"]["schedule_type"]; got != "interval" {
		t.Errorf("expected schedule_type interval in request body, got %v", got)
	}
	if task.ScheduleType != "interval" {
		t.Errorf("expected schedule_type interval, got %s", task.ScheduleType)
	}
}

// A nil ScheduleType must stay out of the PATCH body entirely — sending an empty
// schedule_type would fail the API's enum validation on an unrelated update.
func TestClient_UpdateTask_OmitsUnsetScheduleType(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task": map[string]interface{}{
				"id":            "task-uuid",
				"name":          "renamed",
				"schedule_type": "cron",
				"schedule":      "0 2 * * *",
				"status":        "active",
				"created_at":    "2026-01-01T00:00:00Z",
				"updated_at":    "2026-01-01T00:00:00Z",
			},
		})
	})

	name := "renamed"
	if _, err := c.UpdateTask(context.Background(), "task-uuid", "", UpdateTaskInput{Name: &name}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := gotBody["task"]["schedule_type"]; present {
		t.Errorf("expected schedule_type to be omitted, got %v", gotBody["task"]["schedule_type"])
	}
}

func TestClient_PauseTask(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/tasks/task-uuid/pause" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task": map[string]interface{}{
				"id":            "task-uuid",
				"name":          "nightly-backup",
				"schedule_type": "cron",
				"schedule":      "0 2 * * *",
				"status":        "paused",
				"created_at":    "2026-01-01T00:00:00Z",
				"updated_at":    "2026-01-01T00:00:00Z",
			},
		})
	})

	task, err := c.PauseTask(context.Background(), "task-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != "paused" {
		t.Errorf("expected status paused, got %s", task.Status)
	}
}

// A 200 whose body is empty or names a different task must not reach state — writing
// it would blank the resource ID and lose Terraform's handle on the live task.
func TestClient_TaskAction_RejectsMismatchedTask(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]interface{}
	}{
		{"null task", map[string]interface{}{"task": nil}},
		{"empty object", map[string]interface{}{}},
		{"different id", map[string]interface{}{"task": map[string]interface{}{"id": "someone-elses-task"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(tc.body)
			})

			task, err := c.PauseTask(context.Background(), "task-uuid")
			if err == nil {
				t.Fatalf("expected an error, got task %+v", task)
			}
			if task != nil {
				t.Errorf("expected nil task alongside the error, got %+v", task)
			}
		})
	}
}

// Resume recomputes expected_ping_at server-side. The client must return the
// rendered task so state does not keep the pre-resume deadline.
func TestClient_ResumeTask_ReturnsRecomputedTask(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/tasks/task-uuid/resume" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task": map[string]interface{}{
				"id":               "task-uuid",
				"name":             "nightly-backup",
				"schedule_type":    "cron",
				"schedule":         "0 2 * * *",
				"status":           "active",
				"expected_ping_at": "2026-02-01T02:00:00Z",
				"created_at":       "2026-01-01T00:00:00Z",
				"updated_at":       "2026-02-01T00:00:00Z",
			},
		})
	})

	task, err := c.ResumeTask(context.Background(), "task-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != "active" {
		t.Errorf("expected status active, got %s", task.Status)
	}
	if task.ExpectedPingAt == nil || *task.ExpectedPingAt != "2026-02-01T02:00:00Z" {
		t.Errorf("expected recomputed expected_ping_at from the resume response, got %v", task.ExpectedPingAt)
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

// --- Network Devices ---

func TestClient_CreateNetworkDevice(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"network_device": map[string]interface{}{
				"id": "dev-uuid", "name": "Core Switch", "ip_address": "192.168.1.1",
				"device_type": "switch", "snmp_version": "v2c", "polling_interval": 60,
				"status": "unknown", "maintenance_mode": false,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	dev, err := c.CreateNetworkDevice(context.Background(), CreateNetworkDeviceInput{
		Name: "Core Switch", IPAddress: "192.168.1.1", SNMPVersion: "v2c", SNMPCommunity: "public",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dev.ID != "dev-uuid" {
		t.Errorf("expected ID dev-uuid, got %s", dev.ID)
	}
}

func TestClient_GetNetworkDevice(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/network_devices/dev-uuid" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"dev-etag"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"network_device": map[string]interface{}{
				"id": "dev-uuid", "name": "Core Switch", "ip_address": "192.168.1.1",
				"device_type": "switch", "snmp_version": "v2c", "polling_interval": 60,
				"status": "up", "maintenance_mode": false, "vendor": "Cisco", "model": "2960",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	dev, etag, err := c.GetNetworkDevice(context.Background(), "dev-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"dev-etag"` {
		t.Errorf("expected etag %q, got %q", `"dev-etag"`, etag)
	}
	if dev.Vendor != "Cisco" {
		t.Errorf("expected vendor Cisco, got %s", dev.Vendor)
	}
}

func TestClient_DeleteNetworkDevice_202(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	err := c.DeleteNetworkDevice(context.Background(), "dev-uuid")
	if err != nil {
		t.Fatalf("expected no error for 202, got: %v", err)
	}
}

func TestClient_EnterMaintenanceNetworkDevice(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/network_devices/dev-uuid/enter_maintenance" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"network_device": map[string]interface{}{"id": "dev-uuid", "maintenance_mode": true},
		})
	})
	err := c.EnterMaintenanceNetworkDevice(context.Background(), "dev-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Status Pages ---

func TestClient_CreateStatusPage(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status_page": map[string]interface{}{
				"id": 1, "name": "Service Status", "slug": "abc1",
				"public": true, "uptime": true, "theme_variant": "system",
				"items":      []interface{}{},
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	pub := true
	page, err := c.CreateStatusPage(context.Background(), CreateStatusPageInput{
		Name: "Service Status", Public: &pub,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Slug != "abc1" {
		t.Errorf("expected slug abc1, got %s", page.Slug)
	}
}

func TestClient_GetStatusPage(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status_pages/1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"sp-etag"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status_page": map[string]interface{}{
				"id": 1, "name": "Service Status", "slug": "abc1",
				"public": true, "uptime": true, "theme_variant": "dark",
				"items": []map[string]interface{}{
					{"item_type": "Host", "item_id": "host-uuid", "position": 0},
					{"item_type": "UptimeMonitor", "item_id": "mon-uuid", "position": 1},
				},
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	page, etag, err := c.GetStatusPage(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"sp-etag"` {
		t.Errorf("expected etag %q, got %q", `"sp-etag"`, etag)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(page.Items))
	}
	if page.Items[0].ItemType != "Host" {
		t.Errorf("expected first item type Host, got %s", page.Items[0].ItemType)
	}
	if page.ThemeVariant != "dark" {
		t.Errorf("expected theme_variant dark, got %s", page.ThemeVariant)
	}
}

func TestClient_DeleteStatusPage(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	err := c.DeleteStatusPage(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected no error for 204, got: %v", err)
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

// --- Security: vulnerabilities ---

func TestClient_ListVulnerabilities_PaginationAndFilters(t *testing.T) {
	var gotQueries []string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/vulnerabilities" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotQueries = append(gotQueries, r.URL.RawQuery)

		page := r.URL.Query().Get("page")
		finding := map[string]interface{}{
			"id":                84213,
			"host_id":           "host-uuid",
			"host_name":         "web-01",
			"ecosystem":         "Ubuntu:22.04",
			"package_name":      "openssl",
			"cve_ids":           []string{"CVE-2024-2511"},
			"cvss_score":        9.8,
			"severity":          "Critical",
			"patchable":         true,
			"fix_state":         "fixed",
			"fix_version":       "3.0.2-0ubuntu1.15",
			"detected_at":       "2026-08-30T12:00:00Z",
			"page_marker":       page,
			"advisory_url":      "https://ubuntu.com/security/CVE-2024-2511",
			"summary":           "openssl: unbounded memory growth",
			"vendor":            "Canonical",
			"vendor_note":       "fixed in 22.04 LTS",
			"installed_version": "3.0.2-0ubuntu1.10",
			"vulnerability_id":  "UBUNTU-CVE-2024-2511",
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{finding},
			"scan": map[string]interface{}{
				"oldest_checked_at":       "2026-08-29T00:00:00Z",
				"instances_never_checked": 2,
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 2, "total_count": 2, "per_page": 100},
		})
	})

	patchable := true
	list, err := c.ListVulnerabilities(context.Background(), VulnerabilityFilters{
		Severity:  []string{"Critical", "High"},
		Patchable: &patchable,
		Ecosystem: "Ubuntu:22.04",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// total_pages is 2, so both pages must be fetched.
	if len(gotQueries) != 2 {
		t.Fatalf("expected 2 requests, got %d: %v", len(gotQueries), gotQueries)
	}
	if len(list.Vulnerabilities) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(list.Vulnerabilities))
	}

	q, err := url.ParseQuery(gotQueries[0])
	if err != nil {
		t.Fatalf("parsing query: %v", err)
	}
	if got := q.Get("severity"); got != "Critical,High" {
		t.Errorf("expected severity 'Critical,High', got %q", got)
	}
	if got := q.Get("patchable"); got != "true" {
		t.Errorf("expected patchable 'true', got %q", got)
	}
	if got := q.Get("ecosystem"); got != "Ubuntu:22.04" {
		t.Errorf("expected ecosystem 'Ubuntu:22.04', got %q", got)
	}
	if got := q.Get("per_page"); got != "100" {
		t.Errorf("expected per_page '100', got %q", got)
	}
	if got := q.Get("page"); got != "1" {
		t.Errorf("expected page '1', got %q", got)
	}
	if q2, _ := url.ParseQuery(gotQueries[1]); q2.Get("page") != "2" {
		t.Errorf("expected second request on page 2, got %q", q2.Get("page"))
	}

	v := list.Vulnerabilities[0]
	if v.Severity != "Critical" || v.CVSSScore == nil || *v.CVSSScore != 9.8 {
		t.Errorf("unexpected finding: %+v", v)
	}
	if len(v.CVEIDs) != 1 || v.CVEIDs[0] != "CVE-2024-2511" {
		t.Errorf("expected one CVE alias, got %v", v.CVEIDs)
	}
	if v.HostName == nil || *v.HostName != "web-01" {
		t.Errorf("expected host_name web-01, got %v", v.HostName)
	}
	if list.Scan == nil || list.Scan.InstancesNeverChecked == nil || *list.Scan.InstancesNeverChecked != 2 {
		t.Errorf("expected scan.instances_never_checked 2, got %+v", list.Scan)
	}
}

// An unset filter must never reach the API: it rejects an empty value with a
// 400 rather than answering an unfiltered list.
func TestClient_ListVulnerabilities_OmitsUnsetFilters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{},
			"meta":            map[string]int{"current_page": 1, "total_pages": 0, "total_count": 0, "per_page": 100},
		})
	})

	if _, err := c.ListVulnerabilities(context.Background(), VulnerabilityFilters{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, key := range []string{"severity", "patchable", "fix_state", "package_name", "vulnerability_id", "ecosystem", "q"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %q to be omitted, got %q", key, gotQuery.Get(key))
		}
	}
}

// A scanned subject with nothing wrong is an empty list, and it must stay
// distinguishable from the refusal below.
func TestClient_ListVulnerabilities_EmptyIsNotNil(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{},
			"scan":            map[string]interface{}{"oldest_checked_at": "2026-08-30T12:00:00Z", "instances_never_checked": 0},
			"meta":            map[string]int{"current_page": 1, "total_pages": 0, "total_count": 0, "per_page": 100},
		})
	})

	list, err := c.ListVulnerabilities(context.Background(), VulnerabilityFilters{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities == nil {
		t.Fatal("expected an empty non-nil slice for a scanned fleet with no findings")
	}
	if len(list.Vulnerabilities) != 0 {
		t.Errorf("expected 0 findings, got %d", len(list.Vulnerabilities))
	}
}

// The refusal: a never-scanned instance answers `vulnerabilities: null` beside
// `meta: null`, and that must not collapse into an empty list.
func TestClient_ListInstanceVulnerabilities_NeverScanned(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/instances/host-uuid/vulnerabilities" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": nil,
			"scan":            map[string]interface{}{"last_checked_at": nil, "never_checked": true},
			"meta":            nil,
		})
	})

	list, err := c.ListInstanceVulnerabilities(context.Background(), "host-uuid", VulnerabilityFilters{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities != nil {
		t.Errorf("expected nil findings for a never-scanned instance, got %v", list.Vulnerabilities)
	}
	if list.Scan == nil || list.Scan.NeverChecked == nil || !*list.Scan.NeverChecked {
		t.Errorf("expected scan.never_checked true, got %+v", list.Scan)
	}
}

// `ecosystem` is an org-wide-only filter; the nested routes 400 on it.
func TestClient_ListInstanceVulnerabilities_DropsEcosystemFilter(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{},
			"scan":            map[string]interface{}{"last_checked_at": "2026-08-30T12:00:00Z", "never_checked": false},
			"meta":            map[string]int{"current_page": 1, "total_pages": 0, "total_count": 0, "per_page": 100},
		})
	})

	_, err := c.ListInstanceVulnerabilities(context.Background(), "host-uuid", VulnerabilityFilters{
		Ecosystem: "Ubuntu:22.04",
		Severity:  []string{"Critical"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := gotQuery["ecosystem"]; ok {
		t.Error("expected ecosystem to be dropped on the nested route")
	}
	if got := gotQuery.Get("severity"); got != "Critical" {
		t.Errorf("expected severity to survive, got %q", got)
	}
}

// A non-scanned image refuses the same way, and returns the image so a caller
// can tell WHY there are no findings.
func TestClient_ListDockerImageVulnerabilities_NotScanned(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/docker_images/image-uuid/vulnerabilities" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_image": map[string]interface{}{
				"id":                           "image-uuid",
				"state":                        "pending",
				"state_reason":                 "inventory not received",
				"countable":                    false,
				"vulnerability_count":          nil,
				"critical_vulnerability_count": nil,
				"packages_truncated":           false,
				"finding_count_is_floor":       false,
			},
			"vulnerabilities": nil,
			"meta":            nil,
		})
	})

	list, err := c.ListDockerImageVulnerabilities(context.Background(), "image-uuid", VulnerabilityFilters{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities != nil {
		t.Errorf("expected nil findings for a pending image, got %v", list.Vulnerabilities)
	}
	if list.DockerImage == nil {
		t.Fatal("expected the image to come back on the refusal")
	}
	if list.DockerImage.State != "pending" {
		t.Errorf("expected state pending, got %q", list.DockerImage.State)
	}
	if list.DockerImage.VulnerabilityCount != nil {
		t.Errorf("expected a null count on a non-scanned image, got %d", *list.DockerImage.VulnerabilityCount)
	}
}

// A scan that moves mid-request refuses on a later page too, and the partial
// set is discarded rather than reported as complete.
func TestClient_ListDockerImageVulnerabilities_RefusesMidPage(t *testing.T) {
	var calls int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"docker_image":    map[string]interface{}{"id": "image-uuid", "state": "scanned", "countable": true},
				"vulnerabilities": []interface{}{map[string]interface{}{"id": 1, "package_name": "openssl", "severity": "Critical"}},
				"meta":            map[string]int{"current_page": 1, "total_pages": 2, "total_count": 2, "per_page": 100},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_image":    map[string]interface{}{"id": "image-uuid", "state": "pending", "countable": false},
			"vulnerabilities": nil,
			"meta":            nil,
		})
	})

	list, err := c.ListDockerImageVulnerabilities(context.Background(), "image-uuid", VulnerabilityFilters{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities != nil {
		t.Errorf("expected the partial page to be discarded, got %d findings", len(list.Vulnerabilities))
	}
	// The image reported must be the one the refusal describes, not the
	// "scanned" snapshot page 1 carried.
	if list.DockerImage == nil || list.DockerImage.State != "pending" {
		t.Errorf("expected the refusal's own image state, got %+v", list.DockerImage)
	}
}

// The plan gate answers 403, never an empty list.
func TestClient_ListVulnerabilities_Forbidden(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Vulnerability details (CVEs, scores, fix versions) require the Pro plan or above.",
		})
	})

	list, err := c.ListVulnerabilities(context.Background(), VulnerabilityFilters{})
	if err == nil {
		t.Fatalf("expected an error, got %+v", list)
	}
	if !IsForbidden(err) {
		t.Errorf("expected IsForbidden to be true for %v", err)
	}
	if IsForbidden(fmt.Errorf("network down")) {
		t.Error("expected IsForbidden to be false for a non-API error")
	}
}

// --- Security: container images ---

func TestClient_ListDockerImages(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/docker_images" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_images": []interface{}{
				map[string]interface{}{
					"id":                           "image-uuid",
					"organization_id":              42,
					"image_id":                     "sha256:1f2e3d",
					"short_digest":                 "1f2e3d4c5b6a",
					"display_name":                 "nginx:1.27",
					"tags":                         []string{"nginx:1.27"},
					"repo_digests":                 []string{"nginx@sha256:1f2e3d"},
					"distro":                       "debian:12",
					"ecosystem":                    "Debian:12",
					"state":                        "scanned",
					"countable":                    true,
					"vulnerability_count":          12,
					"critical_vulnerability_count": 3,
					"packages_truncated":           true,
					"finding_count_is_floor":       true,
					"last_seen_at":                 "2026-08-30T12:00:00Z",
					"running_host_count":           4,
				},
				map[string]interface{}{
					"id":                           "image-uuid-2",
					"state":                        "unscannable",
					"state_reason":                 "extraction failed",
					"state_error_type":             "api_error",
					"countable":                    false,
					"vulnerability_count":          nil,
					"critical_vulnerability_count": nil,
				},
			},
			"posture": map[string]int{"pending": 1, "scanned": 7, "unsupported": 2, "unscannable": 1},
			"meta":    map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	truncated := true
	list, err := c.ListDockerImages(context.Background(), DockerImageFilters{
		State:             "scanned",
		PackagesTruncated: &truncated,
		Query:             "nginx",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := gotQuery.Get("state"); got != "scanned" {
		t.Errorf("expected state 'scanned', got %q", got)
	}
	if got := gotQuery.Get("packages_truncated"); got != "true" {
		t.Errorf("expected packages_truncated 'true', got %q", got)
	}
	if got := gotQuery.Get("q"); got != "nginx" {
		t.Errorf("expected q 'nginx', got %q", got)
	}

	if len(list.Images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(list.Images))
	}
	scanned := list.Images[0]
	if scanned.VulnerabilityCount == nil || *scanned.VulnerabilityCount != 12 {
		t.Errorf("expected 12 vulnerabilities, got %v", scanned.VulnerabilityCount)
	}
	if !scanned.FindingCountIsFloor || !scanned.PackagesTruncated {
		t.Error("expected the truncated image's count to be marked a floor")
	}
	if scanned.RunningHostCount == nil || *scanned.RunningHostCount != 4 {
		t.Errorf("expected running_host_count 4, got %v", scanned.RunningHostCount)
	}

	// The honesty contract: a non-scanned image carries no count at all.
	unscannable := list.Images[1]
	if unscannable.VulnerabilityCount != nil || unscannable.CriticalVulnerabilityCount != nil {
		t.Error("expected null counts on an unscannable image")
	}
	if unscannable.Countable {
		t.Error("expected countable false on an unscannable image")
	}

	if list.Posture.Pending != 1 || list.Posture.Scanned != 7 || list.Posture.Unsupported != 2 || list.Posture.Unscannable != 1 {
		t.Errorf("unexpected posture: %+v", list.Posture)
	}
}

func TestClient_ListDockerImages_Pagination(t *testing.T) {
	var calls int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&calls, 1)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_images": []interface{}{map[string]interface{}{"id": fmt.Sprintf("image-%d", page), "state": "pending"}},
			"posture":       map[string]int{"pending": 3},
			"meta":          map[string]int{"current_page": int(page), "total_pages": 3, "total_count": 3, "per_page": 100},
		})
	})

	list, err := c.ListDockerImages(context.Background(), DockerImageFilters{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Images) != 3 {
		t.Fatalf("expected 3 images across 3 pages, got %d", len(list.Images))
	}
	// Posture is org-wide and identical on every page: taken once, from the first.
	if list.Posture.Pending != 3 {
		t.Errorf("expected posture.pending 3, got %d", list.Posture.Pending)
	}
}
