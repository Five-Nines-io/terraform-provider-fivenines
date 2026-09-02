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

// A page whose records are all filtered out still means there is more to fetch.
func TestClient_ListWorkflows_FullyArchivedPageContinues(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		workflow := map[string]interface{}{"id": 1, "name": "archived", "status": "archived"}
		if page == 2 {
			workflow = map[string]interface{}{"id": 2, "name": "live", "status": "active"}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []interface{}{workflow},
			"meta":      map[string]int{"current_page": page, "total_pages": 2, "total_count": 2, "per_page": 100},
		})
	})

	workflows, err := c.ListWorkflows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workflows) != 1 || workflows[0].Name != "live" {
		t.Errorf("expected the live workflow from page 2, got %v", workflows)
	}
}

func TestClient_ListWorkflows_FiltersArchived(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []map[string]interface{}{
				{"id": 1, "name": "active-wf", "status": "active", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
				{"id": 2, "name": "archived-wf", "status": "archived", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
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
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
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
	if dev.Vendor == nil || *dev.Vendor != "Cisco" {
		t.Errorf("expected vendor Cisco, got %v", dev.Vendor)
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
