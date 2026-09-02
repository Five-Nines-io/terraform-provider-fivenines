package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// --- Organization ---

func TestClient_GetOrganization(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/organization" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("ETag", `"org-etag-gzip"`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"organization": map[string]interface{}{
				"id": 42, "name": "Acme Corp", "slug": "acme-corp",
				"display_name": "Acme Corp", "plan": "pro", "trialing": false,
				"seats_used": 4, "seats_total": 10, "seats_remaining": 6,
				"members_count": 3, "pending_invitations_count": 1,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			},
		})
	})

	org, etag, err := c.GetOrganization(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag != `"org-etag"` {
		t.Errorf("expected the -gzip suffix stripped, got %q", etag)
	}
	if org.ID != 42 || org.Slug != "acme-corp" {
		t.Errorf("unexpected organization: %+v", org)
	}
	if org.Name == nil || *org.Name != "Acme Corp" {
		t.Errorf("expected name Acme Corp, got %v", org.Name)
	}
	if org.SeatsTotal == nil || *org.SeatsTotal != 10 {
		t.Errorf("expected seats_total 10, got %v", org.SeatsTotal)
	}
	if org.SeatsRemaining != 6 {
		t.Errorf("expected seats_remaining 6, got %d", org.SeatsRemaining)
	}
}

// An organization that was never named, on a plan with no seat cap.
func TestClient_GetOrganization_NullFields(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"organization": map[string]interface{}{
				"id": 42, "name": nil, "slug": "acme-corp", "seats_total": nil,
			},
		})
	})

	org, _, err := c.GetOrganization(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if org.Name != nil {
		t.Errorf("expected nil name, got %q", *org.Name)
	}
	if org.SeatsTotal != nil {
		t.Errorf("expected nil seats_total, got %d", *org.SeatsTotal)
	}
}

func TestClient_UpdateOrganization(t *testing.T) {
	var gotIfMatch string
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/organization" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotIfMatch = r.Header.Get("If-Match")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"organization": map[string]interface{}{"id": 42, "name": "New Name"},
		})
	})

	name := "New Name"
	org, err := c.UpdateOrganization(context.Background(), `"org-etag"`, UpdateOrganizationInput{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIfMatch != `"org-etag"` {
		t.Errorf("expected If-Match to carry the read's ETag, got %q", gotIfMatch)
	}
	if gotBody["organization"]["name"] != "New Name" {
		t.Errorf("expected an organization wrapper carrying name, got %+v", gotBody)
	}
	if org.Name == nil || *org.Name != "New Name" {
		t.Errorf("unexpected organization: %+v", org)
	}
}

func TestClient_ListOrganizationMembers_Pagination(t *testing.T) {
	var pages []string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organization/members" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("expected per_page=100, got %q", got)
		}
		// current_page has to echo the request: morePages compares it against
		// total_pages, so a stub that always says page 1 would walk forever.
		current, _ := strconv.Atoi(page)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"members": []map[string]interface{}{
				{"id": current, "user_id": current, "email": "a@acme.com", "role": "member"},
			},
			"meta": map[string]int{"current_page": current, "total_pages": 2, "total_count": 2, "per_page": 100},
		})
	})

	members, err := c.ListOrganizationMembers(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pages) != 2 || pages[0] != "1" || pages[1] != "2" {
		t.Errorf("expected the walk to fetch pages 1 and 2, got %v", pages)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members across 2 pages, got %d", len(members))
	}
}

func TestClient_ListOrganizationMembers(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"members": []map[string]interface{}{
				{
					"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "member",
					"two_factor_enabled": true,
					"joined_at":          "2026-01-01T00:00:00Z",
					"updated_at":         "2026-01-02T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	members, err := c.ListOrganizationMembers(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0].ID != 7 || members[0].UserID != 91 {
		t.Errorf("expected membership 7 / user 91, got %d / %d", members[0].ID, members[0].UserID)
	}
	if !members[0].TwoFactorEnabled {
		t.Error("expected two_factor_enabled true")
	}
}

func TestClient_UpdateOrganizationMember(t *testing.T) {
	var gotDryRun string
	var gotBody map[string]map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/organization/members/7" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotDryRun = r.Header.Get("X-Dry-Run")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"member": map[string]interface{}{"id": 7, "email": "engineer@acme.com", "role": "admin"},
		})
	})

	member, err := c.UpdateOrganizationMember(context.Background(), 7, UpdateOrganizationMemberInput{Role: "admin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotDryRun != "" {
		t.Errorf("a plain context must not send X-Dry-Run, got %q", gotDryRun)
	}
	if gotBody["membership"]["role"] != "admin" {
		t.Errorf("expected a membership wrapper carrying role, got %+v", gotBody)
	}
	if member.Role != "admin" {
		t.Errorf("expected role admin, got %q", member.Role)
	}
}

// The role change and the removal are the two writes ModifyPlan pre-flights, so
// both have to carry the header when the context is marked.
func TestClient_OrganizationMemberWrites_HonourWithDryRun(t *testing.T) {
	t.Run("role change", func(t *testing.T) {
		var gotDryRun string
		_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotDryRun = r.Header.Get("X-Dry-Run")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"member": map[string]interface{}{"id": 7, "role": "admin"},
			})
		})

		if _, err := c.UpdateOrganizationMember(WithDryRun(context.Background()), 7, UpdateOrganizationMemberInput{Role: "admin"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotDryRun != dryRunHeaderValue {
			t.Errorf("expected X-Dry-Run: %s, got %q", dryRunHeaderValue, gotDryRun)
		}
	})

	t.Run("removal", func(t *testing.T) {
		var gotDryRun string
		_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotDryRun = r.Header.Get("X-Dry-Run")
			w.WriteHeader(http.StatusNoContent)
		})

		if err := c.DeleteOrganizationMember(WithDryRun(context.Background()), 7); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotDryRun != dryRunHeaderValue {
			t.Errorf("expected X-Dry-Run: %s, got %q", dryRunHeaderValue, gotDryRun)
		}
	})
}

func TestClient_DeleteOrganizationMember(t *testing.T) {
	var gotMethod, gotPath, gotDryRun string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotDryRun = r.Header.Get("X-Dry-Run")
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteOrganizationMember(context.Background(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "DELETE" || gotPath != "/api/v1/organization/members/7" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
	if gotDryRun != "" {
		t.Errorf("a real removal must not send X-Dry-Run, got %q", gotDryRun)
	}
}

// The refusal the plan-time pre-flight exists to surface.
func TestClient_DeleteOrganizationMember_Refused(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "You cannot remove yourself", "code": ErrCodeForbidden, "request_id": "req-123",
		})
	})

	err := c.DeleteOrganizationMember(context.Background(), 7)
	apiErr := AsAPIError(err)
	if apiErr == nil {
		t.Fatalf("expected an *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", apiErr.StatusCode)
	}
	if apiErr.Code != ErrCodeForbidden {
		t.Errorf("expected code %q, got %q", ErrCodeForbidden, apiErr.Code)
	}
	if apiErr.RequestID != "req-123" {
		t.Errorf("expected the request id to survive, got %q", apiErr.RequestID)
	}
}

func TestClient_ListOrganizationInvitations(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organization/invitations" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"invitations": []map[string]interface{}{
				{
					"id": 12, "email": "newhire@acme.com", "role": "member", "status": "pending",
					"invited_by": "admin@acme.com", "expires_at": "2026-09-08T00:00:00Z",
					"accepted_at": nil, "created_at": "2026-09-01T00:00:00Z",
					"updated_at": "2026-09-01T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	invitations, err := c.ListOrganizationInvitations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(invitations) != 1 {
		t.Fatalf("expected 1 invitation, got %d", len(invitations))
	}
	if invitations[0].Status != "pending" {
		t.Errorf("expected status pending, got %q", invitations[0].Status)
	}
	if invitations[0].AcceptedAt != nil {
		t.Errorf("expected nil accepted_at, got %q", *invitations[0].AcceptedAt)
	}
	if invitations[0].InvitedBy == nil || *invitations[0].InvitedBy != "admin@acme.com" {
		t.Errorf("unexpected invited_by: %v", invitations[0].InvitedBy)
	}
}

func TestClient_ListOrganizationInvitations_Pagination(t *testing.T) {
	var pages []string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		current, _ := strconv.Atoi(page)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"invitations": []map[string]interface{}{{"id": current, "email": "a@acme.com", "role": "member"}},
			"meta":        map[string]int{"current_page": current, "total_pages": 3, "total_count": 3, "per_page": 100},
		})
	})

	invitations, err := c.ListOrganizationInvitations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pages) != 3 {
		t.Errorf("expected 3 pages walked, got %v", pages)
	}
	if len(invitations) != 3 {
		t.Errorf("expected 3 invitations, got %d", len(invitations))
	}
}

func TestClient_CreateOrganizationInvitation(t *testing.T) {
	var gotBody map[string]map[string]interface{}
	var gotDryRun string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/organization/invitations" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotDryRun = r.Header.Get("X-Dry-Run")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"invitation": map[string]interface{}{
				"id": 12, "email": "newhire@acme.com", "role": "admin", "status": "pending",
			},
		})
	})

	invitation, err := c.CreateOrganizationInvitation(context.Background(), CreateOrganizationInvitationInput{
		Email: "newhire@acme.com",
		Role:  "admin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotDryRun != "" {
		t.Errorf("a real invite must not send X-Dry-Run — the email cannot be rolled back; got %q", gotDryRun)
	}
	if gotBody["invitation"]["email"] != "newhire@acme.com" || gotBody["invitation"]["role"] != "admin" {
		t.Errorf("expected an invitation wrapper carrying email and role, got %+v", gotBody)
	}
	if invitation.ID != 12 {
		t.Errorf("expected id 12, got %d", invitation.ID)
	}
}

func TestClient_CreateOrganizationInvitation_SeatLimit(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"Seat limit reached"}})
	})

	_, err := c.CreateOrganizationInvitation(context.Background(), CreateOrganizationInvitationInput{
		Email: "newhire@acme.com",
	})
	apiErr := AsAPIError(err)
	if apiErr == nil {
		t.Fatalf("expected an *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", apiErr.StatusCode)
	}
}

func TestClient_DeleteOrganizationInvitation(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteOrganizationInvitation(context.Background(), 12); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "DELETE" || gotPath != "/api/v1/organization/invitations/12" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
}

func TestClient_GetOrganizationSecurity(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organization/security" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"security": map[string]interface{}{
				"require_two_factor": true, "two_factor_enforced_at": "2026-08-01T00:00:00Z",
				"members_count": 8, "members_with_two_factor": 5,
				"members_pending_two_factor": 3, "sso_enforced": false,
			},
		})
	})

	security, err := c.GetOrganizationSecurity(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !security.RequireTwoFactor {
		t.Error("expected require_two_factor true")
	}
	if security.MembersPendingTwoFactor != 3 {
		t.Errorf("expected 3 pending, got %d", security.MembersPendingTwoFactor)
	}
	if security.TwoFactorEnforcedAt == nil {
		t.Error("expected two_factor_enforced_at to be set while the policy is on")
	}
}

// "This tenant has no SAML" is an answer, not a 404: every key comes back null
// or false, so a sweep never branches on key presence.
func TestClient_GetOrganizationSAML_NeverConfigured(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organization/saml" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"saml": map[string]interface{}{
				"configured": false, "enabled": false, "enforce_sso": false,
				"idp_entity_id": nil, "idp_sso_url": nil, "idp_certificate_expires_at": nil,
				"session_duration_hours": nil, "domains": []string{}, "updated_at": nil,
			},
		})
	})

	saml, err := c.GetOrganizationSAML(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saml.Configured || saml.Enabled {
		t.Error("expected an unconfigured SAML posture")
	}
	if saml.IdpSSOURL != nil || saml.IdpCertificateExpiresAt != nil || saml.SessionDurationHours != nil {
		t.Error("expected the null IdP fields to decode as nil")
	}
	if len(saml.Domains) != 0 {
		t.Errorf("expected no domains, got %v", saml.Domains)
	}
}

func TestClient_GetOrganizationSAML_Configured(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"saml": map[string]interface{}{
				"configured": true, "enabled": true, "enforce_sso": true,
				"idp_sso_url": "https://idp.acme.com/sso", "idp_certificate_present": true,
				"idp_certificate_expires_at": "2027-01-01T00:00:00Z",
				"session_duration_hours":     24,
				"domains":                    []string{"acme.com", "acme.io"},
			},
		})
	})

	saml, err := c.GetOrganizationSAML(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The date nothing else surfaces: when it passes, every member is locked out.
	if saml.IdpCertificateExpiresAt == nil || *saml.IdpCertificateExpiresAt != "2027-01-01T00:00:00Z" {
		t.Errorf("unexpected certificate expiry: %v", saml.IdpCertificateExpiresAt)
	}
	if saml.SessionDurationHours == nil || *saml.SessionDurationHours != 24 {
		t.Errorf("unexpected session duration: %v", saml.SessionDurationHours)
	}
	if len(saml.Domains) != 2 {
		t.Errorf("expected 2 domains, got %v", saml.Domains)
	}
}
