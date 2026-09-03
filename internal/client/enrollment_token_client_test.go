package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// --- Enrollment Tokens ---

func TestClient_CreateEnrollmentToken(t *testing.T) {
	var gotBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/enrollment_tokens" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_token": map[string]interface{}{
				"id":                     7,
				"name":                   "Production fleet",
				"active":                 true,
				"hosts_registered_count": 0,
				"last_used_at":           nil,
				"created_at":             "2026-01-01T00:00:00Z",
				"updated_at":             "2026-01-01T00:00:00Z",
				"token":                  "a1b2c3",
				"install_command":        "wget ... && sudo sh fivenines_setup.sh a1b2c3",
			},
		})
	})

	token, err := c.CreateEnrollmentToken(context.Background(), CreateEnrollmentTokenInput{Name: "Production fleet"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wrapped, ok := gotBody["enrollment_token"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected the body wrapped in enrollment_token, got %v", gotBody)
	}
	if wrapped["name"] != "Production fleet" {
		t.Errorf("expected name 'Production fleet', got %v", wrapped["name"])
	}
	if token.ID != 7 {
		t.Errorf("expected id 7, got %d", token.ID)
	}
	if token.Token != "a1b2c3" {
		t.Errorf("expected token a1b2c3, got %q", token.Token)
	}
	if token.InstallCommand == "" {
		t.Error("expected an install_command")
	}
	if !token.Active {
		t.Error("expected a new token to be active")
	}
}

// The value exists for exactly one response. A create that decodes without one
// has lost it for good, so it must fail loudly rather than write a token to
// state that nothing can enroll with.
func TestClient_CreateEnrollmentToken_RejectsMissingTokenValue(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_token": map[string]interface{}{"id": 7, "name": "no value", "active": true},
		})
	})

	if _, err := c.CreateEnrollmentToken(context.Background(), CreateEnrollmentTokenInput{Name: "no value"}); err == nil {
		t.Fatal("expected an error when the create response carries no token value")
	}
}

func TestClient_CreateEnrollmentToken_RejectionIsAnError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Enrollment tokens are not available on your current plan. Please upgrade.",
		})
	})

	_, err := c.CreateEnrollmentToken(context.Background(), CreateEnrollmentTokenInput{Name: "gated"})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected an *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 422 {
		t.Errorf("expected status 422, got %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "not available on your current plan") {
		t.Errorf("expected the plan-gate message to survive, got %q", apiErr.Error())
	}
}

func TestClient_GetEnrollmentToken_ScansPages(t *testing.T) {
	var gotPaths []string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.String())
		if r.URL.Query().Get("page") == "1" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_tokens": []map[string]interface{}{
					{"id": 1, "name": "first", "active": true},
				},
				"meta": map[string]int{"current_page": 1, "total_pages": 2, "total_count": 2, "per_page": 100},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_tokens": []map[string]interface{}{
				{"id": 2, "name": "second", "active": false, "hosts_registered_count": 3,
					"last_used_at": "2026-02-01T00:00:00Z", "created_at": "2026-01-01T00:00:00Z",
					"updated_at": "2026-02-02T00:00:00Z"},
			},
			"meta": map[string]int{"current_page": 2, "total_pages": 2, "total_count": 2, "per_page": 100},
		})
	})

	token, err := c.GetEnrollmentToken(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.Name != "second" {
		t.Errorf("expected name 'second', got %q", token.Name)
	}
	if token.Active {
		t.Error("expected a revoked token to report active false")
	}
	if token.HostsRegisteredCount != 3 {
		t.Errorf("expected hosts_registered_count 3, got %d", token.HostsRegisteredCount)
	}
	if token.Token != "" {
		t.Errorf("expected the index to withhold the value, got %q", token.Token)
	}
	if len(gotPaths) != 2 {
		t.Fatalf("expected 2 requests, got %d: %v", len(gotPaths), gotPaths)
	}
	// Ascending order is what makes paging stable while a fleet bootstrap is
	// minting tokens: a new row is appended rather than shifting an unread one
	// onto a page already read.
	for _, p := range gotPaths {
		if !strings.Contains(p, "order=created_at") || !strings.Contains(p, "direction=asc") {
			t.Errorf("expected a stable ascending sort, got %q", p)
		}
	}
}

// The id is on page 1 of 5: the walk must stop there rather than read the whole
// index on every refresh.
func TestClient_GetEnrollmentToken_StopsAtMatch(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_tokens": []map[string]interface{}{{"id": 1, "name": "first", "active": true}},
			"meta":              map[string]int{"current_page": 1, "total_pages": 5, "total_count": 5, "per_page": 100},
		})
	})

	if _, err := c.GetEnrollmentToken(context.Background(), 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Errorf("expected the walk to stop at the match, got %d requests", got)
	}
}

// There is no GET /enrollment_tokens/:id, so "gone" has to be synthesized from
// an exhausted index. Resource Read keys its state removal off this 404.
func TestClient_GetEnrollmentToken_NotFound(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_tokens": []map[string]interface{}{{"id": 1, "name": "other", "active": true}},
			"meta":              map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	_, err := c.GetEnrollmentToken(context.Background(), 99)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected an *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", apiErr.StatusCode)
	}
}

// An unreadable meta envelope must not truncate the walk. This is the failure
// that hid behind eight green list tests before morePages: the id sits on page
// 2, and a walk that trusts a zeroed meta reports it missing — which the
// resource would read as "deleted" and mint a replacement for.
func TestClient_GetEnrollmentToken_WalksOnWhenMetaIsUnreadable(t *testing.T) {
	var requestCount int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&requestCount, 1)
		if page == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_tokens": []map[string]interface{}{{"id": 1, "name": "first", "active": true}},
				// The pre-2026-09 envelope: every field this client reads decodes
				// to zero.
				"meta": map[string]int{"count": 1, "total": 2, "offset": 0},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_tokens": []map[string]interface{}{{"id": 2, "name": "second", "active": true}},
			"meta":              map[string]int{"count": 1, "total": 2, "offset": 1},
		})
	})

	token, err := c.GetEnrollmentToken(context.Background(), 2)
	if err != nil {
		t.Fatalf("expected the walk to reach page 2, got %v", err)
	}
	if token.Name != "second" {
		t.Errorf("expected name 'second', got %q", token.Name)
	}
}

func TestClient_RevokeEnrollmentToken(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/enrollment_tokens/7/revoke" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_token": map[string]interface{}{
				"id": 7, "name": "Production fleet", "active": false,
				"hosts_registered_count": 5, "updated_at": "2026-03-01T00:00:00Z",
			},
		})
	})

	token, err := c.RevokeEnrollmentToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.Active {
		t.Error("expected active false after revoke")
	}
	if token.Token != "" {
		t.Errorf("expected revoke to withhold the value, got %q", token.Token)
	}
}

// A 200 that decodes to someone else's token would otherwise be reported as a
// successful revoke of a credential still live in the fleet.
func TestClient_RevokeEnrollmentToken_RejectsMismatchedID(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_token": map[string]interface{}{"id": 99, "name": "someone else", "active": false},
		})
	})

	if _, err := c.RevokeEnrollmentToken(context.Background(), 7); err == nil {
		t.Fatal("expected an error when revoke returns a different token")
	}
}

func TestClient_RevokeEnrollmentToken_RejectionIsAnError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing write scope"})
	})

	_, err := c.RevokeEnrollmentToken(context.Background(), 7)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected an *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 403 {
		t.Errorf("expected status 403, got %d", apiErr.StatusCode)
	}
}

func TestClient_DeleteEnrollmentToken(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteEnrollmentToken(context.Background(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "DELETE" || gotPath != "/api/v1/enrollment_tokens/7" {
		t.Errorf("expected DELETE /api/v1/enrollment_tokens/7, got %s %s", gotMethod, gotPath)
	}
}

// A 200 is not a 204 here. The API answers 204 on a real delete, so accepting
// anything 2xx would report a token destroyed on a response that never said so.
func TestClient_DeleteEnrollmentToken_RejectsNon204(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"error": "not a delete"})
	})

	if err := c.DeleteEnrollmentToken(context.Background(), 7); err == nil {
		t.Fatal("expected a non-204 success status to be an error")
	}
}

// The API refuses to delete a token that has enrolled hosts. The resource keys
// its revoke fallback off this classification, so it has to hold — and it has to
// stay narrow: a 403 or 404 must not be mistaken for it.
func TestClient_DeleteEnrollmentToken_HasRegisteredHosts(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Cannot delete a token that has registered hosts. Revoke it instead.",
		})
	})

	err := c.DeleteEnrollmentToken(context.Background(), 7)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsTokenHasRegisteredHosts(err) {
		t.Errorf("expected the registered-hosts refusal to be recognized, got %v", err)
	}
	for _, other := range []error{
		&APIError{StatusCode: 404, Message: "not found"},
		&APIError{StatusCode: 403, Message: "missing write scope"},
	} {
		if IsTokenHasRegisteredHosts(other) {
			t.Errorf("%v is not the registered-hosts refusal", other)
		}
	}
	if IsTokenHasRegisteredHosts(context.Canceled) {
		t.Error("a non-API error is not the registered-hosts refusal")
	}
}
