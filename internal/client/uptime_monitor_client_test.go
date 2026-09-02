package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// --- ListUptimeMonitors: pagination + errors ---

func TestClient_ListUptimeMonitors_Pagination(t *testing.T) {
	t.Run("filters survive the page walk", func(t *testing.T) {
		var queries []url.Values
		_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			queries = append(queries, r.URL.Query())
			if r.URL.Query().Get("page") == "1" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"uptime_monitors": []interface{}{
						map[string]interface{}{"id": "mon-1", "name": "one", "protocol": "https", "status": "up"},
					},
					"meta": map[string]int{"current_page": 1, "total_pages": 2, "total_count": 2, "per_page": 100},
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"uptime_monitors": []interface{}{
					map[string]interface{}{"id": "mon-2", "name": "two", "protocol": "https", "status": "down"},
				},
				"meta": map[string]int{"current_page": 2, "total_pages": 2, "total_count": 2, "per_page": 100},
			})
		})

		monitors, err := c.ListUptimeMonitors(context.Background(), &ListUptimeMonitorsOptions{Protocol: "https"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(monitors) != 2 {
			t.Fatalf("expected 2 monitors across pages, got %d", len(monitors))
		}
		if len(queries) != 2 {
			t.Fatalf("expected 2 requests, got %d", len(queries))
		}
		// The filter must survive the page walk, otherwise page 2 silently widens
		// the result set.
		for i, want := range []string{"1", "2"} {
			if got := queries[i].Get("page"); got != want {
				t.Errorf("request %d: expected page=%s, got %q", i, want, got)
			}
			if got := queries[i].Get("protocol"); got != "https" {
				t.Errorf("request %d: expected protocol=https, got %q", i, got)
			}
			if got := queries[i].Get("per_page"); got != "100" {
				t.Errorf("request %d: expected per_page=100, got %q", i, got)
			}
		}
	})

	// A recognised meta is authoritative, so the empty-page guard is what stops a
	// walk whose meta the client cannot read at all. Without it an unreadable
	// envelope would either truncate at page 1 or issue requests forever.
	t.Run("an empty page ends a walk with an unreadable meta", func(t *testing.T) {
		var requests int
		_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			requests++
			page := r.URL.Query().Get("page")
			monitors := []interface{}{}
			if page == "1" {
				monitors = append(monitors, map[string]interface{}{
					"id": "mon-1", "name": "one", "protocol": "https", "status": "up",
				})
			}
			// A meta shape the client does not know: every field decodes to zero,
			// so only the empty page can stop this.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"uptime_monitors": monitors,
				"meta":            map[string]int{"page": 1, "pages": 9999, "records": 9999},
			})
		})

		monitors, err := c.ListUptimeMonitors(context.Background(), &ListUptimeMonitorsOptions{Status: "down"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(monitors) != 1 {
			t.Errorf("expected the one monitor from page 1, got %d", len(monitors))
		}
		if requests != 2 {
			t.Errorf("expected the walk to stop after the empty page, got %d requests", requests)
		}
	})

	t.Run("the page ceiling ends the walk", func(t *testing.T) {
		_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			// Always a full page and always more to come: only the ceiling stops it.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"uptime_monitors": []interface{}{
					map[string]interface{}{"id": "mon-1", "name": "one", "protocol": "https", "status": "up"},
				},
				"meta": map[string]int{"current_page": 1, "total_pages": 9999999, "total_count": 9999999, "per_page": 100},
			})
		})

		monitors, err := c.ListUptimeMonitors(context.Background(), nil)
		if err == nil {
			t.Fatal("expected the page ceiling to abort the walk")
		}
		if monitors != nil {
			t.Errorf("expected no monitors on error, got %d", len(monitors))
		}
		if !strings.Contains(err.Error(), "pagination exceeded") {
			t.Errorf("expected a pagination ceiling error, got %v", err)
		}
	})
}

func TestClient_ListUptimeMonitors_Errors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantSubstr string
	}{
		{
			name:       "server error surfaces as an APIError",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":"boom"}`,
			wantSubstr: "API error 500: boom",
		},
		{
			name:       "malformed page body",
			statusCode: http.StatusOK,
			body:       `{not json`,
			wantSubstr: "decoding response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			})

			monitors, err := c.ListUptimeMonitors(context.Background(), nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if monitors != nil {
				t.Errorf("expected no monitors on error, got %v", monitors)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("expected error containing %q, got %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestUptimeMonitorFilters(t *testing.T) {
	tests := []struct {
		name string
		opts *ListUptimeMonitorsOptions
		want map[string]string
	}{
		{
			name: "nil options",
			opts: nil,
			want: map[string]string{},
		},
		{
			name: "all zero values are omitted",
			opts: &ListUptimeMonitorsOptions{},
			want: map[string]string{},
		},
		{
			name: "only the set filters are rendered",
			opts: &ListUptimeMonitorsOptions{Status: "down", Direction: "desc"},
			want: map[string]string{"status": "down", "direction": "desc"},
		},
		{
			name: "query maps to q",
			opts: &ListUptimeMonitorsOptions{Query: "api"},
			want: map[string]string{"q": "api"},
		},
		{
			name: "every filter",
			opts: &ListUptimeMonitorsOptions{
				Status: "up", Protocol: "dns", Query: "a",
				UpdatedSince: "2026-01-01T00:00:00Z", Order: "name", Direction: "asc",
			},
			want: map[string]string{
				"status": "up", "protocol": "dns", "q": "a",
				"updated_since": "2026-01-01T00:00:00Z", "order": "name", "direction": "asc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uptimeMonitorFilters(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			for k, v := range tt.want {
				if got.Get(k) != v {
					t.Errorf("expected %s=%q, got %q", k, v, got.Get(k))
				}
			}
		})
	}
}

// --- Pause/Resume error paths ---

func TestClient_PauseUptimeMonitor_Errors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantSubstr string
	}{
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			body:       `{"error":"Not Found"}`,
			wantSubstr: "API error 404: Not Found",
		},
		{
			// A 200 carrying a different monitor would be written straight to
			// state and blank the resource id.
			name:       "mismatched monitor id",
			statusCode: http.StatusOK,
			body:       `{"uptime_monitor":{"id":"other-uuid","status":"paused"}}`,
			wantSubstr: `unexpected monitor id "other-uuid"`,
		},
		{
			name:       "empty monitor in a 200",
			statusCode: http.StatusOK,
			body:       `{}`,
			wantSubstr: "unexpected monitor id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			})

			mon, err := c.PauseUptimeMonitor(context.Background(), "mon-uuid")
			if err == nil {
				t.Fatal("expected an error")
			}
			if mon != nil {
				t.Errorf("expected a nil monitor on error, got %v", mon)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("expected error containing %q, got %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestClient_ResumeUptimeMonitor_DecodeError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"uptime_monitor": `))
	})

	mon, err := c.ResumeUptimeMonitor(context.Background(), "mon-uuid")
	if err == nil {
		t.Fatal("expected a decode error for a truncated body")
	}
	if mon != nil {
		t.Errorf("expected a nil monitor on error, got %v", mon)
	}
	if !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("expected a decoding error, got %v", err)
	}
}

// --- GetUptimeMonitorStatus error paths ---

func TestClient_GetUptimeMonitorStatus_Errors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantSubstr string
	}{
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			body:       `{"error":"Not Found"}`,
			wantSubstr: "Not Found",
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"Invalid API key"}`,
			wantSubstr: "Invalid API key",
		},
		{
			name:       "malformed body",
			statusCode: http.StatusOK,
			body:       `not json at all`,
			wantSubstr: "decoding response",
		},
		{
			name:       "json array body",
			statusCode: http.StatusOK,
			body:       `[{"status":"up"}]`,
			wantSubstr: "decoding response",
		},
		{
			// An unrecognised shape decodes to a zero value, which downstream
			// reads as a healthy, unpaused monitor. It has to be rejected.
			name:       "unrecognised payload shape",
			statusCode: http.StatusOK,
			body:       `{"data":{"state":"up"}}`,
			wantSubstr: "unrecognized status payload",
		},
		{
			name:       "empty envelope",
			statusCode: http.StatusOK,
			body:       `{"uptime_monitor_status":{}}`,
			wantSubstr: "unrecognized status payload",
		},
		{
			name:       "status for a different monitor",
			statusCode: http.StatusOK,
			body:       `{"uptime_monitor_status":{"id":"other-uuid","status":"up"}}`,
			wantSubstr: `unexpected monitor id "other-uuid"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			})

			status, err := c.GetUptimeMonitorStatus(context.Background(), "mon-uuid")
			if err == nil {
				t.Fatal("expected an error")
			}
			if status != nil {
				t.Errorf("expected a nil status on error, got %v", status)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("expected error containing %q, got %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestClient_GetUptimeMonitorStatus_LegacyEnvelope(t *testing.T) {
	// The status endpoint's wrapper key is not pinned by the spec, so the
	// "uptime_monitor" fallback has to work end to end, not just in unwrapEnvelope.
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"uptime_monitor": map[string]interface{}{"id": "mon-uuid", "status": "recovering"},
		})
	})

	status, err := c.GetUptimeMonitorStatus(context.Background(), "mon-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "recovering" {
		t.Errorf("expected status recovering, got %q", status.Status)
	}
	if status.ID != "mon-uuid" {
		t.Errorf("expected id mon-uuid, got %q", status.ID)
	}
}

func TestUnwrapEnvelope(t *testing.T) {
	tests := []struct {
		name string
		body string
		keys []string
		want string
	}{
		{
			name: "unwraps the first key",
			body: `{"uptime_monitor_status":{"status":"up"},"uptime_monitor":{"status":"down"}}`,
			keys: []string{"uptime_monitor_status", "uptime_monitor"},
			want: `{"status":"up"}`,
		},
		{
			name: "falls back to the second key",
			body: `{"uptime_monitor":{"status":"down"}}`,
			keys: []string{"uptime_monitor_status", "uptime_monitor"},
			want: `{"status":"down"}`,
		},
		{
			name: "bare object is returned unchanged",
			body: `{"status":"up"}`,
			keys: []string{"uptime_monitor_status"},
			want: `{"status":"up"}`,
		},
		{
			name: "non-object body is returned unchanged",
			body: `[1,2,3]`,
			keys: []string{"uptime_monitor_status"},
			want: `[1,2,3]`,
		},
		{
			name: "invalid json is returned unchanged",
			body: `not json`,
			keys: []string{"uptime_monitor_status"},
			want: `not json`,
		},
		{
			name: "null under the key is not unwrapped",
			body: `{"uptime_monitor_status":null,"status":"up"}`,
			keys: []string{"uptime_monitor_status"},
			want: `{"uptime_monitor_status":null,"status":"up"}`,
		},
		{
			name: "array under the key is not unwrapped",
			body: `{"uptime_monitor_status":[],"status":"up"}`,
			keys: []string{"uptime_monitor_status"},
			want: `{"uptime_monitor_status":[],"status":"up"}`,
		},
		{
			name: "no keys given",
			body: `{"status":"up"}`,
			want: `{"status":"up"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(unwrapEnvelope([]byte(tt.body), tt.keys...))
			if got != tt.want {
				t.Errorf("unwrapEnvelope(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}
