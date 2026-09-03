package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
		json.NewEncoder(w).Encode(map[string]interface{}{"instances": []interface{}{}, "meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100}})
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
		json.NewEncoder(w).Encode(map[string]interface{}{"instances": []interface{}{}, "meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100}})
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

	accepted, err := c.DeleteInstance(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("expected no error for 202, got: %v", err)
	}
	if !accepted {
		t.Error("expected 202 to report an asynchronous deletion")
	}
}

func TestClient_DeleteInstance_204(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	accepted, err := c.DeleteInstance(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("expected no error for 204, got: %v", err)
	}
	if accepted {
		t.Error("expected 204 to report a completed deletion")
	}
}

func TestClient_WaitForInstanceDeletion(t *testing.T) {
	shrinkDeletionPolling(t)
	var gets int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Still there for the first two polls, gone afterwards.
		if atomic.AddInt32(&gets, 1) <= 2 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"instance": map[string]interface{}{
					"id":         "abc-123",
					"created_at": "2026-01-01T00:00:00Z",
					"updated_at": "2026-01-01T00:00:00Z",
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})

	if err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&gets); got != 3 {
		t.Errorf("expected to poll until the 404, got %d requests", got)
	}
}

func TestClient_WaitForInstanceDeletion_Timeout(t *testing.T) {
	shrinkDeletionPolling(t)
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{
				"id":         "abc-123",
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error while the instance still exists")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected a timeout error, got: %v", err)
	}
}

// The DELETE was already accepted, so a proxy hiccup during the poll must not
// turn a successful destroy into a failed one.
func TestClient_WaitForInstanceDeletion_SurvivesTransientErrors(t *testing.T) {
	shrinkDeletionPolling(t)
	var gets int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&gets, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad gateway"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})

	if err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Second); err != nil {
		t.Fatalf("expected the 502 to be retried, got: %v", err)
	}
	if got := atomic.LoadInt32(&gets); got != 2 {
		t.Errorf("expected a retry after the 502, got %d requests", got)
	}
}

// A persistent server error still fails, but as a timeout that names the last
// error rather than a bare deadline message.
func TestClient_WaitForInstanceDeletion_ReportsLastTransientError(t *testing.T) {
	shrinkDeletionPolling(t)
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "boom"})
	})

	err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error when the poll never succeeds")
	}
	if !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected a timeout naming the last error, got: %v", err)
	}
}

// A 4xx will not fix itself, so it must not burn the whole timeout window.
func TestClient_WaitForInstanceDeletion_FailsFastOnClientError(t *testing.T) {
	shrinkDeletionPolling(t)
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
	})

	start := time.Now()
	err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Second)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("expected the 403 to surface immediately, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("expected a fast failure, took %s", elapsed)
	}
}

// Regression: waitForDeletion used to check the context before the poll result,
// so a GET that came back 404 on the very tick the deadline expired reported a
// timeout for a resource that was in fact gone.
func TestWaitForDeletion_GoneWinsOnTheDeadlineTick(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := waitForDeletion(context.Background(), time.Nanosecond, func(context.Context) (bool, error) {
		<-expired.Done() // the deadline has already passed when "gone" reports true
		return true, nil
	})
	if err != nil {
		t.Fatalf("expected success when the resource is gone, got: %v", err)
	}
}

func TestClient_WaitForNetworkDeviceDeletion(t *testing.T) {
	shrinkDeletionPolling(t)
	var gets int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&gets, 1) == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"network_device": map[string]interface{}{
					"id": "dev-uuid", "name": "core-sw", "ip_address": "192.0.2.1",
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})

	if err := c.WaitForNetworkDeviceDeletion(context.Background(), "dev-uuid", 30*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&gets); got != 2 {
		t.Errorf("expected to poll until the 404, got %d requests", got)
	}
}

func TestRetryablePoll(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"transport failure", errors.New("connection reset by peer"), true},
		{"bad gateway", &APIError{StatusCode: http.StatusBadGateway}, true},
		{"server error", &APIError{StatusCode: http.StatusInternalServerError}, true},
		{"forbidden", &APIError{StatusCode: http.StatusForbidden}, false},
		{"unprocessable", &APIError{StatusCode: 422}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryablePoll(tt.err); got != tt.want {
				t.Errorf("retryablePoll(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestClient_WaitForInstanceDeletion_HonoursCancellation(t *testing.T) {
	shrinkDeletionPolling(t)
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{
				"id":         "abc-123",
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.WaitForInstanceDeletion(ctx, "abc-123", 30*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
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
				"meta": map[string]int{"current_page": 1, "total_pages": 2, "total_count": 2, "per_page": 100},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"instances": []map[string]interface{}{
					{"id": "b", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
				},
				"meta": map[string]int{"current_page": 2, "total_pages": 2, "total_count": 2, "per_page": 100},
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

// An unrecognised meta envelope must over-fetch by one page, never truncate.
// This is the regression guard for the rename that silently capped every list at
// 100 rows: the old struct decoded to zeros, the exit condition read 0 >= 0, and
// the unit tests stayed green because their fixtures encoded the old shape too.
func TestClient_ListInstances_UnrecognizedMetaDoesNotTruncate(t *testing.T) {
	var requests int
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		instances := []interface{}{}
		if page := r.URL.Query().Get("page"); page == "1" || page == "2" {
			instances = append(instances, map[string]interface{}{
				"id": "host-" + page, "display_name": "web-" + page,
			})
		}
		// A meta shape the client does not know: every field decodes to zero.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instances": instances,
			"meta":      map[string]interface{}{"page": 1, "pages": 3, "records": 3},
		})
	})

	instances, err := c.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected both pages to be walked, got %d instances", len(instances))
	}
	// Two full pages plus the empty page that ends the walk.
	if requests != 3 {
		t.Errorf("expected 3 requests, got %d", requests)
	}
}

// The archived filter moved server-side, so a page is walked for what the API
// chose to return rather than for what survives a client-side predicate.
func TestClient_ListWorkflows_WalksEveryPage(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []interface{}{
				map[string]interface{}{"id": page, "name": fmt.Sprintf("wf-%d", page), "status": "active"},
			},
			"meta": map[string]int{"current_page": page, "total_pages": 2, "total_count": 2, "per_page": 100},
		})
	})

	workflows, err := c.ListWorkflows(context.Background(), WorkflowListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workflows) != 2 || workflows[0].Name != "wf-1" || workflows[1].Name != "wf-2" {
		t.Errorf("expected both pages to be walked, got %v", workflows)
	}
}

func TestClient_ListWorkflows_SendsServerSideFilters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []map[string]interface{}{
				{"id": 2, "name": "archived-wf", "status": "archived", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	workflows, err := c.ListWorkflows(context.Background(), WorkflowListOptions{
		Status:       "archived",
		UpdatedSince: "2026-01-01T00:00:00Z",
		Order:        "updated_at",
		Direction:    "desc",
		Q:            "cpu",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for key, want := range map[string]string{
		"status":        "archived",
		"updated_since": "2026-01-01T00:00:00Z",
		"order":         "updated_at",
		"direction":     "desc",
		"q":             "cpu",
		"page":          "1",
		"per_page":      "100",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected query %s=%q, got %q", key, want, got)
		}
	}

	// Archived workflows are filtered by the API, not by the client, so an
	// explicit status=archived request must come back untouched.
	if len(workflows) != 1 || workflows[0].Name != "archived-wf" {
		t.Fatalf("expected the archived workflow to pass through, got %+v", workflows)
	}
}

func TestClient_ListWorkflows_OmitsEmptyFilters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []map[string]interface{}{},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	if _, err := c.ListWorkflows(context.Background(), WorkflowListOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"status", "updated_since", "order", "direction", "q"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %s to be omitted from the query, got %q", key, gotQuery.Get(key))
		}
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

func TestClient_CreateWorkflowVersion_OmitsCanvasDataWhenUnset(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version": map[string]interface{}{"id": 10, "version_number": 1, "created_at": "2026-01-01T00:00:00Z"},
		})
	})

	_, err := c.CreateWorkflowVersion(context.Background(), 42, CreateWorkflowVersionInput{
		ExecutionGraph: map[string]interface{}{"nodes": []interface{}{}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := gotBody["canvas_data"]; ok {
		t.Error("expected canvas_data to be omitted so the API generates a layout")
	}
}

func TestClient_CreateWorkflowVersion_SendsCanvasData(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version": map[string]interface{}{"id": 10, "version_number": 1, "created_at": "2026-01-01T00:00:00Z"},
		})
	})

	_, err := c.CreateWorkflowVersion(context.Background(), 42, CreateWorkflowVersionInput{
		ExecutionGraph: map[string]interface{}{"nodes": []interface{}{}},
		CanvasData:     map[string]interface{}{"viewport": map[string]interface{}{"zoom": 1.0}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	canvas, ok := gotBody["canvas_data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected canvas_data object, got %v", gotBody["canvas_data"])
	}
	if _, ok := canvas["viewport"]; !ok {
		t.Errorf("expected canvas_data.viewport, got %v", canvas)
	}
}

func TestClient_GetWorkflowVersion(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/workflows/42/versions/10" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version": map[string]interface{}{
				"id":             10,
				"version_number": 3,
				"execution_graph": map[string]interface{}{
					"nodes": []interface{}{map[string]interface{}{"id": "n1", "type": "trigger"}},
					"edges": []interface{}{},
				},
				"canvas_data": map[string]interface{}{"viewport": map[string]interface{}{"zoom": 1.0}},
				"created_at":  "2026-01-01T00:00:00Z",
			},
		})
	})

	ver, err := c.GetWorkflowVersion(context.Background(), 42, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver.ID != 10 {
		t.Errorf("expected version id 10, got %d", ver.ID)
	}
	nodes, ok := ver.ExecutionGraph["nodes"].([]interface{})
	if !ok || len(nodes) != 1 {
		t.Fatalf("expected 1 graph node, got %v", ver.ExecutionGraph["nodes"])
	}
	if ver.CanvasData["viewport"] == nil {
		t.Error("expected canvas_data to be decoded")
	}
}

// The version detail endpoint is documented with a "version" envelope; accept a
// bare object too so a shape change does not break reads.
func TestClient_GetWorkflowVersion_BareObject(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":              10,
			"version_number":  3,
			"execution_graph": map[string]interface{}{"nodes": []interface{}{}},
			"created_at":      "2026-01-01T00:00:00Z",
		})
	})

	ver, err := c.GetWorkflowVersion(context.Background(), 42, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver.ID != 10 || ver.VersionNumber != 3 {
		t.Errorf("expected version 10/3, got %d/%d", ver.ID, ver.VersionNumber)
	}
}

func TestClient_UpdateWorkflow_SendsNoIfMatch(t *testing.T) {
	var gotIfMatch string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotIfMatch = r.Header.Get("If-Match")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflow": map[string]interface{}{
				"id": 42, "name": "Renamed", "status": "draft",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	name := "Renamed"
	wf, err := c.UpdateWorkflow(context.Background(), 42, UpdateWorkflowInput{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIfMatch != "" {
		t.Errorf("workflow endpoints do not support If-Match, got %q", gotIfMatch)
	}
	if wf.Name != "Renamed" {
		t.Errorf("expected name Renamed, got %s", wf.Name)
	}
}

// --- Workflow Templates & Node Types ---

func TestClient_ListWorkflowTemplates(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/workflows/templates" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"templates": []map[string]interface{}{
				{"slug": "high-cpu", "name": "High CPU", "category": "instances", "extra_field": "kept"},
			},
		})
	})

	templates, err := c.ListWorkflowTemplates(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(templates) != 1 || templates[0].Slug != "high-cpu" {
		t.Fatalf("expected the high-cpu template, got %+v", templates)
	}
	if !strings.Contains(string(templates[0].Raw), "extra_field") {
		t.Errorf("expected Raw to keep unmodelled fields, got %s", templates[0].Raw)
	}
}

func TestClient_CreateWorkflowFromTemplate(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/workflows/templates" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflow": map[string]interface{}{
				"id": 7, "name": "High CPU", "status": "draft", "published_version_id": 3,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	wf, err := c.CreateWorkflowFromTemplate(context.Background(), "high-cpu")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["slug"] != "high-cpu" {
		t.Errorf("expected slug high-cpu in body, got %v", gotBody["slug"])
	}
	if wf.ID != 7 || wf.PublishedVersionID == nil || *wf.PublishedVersionID != 3 {
		t.Errorf("expected workflow 7 with published version 3, got %+v", wf)
	}
}

func TestClient_ListNodeTypes(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/node_types" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_types": []map[string]interface{}{
				{"type": "metric_threshold", "name": "Metric Threshold", "category": "trigger",
					"config_schema": map[string]interface{}{"metric": "string"}},
			},
		})
	})

	nodeTypes, err := c.ListNodeTypes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodeTypes) != 1 || nodeTypes[0].Type != "metric_threshold" {
		t.Fatalf("expected the metric_threshold node type, got %+v", nodeTypes)
	}
	if !strings.Contains(string(nodeTypes[0].Raw), "config_schema") {
		t.Errorf("expected Raw to carry the config schema, got %s", nodeTypes[0].Raw)
	}
}

// A bare array with no envelope decodes the same way.
func TestClient_ListNodeTypes_BareArray(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"type": "send_email", "name": "Send Email", "category": "action"},
		})
	})

	nodeTypes, err := c.ListNodeTypes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodeTypes) != 1 || nodeTypes[0].Type != "send_email" {
		t.Fatalf("expected the send_email node type, got %+v", nodeTypes)
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

func TestClient_UpdateUptimeMonitor_Protocol(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{
				"id": "mon-uuid", "name": "TCP Check", "protocol": "tcp", "status": "unknown",
				"hostname": "db.example.com", "port": 5432,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	protocol := "tcp"
	mon, err := c.UpdateUptimeMonitor(context.Background(), "mon-uuid", `"etag"`, UpdateUptimeMonitorInput{
		Protocol: &protocol,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mon.Protocol != "tcp" {
		t.Errorf("expected protocol tcp, got %s", mon.Protocol)
	}
	monitor := gotBody["uptime_monitor"].(map[string]interface{})
	if monitor["protocol"] != "tcp" {
		t.Errorf("expected protocol in update body, got %v", monitor["protocol"])
	}
}

func TestClient_UpdateUptimeMonitor_ClearsDNSExpectedRecords(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{
				"id": "mon-uuid", "name": "DNS Check", "protocol": "dns", "status": "up",
				"dns_expected_records": []string{},
				"created_at":           "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	empty := []string{}
	_, err := c.UpdateUptimeMonitor(context.Background(), "mon-uuid", "", UpdateUptimeMonitorInput{
		DNSExpectedRecords: &empty,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	monitor := gotBody["uptime_monitor"].(map[string]interface{})
	records, ok := monitor["dns_expected_records"]
	if !ok {
		t.Fatal("expected dns_expected_records to be sent, key was omitted")
	}
	if list, ok := records.([]interface{}); !ok || len(list) != 0 {
		t.Errorf("expected dns_expected_records to be [], got %v", records)
	}
}

func TestClient_UpdateUptimeMonitor_SendsNullForUnsetProtocolFields(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{
				"id": "mon-uuid", "name": "DNS Check", "protocol": "dns", "status": "up",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	name := "DNS Check"
	if _, err := c.UpdateUptimeMonitor(context.Background(), "mon-uuid", "", UpdateUptimeMonitorInput{Name: &name}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Protocol-scoped fields carry no omitempty: a nil pointer must reach the API as
	// an explicit null so switching protocol clears the previous protocol's values.
	monitor := gotBody["uptime_monitor"].(map[string]interface{})
	for _, key := range []string{
		"dns_expected_records", "port", "keyword", "dns_record_type",
		"custom_headers", "custom_body", "content_type",
	} {
		value, present := monitor[key]
		if !present {
			t.Errorf("expected %s to be sent as an explicit null, key was omitted", key)
			continue
		}
		if value != nil {
			t.Errorf("expected %s to be null, got %v", key, value)
		}
	}
	// Fields the plan always carries a value for stay omitempty and must not appear.
	if _, ok := monitor["interval_seconds"]; ok {
		t.Error("expected interval_seconds to be omitted when nil")
	}
}

func TestClient_UpdateUptimeMonitor_ClearsProbeRegionIDs(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{
				"id": "mon-uuid", "name": "API", "protocol": "https", "status": "up",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	// `probe_region_ids = []` has no validator shielding it, so an omitted key
	// would silently leave the server's old region set in place.
	empty := []int64{}
	if _, err := c.UpdateUptimeMonitor(context.Background(), "mon-uuid", "", UpdateUptimeMonitorInput{
		ProbeRegionIDs: &empty,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	monitor := gotBody["uptime_monitor"].(map[string]interface{})
	ids, present := monitor["probe_region_ids"]
	if !present {
		t.Fatal("expected probe_region_ids to be sent, key was omitted")
	}
	if list, ok := ids.([]interface{}); !ok || len(list) != 0 {
		t.Errorf("expected probe_region_ids to be [], got %v", ids)
	}
}

func TestParseError_FallsBackToRawBody(t *testing.T) {
	// A body keyed differently unmarshals without error and leaves both fields
	// empty, which would render a diagnostic with no reason at all.
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"protocol cannot be changed for this monitor"}`))
	})

	_, _, err := c.GetUptimeMonitor(context.Background(), "mon-uuid")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "protocol cannot be changed") {
		t.Errorf("expected the raw body in the message, got %q", err.Error())
	}
}

func TestClient_PauseUptimeMonitor(t *testing.T) {
	var gotPath, gotMethod string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{
				"id": "mon-uuid", "name": "API Health", "protocol": "https", "status": "paused",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	mon, err := c.PauseUptimeMonitor(context.Background(), "mon-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/api/v1/uptime_monitors/mon-uuid/pause" {
		t.Errorf("expected POST /api/v1/uptime_monitors/mon-uuid/pause, got %s %s", gotMethod, gotPath)
	}
	if mon.Status != "paused" {
		t.Errorf("expected status paused, got %s", mon.Status)
	}
}

func TestClient_ResumeUptimeMonitor(t *testing.T) {
	var gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{
				"id": "mon-uuid", "name": "API Health", "protocol": "https", "status": "recovering",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	mon, err := c.ResumeUptimeMonitor(context.Background(), "mon-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/uptime_monitors/mon-uuid/resume" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	// The resumed status comes from the API, not from an assumption in the provider.
	if mon.Status != "recovering" {
		t.Errorf("expected status recovering, got %s", mon.Status)
	}
}

func TestClient_GetUptimeMonitorStatus(t *testing.T) {
	var gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor_status": map[string]interface{}{
				"id":             "mon-uuid",
				"status":         "up",
				"last_check_at":  "2026-01-02T00:00:00Z",
				"next_check_at":  "2026-01-02T00:01:00Z",
				"last_error":     nil,
				"ssl_expires_at": "2026-10-11T17:06:46Z",
			},
		})
	})

	status, err := c.GetUptimeMonitorStatus(context.Background(), "mon-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/uptime_monitors/mon-uuid/status" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if status.Status != "up" {
		t.Errorf("expected status up, got %s", status.Status)
	}
	if status.LastError != nil {
		t.Errorf("expected last_error nil, got %v", *status.LastError)
	}
	if status.SSLExpiresAt == nil || *status.SSLExpiresAt != "2026-10-11T17:06:46Z" {
		t.Errorf("unexpected ssl_expires_at: %v", status.SSLExpiresAt)
	}
}

func TestClient_GetUptimeMonitorStatus_BareBody(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "mon-uuid", "status": "down", "last_error": "connection refused",
		})
	})

	status, err := c.GetUptimeMonitorStatus(context.Background(), "mon-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "down" {
		t.Errorf("expected status down, got %s", status.Status)
	}
	if status.LastError == nil || *status.LastError != "connection refused" {
		t.Errorf("unexpected last_error: %v", status.LastError)
	}
	// Absent keys must surface as null, not as a zero value.
	if status.NextCheckAt != nil {
		t.Errorf("expected next_check_at nil, got %v", *status.NextCheckAt)
	}
}

func TestClient_ListUptimeMonitors_Filters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitors": []interface{}{},
			"meta":            map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	_, err := c.ListUptimeMonitors(context.Background(), &ListUptimeMonitorsOptions{
		Status:       "down",
		Protocol:     "https",
		Query:        "api",
		UpdatedSince: "2026-01-01T00:00:00Z",
		Order:        "name",
		Direction:    "asc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for key, want := range map[string]string{
		"status": "down", "protocol": "https", "q": "api",
		"updated_since": "2026-01-01T00:00:00Z", "order": "name", "direction": "asc",
		"page": "1", "per_page": "100",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q, got %q", key, want, got)
		}
	}
}

func TestClient_ListUptimeMonitors_NoFilters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitors": []interface{}{
				map[string]interface{}{"id": "mon-uuid", "name": "API Health", "protocol": "https", "status": "up"},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	monitors, err := c.ListUptimeMonitors(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(monitors))
	}
	for _, key := range []string{"status", "protocol", "q", "updated_since", "order", "direction"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %s to be omitted when unset", key)
		}
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
				{"id": 1, "type": "SlackIntegration", "name": "Slack", "provider": "slack", "enabled": true, "verified": true, "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z"},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	integrations, err := c.ListIntegrations(context.Background(), IntegrationListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(integrations) != 1 {
		t.Errorf("expected 1 integration, got %d", len(integrations))
	}
	if integrations[0].Provider != "slack" {
		t.Errorf("expected provider slack, got %s", integrations[0].Provider)
	}
	if integrations[0].UpdatedAt != "2026-01-02T00:00:00Z" {
		t.Errorf("expected updated_at to be mapped, got %q", integrations[0].UpdatedAt)
	}
}

// The index went 25-per-page on 2026-09-01 while this client still sent a
// single un-paginated GET, so an organisation with more channels than one page
// silently lost the rest — and the data source fed that short list to for_each.
func TestClient_ListIntegrations_Pagination(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := int(atomic.AddInt32(&requestCount, 1))
		if got := r.URL.Query().Get("page"); got != strconv.Itoa(page) {
			t.Errorf("request %d asked for page %q, want %d", page, got, page)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integrations": []map[string]interface{}{
				{"id": page, "type": "WebhookIntegration", "provider": "Webhook"},
			},
			"meta": map[string]int{"current_page": page, "total_pages": 3, "total_count": 3, "per_page": 1},
		})
	})

	integrations, err := c.ListIntegrations(context.Background(), IntegrationListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(integrations) != 3 {
		t.Fatalf("expected 3 integrations across 3 pages, got %d", len(integrations))
	}
	for i, want := range []int64{1, 2, 3} {
		if integrations[i].ID != want {
			t.Errorf("integration %d: expected id %d, got %d", i, want, integrations[i].ID)
		}
	}
}

// An envelope the client cannot read must over-fetch, not truncate: that is the
// guard morePages exists for, and the reason the last meta rename cost eight
// list loops instead of being caught by one.
func TestClient_ListIntegrations_UnrecognisedMetaWalksToEmptyPage(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&requestCount, 1)
		body := map[string]interface{}{
			"integrations": []map[string]interface{}{},
			// The pre-2026-09 envelope: every field decodes to zero.
			"meta": map[string]int{"count": 1, "total": 2, "offset": 0},
		}
		if page == 1 {
			body["integrations"] = []map[string]interface{}{{"id": 1, "type": "WebhookIntegration"}}
		}
		json.NewEncoder(w).Encode(body)
	})

	integrations, err := c.ListIntegrations(context.Background(), IntegrationListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(integrations) != 1 {
		t.Errorf("expected 1 integration, got %d", len(integrations))
	}
	if requestCount != 2 {
		t.Errorf("expected the walk to continue past the unreadable meta to an empty page (2 requests), got %d", requestCount)
	}
}

func TestClient_GetIntegration(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/integrations/42" {
			t.Errorf("expected path /api/v1/integrations/42, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integration": map[string]interface{}{
				"id": 42, "type": "WebhookIntegration", "name": "Ops hook",
				"provider": "Webhook", "enabled": true, "verified": false,
				"created_at": "2026-09-01T00:00:00Z", "updated_at": "2026-09-01T01:00:00Z",
			},
		})
	})

	integration, err := c.GetIntegration(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if integration.Type != "WebhookIntegration" {
		t.Errorf("expected type WebhookIntegration, got %s", integration.Type)
	}
	if integration.UpdatedAt != "2026-09-01T01:00:00Z" {
		t.Errorf("expected updated_at to be decoded, got %q", integration.UpdatedAt)
	}
}

func TestClient_CreateIntegration_Webhook(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/integrations" {
			t.Errorf("expected POST /api/v1/integrations, got %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integration": map[string]interface{}{
				"id": 7, "type": "WebhookIntegration", "name": "https://example.com/hook",
				"provider": "Webhook", "enabled": true, "verified": false,
			},
			"webhook": map[string]interface{}{
				"verification_header":           "X-Fivenines-Verification",
				"verification_token":            "tok_abc",
				"verification_token_expires_at": "2026-09-02T00:00:00Z",
				"secret":                        "whsec_generated",
			},
		})
	})

	result, err := c.CreateIntegration(context.Background(), CreateIntegrationInput{
		Type: "webhook",
		URL:  "https://example.com/hook",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The API takes the short key, not the class name the response carries back.
	if got := gotBody["integration"]["type"]; got != "webhook" {
		t.Errorf("expected request type webhook, got %v", got)
	}
	if got := gotBody["integration"]["url"]; got != "https://example.com/hook" {
		t.Errorf("expected request url, got %v", got)
	}
	if _, present := gotBody["integration"]["routing_key"]; present {
		t.Error("expected unset fields to be omitted from the request body")
	}
	if result.Integration == nil || result.Integration.ID != 7 {
		t.Fatalf("expected integration 7, got %+v", result.Integration)
	}
	if result.Webhook == nil {
		t.Fatal("expected webhook verification block")
	}
	if result.Webhook.Secret != "whsec_generated" {
		t.Errorf("expected generated signing secret, got %q", result.Webhook.Secret)
	}
	if result.Webhook.VerificationToken != "tok_abc" {
		t.Errorf("expected verification token tok_abc, got %q", result.Webhook.VerificationToken)
	}
	if result.EmailVerification != nil {
		t.Error("expected no email verification for a webhook")
	}
}

func TestClient_CreateIntegration_Pagerduty(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integration": map[string]interface{}{
				"id": 8, "type": "PagerdutyIntegration", "name": "Ops",
				"provider": "Pagerduty", "enabled": true, "verified": true,
			},
		})
	})

	result, err := c.CreateIntegration(context.Background(), CreateIntegrationInput{
		Type:       "pagerduty",
		Name:       "Ops",
		RoutingKey: "R0UT1NGK3Y",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gotBody["integration"]["routing_key"]; got != "R0UT1NGK3Y" {
		t.Errorf("expected routing_key in request, got %v", got)
	}
	if result.Webhook != nil {
		t.Error("expected no webhook block for pagerduty")
	}
	if !result.Integration.Verified {
		t.Error("expected pagerduty integration to come back verified")
	}
}

func TestClient_CreateIntegration_Email202(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "pending_verification",
			"verification": map[string]interface{}{
				"id": 42, "email": "ops@example.com",
				"expires_at": "2026-09-01T00:15:00Z", "verify_path": "/api/v1/integrations/42/verify",
			},
		})
	})

	result, err := c.CreateIntegration(context.Background(), CreateIntegrationInput{
		Type:  "email",
		Email: "ops@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 202 means no channel was created — only a pending verification code.
	if result.Integration != nil {
		t.Errorf("expected no integration on 202, got %+v", result.Integration)
	}
	if result.EmailVerification == nil || result.EmailVerification.ID != 42 {
		t.Fatalf("expected verification id 42, got %+v", result.EmailVerification)
	}
}

func TestClient_CreateIntegration_422NotCreatable(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []string{"slack integrations must be connected from the dashboard"},
		})
	})

	_, err := c.CreateIntegration(context.Background(), CreateIntegrationInput{Type: "slack"})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 422 {
		t.Errorf("expected status 422, got %d", apiErr.StatusCode)
	}
}

func TestClient_CreateIntegration_403PlanGate(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "plan does not include PagerDuty alerts"})
	})

	_, err := c.CreateIntegration(context.Background(), CreateIntegrationInput{Type: "pagerduty"})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 403 {
		t.Errorf("expected status 403, got %d", apiErr.StatusCode)
	}
}

func TestClient_DeleteIntegration(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/integrations/7" {
			t.Errorf("expected DELETE /api/v1/integrations/7, got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteIntegration(context.Background(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Delete is the one integration call with no body to decode, so the status code
// is the entire result. Swallowing a non-204 would drop the channel from state
// while it still exists server-side and still delivers.
func TestClient_DeleteIntegration_Errors(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "nope"})
		})

		err := c.DeleteIntegration(context.Background(), 7)
		apiErr, ok := err.(*APIError)
		if !ok {
			t.Fatalf("status %d: expected *APIError, got %T (%v)", status, err, err)
		}
		if apiErr.StatusCode != status {
			t.Errorf("expected status %d, got %d", status, apiErr.StatusCode)
		}
	}
}

func TestClient_VerifyWebhookIntegration(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/integrations/7/verify_webhook" {
			t.Errorf("expected POST /api/v1/integrations/7/verify_webhook, got %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integration": map[string]interface{}{
				"id": 7, "type": "WebhookIntegration", "verified": true, "enabled": true,
			},
		})
	})

	integration, err := c.VerifyWebhookIntegration(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !integration.Verified {
		t.Error("expected verified true")
	}
}

func TestClient_VerifyWebhookIntegration_422(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []string{"endpoint returned 404"},
		})
	})

	_, err := c.VerifyWebhookIntegration(context.Background(), 7)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 422 {
		t.Errorf("expected status 422, got %d", apiErr.StatusCode)
	}
}

func TestClient_RegenerateWebhookToken(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/integrations/7/regenerate_webhook_token" {
			t.Errorf("expected POST /api/v1/integrations/7/regenerate_webhook_token, got %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integration": map[string]interface{}{"id": 7, "type": "WebhookIntegration", "verified": false},
			"webhook": map[string]interface{}{
				"verification_header":           "X-Fivenines-Verification",
				"verification_token":            "tok_fresh",
				"verification_token_expires_at": "2026-09-03T00:00:00Z",
			},
		})
	})

	integration, webhook, err := c.RegenerateWebhookToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if integration.ID != 7 {
		t.Errorf("expected integration 7, got %d", integration.ID)
	}
	if webhook == nil || webhook.VerificationToken != "tok_fresh" {
		t.Fatalf("expected fresh token, got %+v", webhook)
	}
	// Regenerating a token does not re-issue the signing secret.
	if webhook.Secret != "" {
		t.Errorf("expected no signing secret, got %q", webhook.Secret)
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
					"id": 1, "title": "High CPU", "status": "open",
					"summary": "CPU above 90%", "created_at": "2026-01-01T00:00:00Z",
					"updated_at":        "2026-01-01T00:00:00Z",
					"public":            true,
					"uptime_monitor_id": "8b1d0e7a-0000-4000-8000-000000000001",
					"duration_seconds":  3600,
				},
				{
					"id": 2, "title": "Disk Full", "status": "resolved",
					"summary": "Disk at 95%", "created_at": "2026-01-01T00:00:00Z",
					"updated_at": "2026-01-02T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	incidents, err := c.ListIncidents(context.Background(), IncidentListOptions{})
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
	if !incidents[0].Public {
		t.Error("expected incident 1 to be public")
	}
	if incidents[0].UptimeMonitorID == nil || *incidents[0].UptimeMonitorID != "8b1d0e7a-0000-4000-8000-000000000001" {
		t.Errorf("expected uptime_monitor_id to be mapped, got %v", incidents[0].UptimeMonitorID)
	}
	if incidents[0].DurationSeconds == nil || *incidents[0].DurationSeconds != 3600 {
		t.Errorf("expected duration_seconds 3600, got %v", incidents[0].DurationSeconds)
	}
	if incidents[1].UptimeMonitorID != nil {
		t.Errorf("expected null uptime_monitor_id to stay nil, got %v", *incidents[1].UptimeMonitorID)
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
				"consecutive_failures": 0, "last_error_type": nil, "last_error_message": nil,
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
	if dev.Vendor == nil || *dev.Vendor != "Cisco" {
		t.Errorf("expected vendor Cisco, got %v", dev.Vendor)
	}
	if dev.ConsecutiveFailures != 0 {
		t.Errorf("expected consecutive_failures 0, got %d", dev.ConsecutiveFailures)
	}
	if dev.LastErrorType != nil {
		t.Errorf("expected last_error_type nil, got %v", *dev.LastErrorType)
	}
	if dev.LastErrorMessage != nil {
		t.Errorf("expected last_error_message nil, got %v", *dev.LastErrorMessage)
	}
}

func TestClient_GetNetworkDevice_Unreachable(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"network_device": map[string]interface{}{
				"id": "dev-uuid", "name": "Core Switch", "ip_address": "192.168.1.1",
				"status": "unreachable", "consecutive_failures": 3,
				"last_error_type": "timeout", "last_error_message": "no response after 5s",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	dev, _, err := c.GetNetworkDevice(context.Background(), "dev-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dev.Status == nil || *dev.Status != "unreachable" {
		t.Errorf("expected status unreachable, got %v", dev.Status)
	}
	if dev.ConsecutiveFailures != 3 {
		t.Errorf("expected consecutive_failures 3, got %d", dev.ConsecutiveFailures)
	}
	if dev.LastErrorType == nil || *dev.LastErrorType != "timeout" {
		t.Errorf("expected last_error_type timeout, got %v", dev.LastErrorType)
	}
	if dev.LastErrorMessage == nil || *dev.LastErrorMessage != "no response after 5s" {
		t.Errorf("expected last_error_message set, got %v", dev.LastErrorMessage)
	}
}

func TestClient_DeleteNetworkDevice_202(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	accepted, err := c.DeleteNetworkDevice(context.Background(), "dev-uuid")
	if err != nil {
		t.Fatalf("expected no error for 202, got: %v", err)
	}
	if !accepted {
		t.Error("expected 202 to report an asynchronous deletion")
	}
}

func TestClient_EnterMaintenanceNetworkDevice(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/network_devices/dev-uuid/enter_maintenance" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"network_device": map[string]interface{}{"id": "dev-uuid", "maintenance_mode": true, "status": "up"},
		})
	})
	dev, err := c.EnterMaintenanceNetworkDevice(context.Background(), "dev-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dev.MaintenanceMode {
		t.Error("expected maintenance_mode true in the returned device")
	}
	if dev.Status == nil || *dev.Status != "up" {
		t.Errorf("expected status up in the returned device, got %v", dev.Status)
	}
}

func TestClient_ExitMaintenanceNetworkDevice(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/network_devices/dev-uuid/exit_maintenance" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"network_device": map[string]interface{}{"id": "dev-uuid", "maintenance_mode": false},
		})
	})
	dev, err := c.ExitMaintenanceNetworkDevice(context.Background(), "dev-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dev.MaintenanceMode {
		t.Error("expected maintenance_mode false in the returned device")
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
	if page.ThemeVariant == nil || *page.ThemeVariant != "dark" {
		t.Errorf("expected theme_variant dark, got %v", page.ThemeVariant)
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

// --- Error Envelope (code / request_id) ---

// The exact shape the PUBLIC envelope renders through render_error: error plus
// request_id, and no code — the public renderer drops it. Nothing in the client
// may require a code to work, so this is the fixture that matters.
func TestClient_APIError_ParsesRequestIDFromBody(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "header-id")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Not found", "request_id": "body-id",
		})
	})

	_, _, err := c.GetInstance(context.Background(), "missing")
	apiErr := AsAPIError(err)
	if apiErr == nil {
		t.Fatalf("expected *APIError, got %T", err)
	}
	// The body wins: it survives a proxy that strips response headers.
	if apiErr.RequestID != "body-id" {
		t.Errorf("expected request_id %q, got %q", "body-id", apiErr.RequestID)
	}
	if apiErr.Code != "" {
		t.Errorf("the public envelope emits no code, got %q", apiErr.Code)
	}
	if !IsNotFound(err) {
		t.Error("a 404 with no code must still classify as not-found")
	}
}

// Code is parsed when a surface DOES send one (the partner envelope carries a
// code today, and the public error table is specified to grow one). This is
// forward-compatibility only: no behaviour may depend on it, which is why the
// case above is the one that drives IsNotFound.
func TestClient_APIError_ParsesCodeWhenPresent(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Unknown query key: statuss", "request_id": "req-9",
			"code": "unknown_parameter",
		})
	})

	_, _, err := c.GetInstance(context.Background(), "abc")
	apiErr := AsAPIError(err)
	if apiErr == nil {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != ErrCodeUnknownParameter {
		t.Errorf("expected code %q, got %q", ErrCodeUnknownParameter, apiErr.Code)
	}
	// A 400 is not drift, whatever code it carries.
	if IsNotFound(err) {
		t.Error("a 400 must never classify as not-found")
	}
}

// A 422 renders `{"errors": [...]}` alone — no message, no request_id — so the
// only place to recover the correlation id is the X-Request-Id header.
func TestClient_APIError_RequestIDFallsBackToHeader(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "header-id")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"Name can't be blank"}})
	})

	_, err := c.CreateInstance(context.Background(), CreateInstanceInput{})
	apiErr := AsAPIError(err)
	if apiErr == nil {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.RequestID != "header-id" {
		t.Errorf("expected request_id from header %q, got %q", "header-id", apiErr.RequestID)
	}
}

// The request_id must reach the diagnostic, which renders err.Error().
func TestAPIError_ErrorStringIncludesRequestIDAndCode(t *testing.T) {
	tests := []struct {
		name string
		err  *APIError
		want string
	}{
		{
			name: "code and request_id",
			err:  &APIError{StatusCode: 404, Message: "Not found", Code: ErrCodeNotFound, RequestID: "req-1"},
			want: "API error 404 [not_found]: Not found (request_id: req-1)",
		},
		{
			name: "validation errors without code",
			err:  &APIError{StatusCode: 422, Errors: []string{"Name can't be blank"}, RequestID: "req-2"},
			want: "API error 422: [Name can't be blank] (request_id: req-2)",
		},
		{
			name: "no code, no request_id (pre-envelope server)",
			err:  &APIError{StatusCode: 500, Message: "boom"},
			want: "API error 500: boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Update input clearing convention, outside the uptime monitor ---
//
// The same tag convention #9 established for protocol-scoped monitor fields
// applies to the Optional-only attributes on the other resources: no omitempty,
// so a nil pointer reaches the API as an explicit null and clears the value.

func TestClient_UpdateTask_SendsNullForUnsetHostID(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task": map[string]interface{}{
				"id": "task-uuid", "name": "backup", "schedule_type": "interval", "status": "active",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	name := "backup"
	if _, err := c.UpdateTask(context.Background(), "task-uuid", "", UpdateTaskInput{Name: &name}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	task := gotBody["task"].(map[string]interface{})
	value, present := task["host_id"]
	if !present {
		t.Fatal("expected host_id to be sent as an explicit null, key was omitted")
	}
	if value != nil {
		t.Errorf("expected host_id to be null, got %v", value)
	}
	// schedule/interval_seconds are Optional+Computed: the API keeps the
	// counterpart it stored across a schedule_type switch, so they stay omitted.
	for _, key := range []string{"schedule", "interval_seconds"} {
		if _, ok := task[key]; ok {
			t.Errorf("expected %s to be omitted when nil", key)
		}
	}
}

func TestClient_UpdateNetworkDevice_ClearsIDsButKeepsSecrets(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"network_device": map[string]interface{}{
				"id": "dev-uuid", "name": "core-sw", "ip_address": "192.0.2.1",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	name := "core-sw"
	if _, err := c.UpdateNetworkDevice(context.Background(), "dev-uuid", "", UpdateNetworkDeviceInput{Name: &name}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	device := gotBody["network_device"].(map[string]interface{})
	for _, key := range []string{"polling_host_id", "snmp_username"} {
		value, present := device[key]
		if !present {
			t.Errorf("expected %s to be sent as an explicit null, key was omitted", key)
			continue
		}
		if value != nil {
			t.Errorf("expected %s to be null, got %v", key, value)
		}
	}
	// The write-only credentials are blank-means-keep server-side. Sending null
	// would wipe a working credential on every unrelated update.
	for _, key := range []string{"snmp_community", "snmp_auth_password", "snmp_priv_password"} {
		if _, ok := device[key]; ok {
			t.Errorf("expected %s to be omitted when unset, not sent as null", key)
		}
	}
}

func TestClient_UpdateStatusPage_EmptyItems(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status_page": map[string]interface{}{
				"id": 1, "name": "Status", "slug": "status",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	// Emptying a page needs an explicit []. A plain []StatusPageItem with
	// omitempty marshals an empty list to nothing, which the API reads as
	// "preserve the current items", so a page could never be emptied.
	name := "Status"
	if _, err := c.UpdateStatusPage(context.Background(), 1, "", UpdateStatusPageInput{
		Name:  &name,
		Items: &[]StatusPageItemInput{},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	page := gotBody["status_page"].(map[string]interface{})
	items, present := page["items"]
	if !present {
		t.Fatal("expected items to be sent as an explicit [], key was omitted")
	}
	if list, ok := items.([]interface{}); !ok || len(list) != 0 {
		t.Errorf("expected items to be [], got %v", items)
	}

	// Leaving items unmanaged must not touch them.
	if _, err := c.UpdateStatusPage(context.Background(), 1, "", UpdateStatusPageInput{Name: &name}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	page = gotBody["status_page"].(map[string]interface{})
	if _, ok := page["items"]; ok {
		t.Error("expected items to be omitted when the pointer is nil")
	}
}

// shrinkDeletionPolling makes the poll tests run in milliseconds instead of
// sleeping at the production pace. Without it the deletion tests alone add
// several seconds to every run of this package.
func shrinkDeletionPolling(t *testing.T) {
	t.Helper()
	interval, maxInterval := deletionPollInterval, deletionPollMaxInterval
	deletionPollInterval, deletionPollMaxInterval = time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() {
		deletionPollInterval, deletionPollMaxInterval = interval, maxInterval
	})
}

// A syntactically bad body will be just as bad next time, so it must not burn
// the whole timeout window. Driven end to end: classifying on a message string
// would pin nothing about the production path.
func TestClient_WaitForInstanceDeletion_MalformedBodyFailsFast(t *testing.T) {
	shrinkDeletionPolling(t)
	var gets int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&gets, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ this is not json"))
	})

	err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Second)
	if err == nil {
		t.Fatal("expected an undecodable body to fail")
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected a fast failure, not a timeout: %v", err)
	}
	if got := atomic.LoadInt32(&gets); got != 1 {
		t.Errorf("expected a single poll, got %d", got)
	}
}

// A read that dies in transit is the transport failing, not the server sending
// garbage — the case retrying exists for.
func TestRetryablePoll_TruncatedReadIsRetried(t *testing.T) {
	if !retryablePoll(fmt.Errorf("decoding response: %w", io.ErrUnexpectedEOF)) {
		t.Error("expected a truncated read to be retried")
	}
	if retryablePoll(fmt.Errorf("%w: bad", errMalformedBody)) {
		t.Error("expected a malformed body to fail fast")
	}
}

// 429 is the one 4xx that fixes itself, and a fleet destroy is exactly the
// workload that trips the limiter.
func TestRetryablePoll_RateLimitIsRetried(t *testing.T) {
	if !retryablePoll(&APIError{StatusCode: http.StatusTooManyRequests}) {
		t.Error("expected 429 to be retried")
	}
}

// deletionDone must see through a wrapped API error, or a 404 that arrives
// wrapped reads as "still there" and the poll runs to timeout on a resource
// that is already gone.
func TestDeletionDone_UnwrapsAPIError(t *testing.T) {
	wrapped := fmt.Errorf("checking instance: %w", &APIError{StatusCode: http.StatusNotFound})
	done, err := deletionDone(wrapped)
	if err != nil || !done {
		t.Errorf("expected a wrapped 404 to report done, got (%v, %v)", done, err)
	}
}

// --- deletion poll: the conditions that had no test before ---
//
// The poll took three fix cycles and each fix introduced the next defect, so the
// terminal and transient conditions are enumerated rather than sampled. These
// five cells were previously covered only by unit assertions on retryablePoll,
// or not at all.

// deletionPollServer answers `first` for the first n polls, then 404s.
func deletionPollServer(t *testing.T, n int32, first http.HandlerFunc) (*Client, *int32) {
	t.Helper()
	var polls int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&polls, 1) <= n {
			first(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})
	return c, &polls
}

func statusHandler(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "transient"})
	}
}

// A fleet destroy at parallelism 10 is exactly the workload that trips the
// limiter. doRequest absorbs a 429 itself, so the poll's own 429 branch is only
// reachable once doRequest EXHAUSTS its five internal retries and hands the 429
// up — which is what this drives. Retry-After: 0 keeps doRequest's own backoff
// from adding a minute of sleeping.
func TestClient_WaitForInstanceDeletion_RetriesExhaustedRateLimit(t *testing.T) {
	shrinkDeletionPolling(t)

	// doRequest makes 1 + 5 attempts before giving up, so 6 x 429 exhausts one
	// poll; the seventh request is the next poll and reports the deletion.
	c, requests := deletionPollServer(t, 6, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "rate limited"})
	})

	if err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Second); err != nil {
		t.Fatalf("expected an exhausted 429 to be retried by the poll, got: %v", err)
	}
	if got := atomic.LoadInt32(requests); got != 7 {
		t.Errorf("expected 6 rate-limited attempts then the 404, got %d requests", got)
	}
}

func TestClient_WaitForInstanceDeletion_RetriesRequestTimeoutEndToEnd(t *testing.T) {
	shrinkDeletionPolling(t)
	c, polls := deletionPollServer(t, 1, statusHandler(http.StatusRequestTimeout))

	if err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Second); err != nil {
		t.Fatalf("expected the 408 to be retried, got: %v", err)
	}
	if got := atomic.LoadInt32(polls); got != 2 {
		t.Errorf("expected a retry after the 408, got %d polls", got)
	}
}

// The regression cycle 3 introduced and two reviewers demonstrated: a connection
// dropped mid-body surfaces through the same wrapper as a syntactically bad
// payload, and must stay retryable. Driven end to end, not through retryablePoll.
func TestClient_WaitForInstanceDeletion_RetriesTruncatedReadEndToEnd(t *testing.T) {
	shrinkDeletionPolling(t)
	c, polls := deletionPollServer(t, 1, func(w http.ResponseWriter, r *http.Request) {
		// Promise more than we send, then kill the connection mid-body.
		w.Header().Set("Content-Length", "500")
		w.Write([]byte(`{"instance":{"id":"abc`))
		if hijacker, ok := w.(http.Hijacker); ok {
			conn, _, err := hijacker.Hijack()
			if err == nil {
				conn.Close()
			}
		}
	})

	if err := c.WaitForInstanceDeletion(context.Background(), "abc-123", 30*time.Second); err != nil {
		t.Fatalf("expected a truncated read to be retried, got: %v", err)
	}
	if got := atomic.LoadInt32(polls); got != 2 {
		t.Errorf("expected a retry after the truncated read, got %d polls", got)
	}
}

// Ctrl+C between polls, not during one — the poll spends most of its life
// asleep, so this is the likeliest moment to be interrupted.
func TestClient_WaitForInstanceDeletion_CancelDuringBackoffSleep(t *testing.T) {
	interval, maxInterval := deletionPollInterval, deletionPollMaxInterval
	deletionPollInterval, deletionPollMaxInterval = 50*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { deletionPollInterval, deletionPollMaxInterval = interval, maxInterval })

	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{
				"id": "abc-123", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond) // lands inside the 50ms sleep
		cancel()
	}()

	err := c.WaitForInstanceDeletion(ctx, "abc-123", 30*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled when interrupted between polls, got: %v", err)
	}
}

// Without a cap the interval doubles unboundedly and a slow teardown spends the
// back half of its window asleep instead of polling.
func TestWaitForDeletion_BackoffCaps(t *testing.T) {
	interval, maxInterval := deletionPollInterval, deletionPollMaxInterval
	deletionPollInterval, deletionPollMaxInterval = time.Millisecond, 4*time.Millisecond
	t.Cleanup(func() { deletionPollInterval, deletionPollMaxInterval = interval, maxInterval })

	var polls int
	_ = waitForDeletion(context.Background(), 60*time.Millisecond, func(context.Context) (bool, error) {
		polls++
		return false, nil
	})

	// Uncapped, 1ms doubling reaches 64ms and fits ~7 polls in the window.
	// Capped at 4ms it manages appreciably more.
	if polls < 10 {
		t.Errorf("expected the backoff to cap and keep polling, got %d polls in 60ms", polls)
	}
}

// --- API Tokens ---

func TestClient_CreateAPIToken(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/api_tokens" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_token": map[string]interface{}{
				"id":           7,
				"name":         "CI deploy key",
				"token_prefix": "fn_a1b2c",
				"scopes":       []string{"write", "read"},
				"expires_at":   nil,
				"active":       true,
				"created_at":   "2026-09-01T10:00:00Z",
				"updated_at":   "2026-09-01T10:00:00Z",
				"token":        "fn_a1b2c3d4e5f6",
			},
		})
	})

	token, err := c.CreateAPIToken(context.Background(), CreateAPITokenInput{
		Name:   "CI deploy key",
		Scopes: []string{"read", "write"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The body has to be nested under the api_token root key.
	wrapped, ok := gotBody["api_token"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected body nested under api_token, got %v", gotBody)
	}
	if wrapped["name"] != "CI deploy key" {
		t.Errorf("expected name 'CI deploy key', got %v", wrapped["name"])
	}
	if _, sent := wrapped["expires_at"]; sent {
		t.Error("expected expires_at to be omitted when unset")
	}
	if token.Token != "fn_a1b2c3d4e5f6" {
		t.Errorf("expected the plaintext token to be returned, got %q", token.Token)
	}
	if token.ID != 7 {
		t.Errorf("expected id 7, got %d", token.ID)
	}
}

func TestClient_CreateAPIToken_SendsExpiry(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"api_token": map[string]interface{}{"id": 1}})
	})

	expires := "2026-12-01T00:00:00Z"
	if _, err := c.CreateAPIToken(context.Background(), CreateAPITokenInput{Name: "t", ExpiresAt: &expires}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wrapped := gotBody["api_token"].(map[string]interface{})
	if wrapped["expires_at"] != expires {
		t.Errorf("expected expires_at %q, got %v", expires, wrapped["expires_at"])
	}
}

func TestClient_ListAPITokens_Pagination(t *testing.T) {
	var pages int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&pages, 1)
		if got := r.URL.Query().Get("page"); got != fmt.Sprint(page) {
			t.Errorf("expected page %d, got %q", page, got)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_tokens": []map[string]interface{}{{"id": int(page), "name": fmt.Sprintf("token-%d", page)}},
			"meta": map[string]int{
				"current_page": int(page),
				"total_pages":  2,
				"total_count":  2,
				"per_page":     100,
			},
		})
	})

	tokens, err := c.ListAPITokens(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens across 2 pages, got %d", len(tokens))
	}
	if tokens[1].Name != "token-2" {
		t.Errorf("expected the second page to be appended, got %q", tokens[1].Name)
	}
}

// An unreadable meta envelope walks until an empty page rather than trusting
// counters it cannot decode — the morePages guard that exists because a meta
// rename once truncated every list in the provider at one page while the unit
// tests stayed green. A recognised meta is authoritative instead, empty middle
// page included, which TestClient_ListAPITokens_Pagination covers.
func TestClient_ListAPITokens_StopsOnEmptyPageWhenMetaIsUnreadable(t *testing.T) {
	var calls int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&calls, 1)
		tokens := []map[string]interface{}{}
		if page == 1 {
			tokens = append(tokens, map[string]interface{}{"id": 1, "name": "only"})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_tokens": tokens,
			// The pre-2026-09 envelope: every field of PaginationMeta decodes to zero.
			"meta": map[string]int{"count": 1, "total": 1, "offset": 0},
		})
	})

	tokens, err := c.ListAPITokens(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}
	if calls != 2 {
		t.Errorf("expected the walk to over-fetch by exactly one empty page, got %d requests", calls)
	}
}

func TestClient_GetAPIToken(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/api_tokens" {
			t.Errorf("expected the index to be walked, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_tokens": []map[string]interface{}{
				{"id": 1, "name": "other"},
				{"id": 42, "name": "wanted", "token_prefix": "fn_a1b2c", "scopes": []string{"read"}, "active": true},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1},
		})
	})

	token, err := c.GetAPIToken(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.Name != "wanted" {
		t.Errorf("expected token 42, got %q", token.Name)
	}
}

// There is no show endpoint, so a missing row has to answer like one.
func TestClient_GetAPIToken_NotFound(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_tokens": []map[string]interface{}{{"id": 1}},
			"meta":       map[string]int{"current_page": 1, "total_pages": 1},
		})
	})

	_, err := c.GetAPIToken(context.Background(), 42)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("expected 404, got %d", apiErr.StatusCode)
	}
}

func TestClient_RevokeAPIToken(t *testing.T) {
	var hit bool
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hit = r.Method == "DELETE" && r.URL.Path == "/api/v1/api_tokens/7"
		if !hit {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_token": map[string]interface{}{
				"id":         7,
				"name":       "CI deploy key",
				"revoked_at": "2026-09-01T12:00:00Z",
				"active":     false,
			},
		})
	})

	if err := c.RevokeAPIToken(context.Background(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hit {
		t.Error("expected DELETE /api/v1/api_tokens/7")
	}
}

// A refused mint must not read as a success. Every failure mode on this endpoint
// answers with a body — 403 for a read-scoped caller or a scope it cannot grant,
// 402 for a restricted organization, 422 for a bad expiry — and all of them
// decode into an APIToken with an empty value. Without the status check the
// provider would store that empty credential and report the apply green.
func TestClient_CreateAPIToken_RejectionIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   map[string]interface{}
	}{
		{"scope escalation", http.StatusForbidden, map[string]interface{}{"error": "This token cannot grant scopes it does not hold itself: write."}},
		{"restricted organization", http.StatusPaymentRequired, map[string]interface{}{"error": "Access restricted"}},
		{"bad expiry", http.StatusUnprocessableEntity, map[string]interface{}{"error": "expires_at must be in the future"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(tc.body)
			})

			token, err := c.CreateAPIToken(context.Background(), CreateAPITokenInput{Name: "nope"})
			if err == nil {
				t.Fatalf("expected an error, got token %+v", token)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %T (%v)", err, err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("expected status %d, got %d", tc.status, apiErr.StatusCode)
			}
		})
	}
}

// A non-JSON body (an HTML 404 from a routing miss, or a proxy error page) must
// still produce a usable APIError rather than a blank one.
func TestClient_APIError_NonJSONBody(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "header-id")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "<html>502 Bad Gateway</html>\n")
	})

	_, _, err := c.GetInstance(context.Background(), "abc")
	apiErr := AsAPIError(err)
	if apiErr == nil {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Message != "<html>502 Bad Gateway</html>" {
		t.Errorf("expected raw body as message, got %q", apiErr.Message)
	}
	if apiErr.RequestID != "header-id" {
		t.Errorf("expected request_id from header, got %q", apiErr.RequestID)
	}
}

func TestErrorPredicates(t *testing.T) {
	tests := []struct {
		name string
		fn   func(error) bool
		err  error
		want bool
	}{
		{"404 is not found", IsNotFound, &APIError{StatusCode: 404}, true},
		// The status decides, not the code. This case is the guard on that:
		// IsNotFound authorises state removal, so a non-404 that merely
		// mentions not_found must NOT be allowed to delete a live resource
		// from state.
		{"not_found code without a 404 status is not not-found", IsNotFound,
			&APIError{StatusCode: 400, Code: ErrCodeNotFound}, false},
		{"403 is not not-found", IsNotFound, &APIError{StatusCode: 403}, false},
		{"401 is not not-found", IsNotFound, &APIError{StatusCode: 401}, false},
		{"non-API error is not not-found", IsNotFound, fmt.Errorf("dial tcp: refused"), false},
		{"nil error is not not-found", IsNotFound, nil, false},
		{"412 is precondition failed", IsPreconditionFailed, &APIError{StatusCode: 412}, true},
		{"409 is not precondition failed", IsPreconditionFailed, &APIError{StatusCode: 409}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(tt.err); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// The predicates must see through a wrapped error, since AsAPIError uses errors.As.
func TestErrorPredicates_Wrapped(t *testing.T) {
	wrapped := fmt.Errorf("reading instance: %w", &APIError{StatusCode: 404})
	if !IsNotFound(wrapped) {
		t.Error("expected IsNotFound to unwrap a wrapped *APIError")
	}
}

// --- Dry Run ---

func TestClient_DryRun(t *testing.T) {
	var gotHeader string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Dry-Run")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{"id": "abc-123", "display_name": "dry"},
		})
	})

	if _, err := c.CreateInstance(WithDryRun(context.Background()), CreateInstanceInput{DisplayName: "dry"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The server parses the header as a boolean and 400s on anything outside
	// its accepted token set, so the value must be one of those tokens.
	if gotHeader != "true" {
		t.Errorf("expected X-Dry-Run 'true', got %q", gotHeader)
	}
}

func TestClient_NoDryRunHeaderByDefault(t *testing.T) {
	var present bool
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["X-Dry-Run"]
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instance": map[string]interface{}{"id": "abc-123", "display_name": "real"},
		})
	})

	if _, err := c.CreateInstance(context.Background(), CreateInstanceInput{DisplayName: "real"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Absent, not "false": the header must not be sent at all on a real write.
	if present {
		t.Error("expected no X-Dry-Run header on a normal request")
	}
}

// A refused revoke is the same hazard in reverse: swallow it and Terraform drops
// a live credential out of state, leaving it valid and unmanaged.
func TestClient_RevokeAPIToken_RejectionIsAnError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing write scope"})
	})

	if err := c.RevokeAPIToken(context.Background(), 7); err == nil {
		t.Fatal("expected an error")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
			t.Errorf("expected a 403 APIError, got %v", err)
		}
	}
}

// --- Index filters (#21) ---
//
// Every index rejects an unknown query parameter with a 400, so these assert
// both halves of the contract: a configured filter is sent under its
// allowlisted name, and an unset one is absent rather than sent empty. The
// exact-count assertion is the half that matters — without it a stray key
// would ride along unnoticed until the API rejected the whole request.

func TestClient_ListIncidents_Filters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	workflowID := int64(7)
	_, err := c.ListIncidents(context.Background(), IncidentListOptions{
		Status:          "open",
		Q:               "CPU",
		HostID:          "3cac0e44-0000-4000-8000-000000000001",
		TaskID:          "3cac0e44-0000-4000-8000-000000000002",
		UptimeMonitorID: "3cac0e44-0000-4000-8000-000000000003",
		WorkflowID:      &workflowID,
		From:            "2026-08-29T00:00:00Z",
		To:              "2026-08-30T00:00:00Z",
		UpdatedSince:    "2026-08-28T00:00:00Z",
		Order:           "updated_at",
		Direction:       "asc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"status":            "open",
		"q":                 "CPU",
		"host_id":           "3cac0e44-0000-4000-8000-000000000001",
		"task_id":           "3cac0e44-0000-4000-8000-000000000002",
		"uptime_monitor_id": "3cac0e44-0000-4000-8000-000000000003",
		"workflow_id":       "7",
		"from":              "2026-08-29T00:00:00Z",
		"to":                "2026-08-30T00:00:00Z",
		"updated_since":     "2026-08-28T00:00:00Z",
		"order":             "updated_at",
		"direction":         "asc",
		"page":              "1",
		"per_page":          "100",
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

func TestClient_ListIncidents_NoFiltersSendsOnlyPagination(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	if _, err := c.ListIncidents(context.Background(), IncidentListOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotQuery) != 2 || gotQuery.Get("page") != "1" || gotQuery.Get("per_page") != "100" {
		t.Errorf("expected only page/per_page, got %v", gotQuery)
	}
}

// WorkflowID is a *int64 precisely so that workflow 0 is a real filter and
// "unset" is the absence of the key. A plain int64 would make them the same.
func TestClient_ListIncidents_ZeroWorkflowIDIsSent(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	zero := int64(0)
	if _, err := c.ListIncidents(context.Background(), IncidentListOptions{WorkflowID: &zero}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gotQuery.Get("workflow_id"); got != "0" {
		t.Errorf("expected workflow_id=0 to be sent, got %q (query: %v)", got, gotQuery)
	}
}

func TestClient_ListIntegrations_Filters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integrations": []map[string]interface{}{},
			"meta":         map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	enabled := false
	_, err := c.ListIntegrations(context.Background(), IntegrationListOptions{
		Type:         "SlackIntegration",
		Enabled:      &enabled,
		Q:            "ops",
		UpdatedSince: "2026-08-30T12:00:00Z",
		Order:        "type",
		Direction:    "asc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"type":          "SlackIntegration",
		"enabled":       "false",
		"q":             "ops",
		"updated_since": "2026-08-30T12:00:00Z",
		"order":         "type",
		"direction":     "asc",
		"page":          "1",
		"per_page":      "100",
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

// enabled=false is a real filter value, not "unset" — the *bool is what keeps
// the two apart, and dropping it would silently widen every disabled-channel
// query to the whole index.
func TestClient_ListIntegrations_NoFiltersSendsOnlyPagination(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"integrations": []map[string]interface{}{},
			"meta":         map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	if _, err := c.ListIntegrations(context.Background(), IntegrationListOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotQuery) != 2 || gotQuery.Get("page") != "1" || gotQuery.Get("per_page") != "100" {
		t.Errorf("expected only page/per_page, got %v", gotQuery)
	}
}

// A filter must survive the pagination walk: dropping it after page 1 would
// widen the query mid-list and pull in rows the caller filtered out.
func TestClient_ListIncidents_FiltersPersistAcrossPages(t *testing.T) {
	var seen []string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("status"))
		page := len(seen)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{{"id": page, "title": "x", "status": "open"}},
			"meta":      map[string]int{"current_page": page, "total_pages": 3, "total_count": 3, "per_page": 1},
		})
	})

	if _, err := c.ListIncidents(context.Background(), IncidentListOptions{Status: "open"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 pages walked, got %d", len(seen))
	}
	for i, got := range seen {
		if got != "open" {
			t.Errorf("page %d sent status=%q, want %q", i+1, got, "open")
		}
	}
}

// --- Workflow runs (#21) ---

func TestClient_ListWorkflowRuns_FiltersAndFields(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"runs": []map[string]interface{}{
				{
					"id": 10, "status": "running", "resource_key": "web-1",
					"workflow_id": 3, "workflow_version_id": 9,
					"started_at": "2026-01-01T00:00:00Z", "completed_at": nil,
					"duration_seconds": 42,
					"created_at":       "2026-01-01T00:00:00Z",
					"updated_at":       "2026-01-01T00:00:10Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	runs, err := c.ListWorkflowRuns(context.Background(), 3, WorkflowRunListOptions{
		Status:       "running",
		UpdatedSince: "2026-01-01T00:00:00Z",
		Order:        "started_at",
		Direction:    "asc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/workflows/3/runs" {
		t.Errorf("unexpected path: %s", gotPath)
	}

	want := map[string]string{
		"status":        "running",
		"updated_since": "2026-01-01T00:00:00Z",
		"order":         "started_at",
		"direction":     "asc",
		"page":          "1",
		"per_page":      "100",
	}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
	if len(gotQuery) != len(want) {
		t.Errorf("expected exactly %d query params, got %d: %v", len(want), len(gotQuery), gotQuery)
	}

	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].WorkflowID != 3 || runs[0].WorkflowVersionID != 9 {
		t.Errorf("expected workflow_id 3 / version 9, got %d / %d", runs[0].WorkflowID, runs[0].WorkflowVersionID)
	}
	if runs[0].DurationSeconds == nil || *runs[0].DurationSeconds != 42 {
		t.Errorf("expected duration_seconds 42, got %v", runs[0].DurationSeconds)
	}
	if runs[0].UpdatedAt != "2026-01-01T00:00:10Z" {
		t.Errorf("expected updated_at to be mapped, got %q", runs[0].UpdatedAt)
	}
	if runs[0].CompletedAt != nil {
		t.Errorf("expected null completed_at to stay nil, got %q", *runs[0].CompletedAt)
	}
	// A fan-out run carries its subject; the plain-string version of this field
	// could not tell that from the null a dispatch-once run returns.
	if runs[0].ResourceKey == nil || *runs[0].ResourceKey != "web-1" {
		t.Errorf("expected resource_key web-1, got %v", runs[0].ResourceKey)
	}
}

func TestClient_GetWorkflowRun(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/3/runs/10" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
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

	run, err := c.GetWorkflowRun(context.Background(), 3, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The header fields are promoted from the embedded WorkflowRun.
	if run.ID != 10 || run.Status != "failed" || run.WorkflowVersionID != 9 {
		t.Errorf("unexpected run header: %+v", run.WorkflowRun)
	}
	if run.Error == nil || *run.Error != "trigger raised" {
		t.Errorf("expected run-level error, got %v", run.Error)
	}
	if run.TriggerOutput["value"] != float64(91) {
		t.Errorf("expected trigger_output to decode, got %v", run.TriggerOutput)
	}
	if len(run.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(run.Steps))
	}
	if run.Steps[0].ErrorMessage == nil || *run.Steps[0].ErrorMessage != "smtp timeout" {
		t.Errorf("expected step error_message, got %v", run.Steps[0].ErrorMessage)
	}
	if run.Steps[0].DurationSeconds == nil || *run.Steps[0].DurationSeconds != 1.25 {
		t.Errorf("expected fractional step duration 1.25, got %v", run.Steps[0].DurationSeconds)
	}
	// Null, never 0, while a step has not both started and finished.
	if run.Steps[1].DurationSeconds != nil {
		t.Errorf("expected nil duration on the unfinished step, got %v", *run.Steps[1].DurationSeconds)
	}
	if run.Steps[0].OutputData["delivered"] != false {
		t.Errorf("expected step output_data to decode, got %v", run.Steps[0].OutputData)
	}
	// Null on a workflow that dispatches once — never "".
	if run.ResourceKey != nil {
		t.Errorf("expected nil resource_key on a non-fan-out run, got %q", *run.ResourceKey)
	}
}

// Run ids are not global: a run belonging to another workflow is a 404 here,
// and that has to surface as an error rather than an empty run.
func TestClient_GetWorkflowRun_404(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	})

	_, err := c.GetWorkflowRun(context.Background(), 3, 999)
	if err == nil {
		t.Fatal("expected an error for a run on another workflow")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != 404 {
		t.Fatalf("expected 404 APIError, got %v", err)
	}
}

// --- Metrics ---

func TestClient_QueryMetrics(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"items": []map[string]interface{}{
					{"name": "web-01", "host_id": "host-uuid", "monitor_id": nil, "value": 42.5, "formatted": "42.5%"},
				},
			},
		})
	})

	limit := int64(10)
	result, err := c.QueryMetrics(context.Background(), MetricsQueryRequest{
		Hosts: []string{"host-uuid"},
		Query: MetricsQuerySpec{
			Resource:    "cpu_usage",
			Format:      "aggregated",
			Aggregation: "avg",
			Limit:       &limit,
			Filters:     map[string][]string{"device": {"sda"}},
		},
		TimeRange: MetricsTimeRange{From: "2026-06-15T13:00:00Z"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != "POST" || gotPath != "/api/v1/metrics/query" {
		t.Errorf("expected POST /api/v1/metrics/query, got %s %s", gotMethod, gotPath)
	}
	query := gotBody["query"].(map[string]interface{})
	if query["resource"] != "cpu_usage" || query["format"] != "aggregated" || query["aggregation"] != "avg" {
		t.Errorf("unexpected query object: %v", query)
	}
	if query["limit"] != float64(10) {
		t.Errorf("expected limit 10, got %v", query["limit"])
	}
	if _, ok := query["merge_labels"]; ok {
		t.Error("expected unset merge_labels to be omitted from the request body")
	}
	if gotBody["time_range"].(map[string]interface{})["from"] != "2026-06-15T13:00:00Z" {
		t.Errorf("unexpected time_range: %v", gotBody["time_range"])
	}

	if len(result.Result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Result.Items))
	}
	if got := result.Result.Items[0].EntityID(); got != "host-uuid" {
		t.Errorf("expected entity id host-uuid, got %q", got)
	}
	if *result.Result.Items[0].Value != 42.5 {
		t.Errorf("expected value 42.5, got %v", *result.Result.Items[0].Value)
	}
}

// The API declares time_range required on /metrics/query, so the key must be
// present even when the caller sets no window and the endpoint default applies.
func TestClient_QueryMetrics_AlwaysSendsTimeRange(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{"result": map[string]interface{}{"type": "aggregated"}})
	})

	c.QueryMetrics(context.Background(), MetricsQueryRequest{
		Hosts: []string{"host-uuid"},
		Query: MetricsQuerySpec{Resource: "cpu_usage", Format: "aggregated", Aggregation: "avg"},
	})

	if _, ok := gotBody["time_range"]; !ok {
		t.Error("expected time_range key to be sent even when empty")
	}
}

func TestClient_QueryMetrics_TimeSeries(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "time_series",
				"series": []map[string]interface{}{
					{"name": "web-01", "host_id": "host-uuid", "data": [][]float64{{1781000000, 12.5}, {1781000060, 13}}},
				},
			},
		})
	})

	result, err := c.QueryMetrics(context.Background(), MetricsQueryRequest{
		Hosts: []string{"host-uuid"},
		Query: MetricsQuerySpec{Resource: "cpu_usage", Format: "time_series", Aggregation: "avg"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Result.Series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(result.Result.Series))
	}
	data := result.Result.Series[0].Data
	if len(data) != 2 || data[0][0] != 1781000000 || data[0][1] != 12.5 {
		t.Errorf("unexpected data points: %v", data)
	}
}

func TestClient_MetricsUptime(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "uptime",
				"items": []map[string]interface{}{
					{"monitor_id": "monitor-uuid", "name": "API", "value": 99.98, "formatted": "99.98%"},
				},
				"series": []map[string]interface{}{
					{"monitor_id": "monitor-uuid", "name": "API", "blocks": []map[string]interface{}{
						{"from": 1781000000, "to": 1781003600, "status": "up", "uptime": 100.0},
					}},
				},
			},
		})
	})

	collapse := true
	result, err := c.MetricsUptime(context.Background(), MetricsUptimeRequest{
		Monitors:      []string{"monitor-uuid"},
		CollapseScope: &collapse,
		Aggregation:   "min",
		TimeRange:     &MetricsTimeRange{From: "2026-08-01T00:00:00Z", To: "2026-09-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/api/v1/metrics/uptime" {
		t.Errorf("expected /api/v1/metrics/uptime, got %s", gotPath)
	}
	if gotBody["collapse_scope"] != true || gotBody["aggregation"] != "min" {
		t.Errorf("unexpected body: %v", gotBody)
	}
	if _, ok := gotBody["hosts"]; ok {
		t.Error("expected empty hosts to be omitted from the request body")
	}

	// Monitor targets report monitor_id, not host_id.
	if got := result.Result.Items[0].EntityID(); got != "monitor-uuid" {
		t.Errorf("expected entity id monitor-uuid, got %q", got)
	}
	if got := result.Result.Series[0].EntityID(); got != "monitor-uuid" {
		t.Errorf("expected series entity id monitor-uuid, got %q", got)
	}
	block := result.Result.Series[0].Blocks[0]
	if block.From != 1781000000 || block.Status != "up" || block.Uptime != 100.0 {
		t.Errorf("unexpected block: %+v", block)
	}
}

// Task targets report task_id, and a collapsed result reports no id at all.
func TestClient_MetricsUptime_TaskAndCollapsedIDs(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "uptime",
				"items": []map[string]interface{}{
					{"task_id": "task-uuid", "name": "Backup", "value": 100.0, "formatted": "100%"},
					{"name": "Uptime (min)", "value": 99.5, "formatted": "99.5%"},
				},
			},
		})
	})

	result, err := c.MetricsUptime(context.Background(), MetricsUptimeRequest{Tasks: []string{"task-uuid"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := result.Result.Items[0].EntityID(); got != "task-uuid" {
		t.Errorf("expected entity id task-uuid, got %q", got)
	}
	if got := result.Result.Items[1].EntityID(); got != "" {
		t.Errorf("expected collapsed item to carry no entity id, got %q", got)
	}
}

func TestClient_MetricsMonitorSSL(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"items": []map[string]interface{}{
					{"monitor_id": "monitor-uuid", "name": "API", "value": -2.5, "formatted": "Expired"},
				},
			},
		})
	})

	result, err := c.MetricsMonitorSSL(context.Background(), MetricsMonitorSSLRequest{
		Monitors: []string{"monitor-uuid"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/api/v1/metrics/monitor_ssl" {
		t.Errorf("expected /api/v1/metrics/monitor_ssl, got %s", gotPath)
	}
	if _, ok := gotBody["collapse_scope"]; ok {
		t.Error("expected unset collapse_scope to be omitted from the request body")
	}
	if *result.Result.Items[0].Value != -2.5 || result.Result.Items[0].Formatted != "Expired" {
		t.Errorf("unexpected expired item: %+v", result.Result.Items[0])
	}
}

func TestClient_MetricsIncidentStats(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "time_series",
				"unit": "duration",
				"series": []map[string]interface{}{
					{"name": "MTTR", "data": [][]float64{{1781000000, 8100}}},
				},
			},
		})
	})

	result, err := c.MetricsIncidentStats(context.Background(), MetricsIncidentStatsRequest{
		Metric:    "incident_mttr",
		Format:    "time_series",
		TimeRange: &MetricsTimeRange{From: "2026-08-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/api/v1/metrics/incident_stats" {
		t.Errorf("expected /api/v1/metrics/incident_stats, got %s", gotPath)
	}
	if gotBody["metric"] != "incident_mttr" {
		t.Errorf("unexpected metric: %v", gotBody["metric"])
	}
	if _, ok := gotBody["group_by"]; ok {
		t.Error("expected unset group_by to be omitted from the request body")
	}
	if result.Result.Unit != "duration" {
		t.Errorf("expected unit duration, got %q", result.Result.Unit)
	}
	if result.Result.Series[0].Data[0][1] != 8100 {
		t.Errorf("unexpected MTTR point: %v", result.Result.Series[0].Data)
	}
}

func TestClient_MetricsCveStats(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"items": []map[string]interface{}{
					{"name": "Critical", "value": 3, "formatted": "3"},
					{"name": "High", "value": 11, "formatted": "11"},
				},
			},
		})
	})

	result, err := c.MetricsCveStats(context.Background(), MetricsCveStatsRequest{
		Metric:  "cve_actionable_count",
		GroupBy: "severity",
		Hosts:   []string{"host-uuid"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/api/v1/metrics/cve_stats" {
		t.Errorf("expected /api/v1/metrics/cve_stats, got %s", gotPath)
	}
	if gotBody["group_by"] != "severity" {
		t.Errorf("unexpected group_by: %v", gotBody["group_by"])
	}
	if len(result.Result.Items) != 2 || *result.Result.Items[0].Value != 3 {
		t.Errorf("unexpected items: %+v", result.Result.Items)
	}
}

func TestClient_Metrics_APIError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": "Too many targets: 51 (max 50 per query)"})
	})

	_, err := c.MetricsUptime(context.Background(), MetricsUptimeRequest{Hosts: []string{"host-uuid"}})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != 422 || apiErr.Message != "Too many targets: 51 (max 50 per query)" {
		t.Errorf("unexpected error: %+v", apiErr)
	}
}
