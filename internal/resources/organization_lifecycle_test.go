package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The organization resources resolve a row out of a LIST rather than a
// per-object GET — the API exposes no per-member and no per-invitation read —
// so their drift, adoption and offboarding branches cannot be reached through
// the read-404 table in notfound_removal_test.go. These drive them directly.

// emptyState builds a null state carrying the given resource's schema.
func emptyState(t *testing.T, s rschema.Schema) tfsdk.State {
	t.Helper()
	objType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}
}

func seededState(t *testing.T, s rschema.Schema, model interface{}) tfsdk.State {
	t.Helper()
	state := emptyState(t, s)
	if d := state.Set(context.Background(), model); d.HasError() {
		t.Fatalf("seeding state: %v", d.Errors())
	}
	return state
}

func testServer(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return client.NewClient(srv.URL, "test-key")
}

func membersEnvelope(members ...map[string]interface{}) map[string]interface{} {
	if members == nil {
		members = []map[string]interface{}{}
	}
	return map[string]interface{}{
		"members": members,
		"meta": map[string]int{
			"current_page": 1, "total_pages": 1, "total_count": len(members), "per_page": 100,
		},
	}
}

func invitationsEnvelope(invitations ...map[string]interface{}) map[string]interface{} {
	if invitations == nil {
		invitations = []map[string]interface{}{}
	}
	return map[string]interface{}{
		"invitations": invitations,
		"meta": map[string]int{
			"current_page": 1, "total_pages": 1, "total_count": len(invitations), "per_page": 100,
		},
	}
}

// --- fivenines_organization ---

// Renaming the organization is the resource's only writable behaviour.
func TestOrganizationUpdate_RenamesTheOrganization(t *testing.T) {
	// Stateful, like the real API: the GET answers the name the organization
	// currently has, so the update actually has something to change.
	name := "Acme Corp"
	var patched string
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			var body struct {
				Organization struct {
					Name string `json:"name"`
				} `json:"organization"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			patched = body.Organization.Name
			name = patched
		}
		w.Header().Set("ETag", `"org-1"`)
		json.NewEncoder(w).Encode(map[string]interface{}{"organization": map[string]interface{}{
			"id": 42, "name": name, "slug": "acme-corp", "display_name": name,
		}})
	})

	ctx := context.Background()
	s := organizationSchemaForTest(t)
	plan := seededState(t, s, &organizationModel{ID: types.Int64Value(42), Name: types.StringValue("Renamed")})
	state := seededState(t, s, &organizationModel{ID: types.Int64Value(42), Name: types.StringValue("Acme Corp")})

	resp := &resource.UpdateResponse{State: state}
	(&organizationResource{client: c}).Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if patched != "Renamed" {
		t.Errorf("expected the new name to reach the API, got %q", patched)
	}
	var got organizationModel
	resp.State.Get(ctx, &got)
	if got.Name.ValueString() != "Renamed" {
		t.Errorf("expected state to carry the renamed organization, got %q", got.Name.ValueString())
	}
}

// A concurrent write invalidates the ETag between the read and the PATCH. The
// update has to re-READ the ETag before retrying — retrying with the same stale
// one just 412s again, and the apply fails on an organization that was only
// renamed underneath it.
func TestOrganizationUpdate_RefreshesTheETagBeforeRetrying(t *testing.T) {
	var gets int
	var ifMatch []string
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			ifMatch = append(ifMatch, r.Header.Get("If-Match"))
			if len(ifMatch) == 1 {
				w.WriteHeader(http.StatusPreconditionFailed)
				json.NewEncoder(w).Encode(map[string]interface{}{"error": "ETag mismatch"})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"organization": map[string]interface{}{
				"id": 42, "name": "Renamed", "slug": "acme-corp",
			}})
			return
		}
		// A fresh ETag per read, the way a server that is being written to
		// concurrently behaves. A stub that serves one fixed ETag cannot tell
		// "re-read then retry" from "retry with the stale value".
		gets++
		w.Header().Set("ETag", fmt.Sprintf(`"org-%d"`, gets))
		json.NewEncoder(w).Encode(map[string]interface{}{"organization": map[string]interface{}{
			"id": 42, "name": "Acme Corp", "slug": "acme-corp",
		}})
	})

	ctx := context.Background()
	s := organizationSchemaForTest(t)
	plan := seededState(t, s, &organizationModel{ID: types.Int64Value(42), Name: types.StringValue("Renamed")})

	resp := &resource.UpdateResponse{State: plan}
	(&organizationResource{client: c}).Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: plan,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a stale ETag must be retried, not surfaced: %v", resp.Diagnostics.Errors())
	}
	if len(ifMatch) != 2 {
		t.Fatalf("expected one retry after the 412, got %d PATCH attempts", len(ifMatch))
	}
	if ifMatch[0] == "" || ifMatch[1] == "" {
		t.Fatalf("both attempts must carry If-Match, got %q and %q", ifMatch[0], ifMatch[1])
	}
	if ifMatch[0] == ifMatch[1] {
		t.Errorf("the retry re-sent the stale ETag %q — the API will 412 again", ifMatch[0])
	}
}

// Adopting an organization already named what the configuration says is the
// common first apply, and it must not spend a write on it.
func TestOrganizationUpdate_SkipsTheWriteWhenTheNameMatches(t *testing.T) {
	var patches int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches++
		}
		w.Header().Set("ETag", `"org-1"`)
		json.NewEncoder(w).Encode(map[string]interface{}{"organization": map[string]interface{}{
			"id": 42, "name": "Acme Corp", "slug": "acme-corp",
		}})
	})

	ctx := context.Background()
	s := organizationSchemaForTest(t)
	plan := seededState(t, s, &organizationModel{ID: types.Int64Value(42), Name: types.StringValue("Acme Corp")})

	resp := &resource.UpdateResponse{State: plan}
	(&organizationResource{client: c}).Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: plan,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if patches != 0 {
		t.Errorf("expected no PATCH when the name already matches, got %d", patches)
	}
}

// The singleton has nothing to address, so import resolves it from the key.
func TestOrganizationImportState_IgnoresTheImportID(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"organization": map[string]interface{}{
			"id": 42, "name": "Acme Corp", "slug": "acme-corp",
		}})
	})

	ctx := context.Background()
	resp := &resource.ImportStateResponse{State: emptyState(t, organizationSchemaForTest(t))}
	(&organizationResource{client: c}).ImportState(ctx,
		resource.ImportStateRequest{ID: "anything-at-all"}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	var id types.Int64
	resp.State.GetAttribute(ctx, path.Root("id"), &id)
	if id.ValueInt64() != 42 {
		t.Errorf("expected the organization the key resolves, got id %d", id.ValueInt64())
	}
}

// --- fivenines_organization_member ---

// The API has no endpoint that creates a member, so Create adopts an existing
// membership and brings its role under Terraform.
func TestOrganizationMemberCreate_AdoptsAndPatchesADifferingRole(t *testing.T) {
	var patchedRole string
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			var body struct {
				Membership struct {
					Role string `json:"role"`
				} `json:"membership"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			patchedRole = body.Membership.Role
			json.NewEncoder(w).Encode(map[string]interface{}{"member": map[string]interface{}{
				"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "admin",
			}})
			return
		}
		json.NewEncoder(w).Encode(membersEnvelope(map[string]interface{}{
			"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "member",
		}))
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("admin"),
	})

	resp := &resource.CreateResponse{State: emptyState(t, s)}
	(&organizationMemberResource{client: c}).Create(ctx, resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if patchedRole != "admin" {
		t.Errorf("expected adoption to PATCH the differing role, got %q", patchedRole)
	}
	var got organizationMemberModel
	resp.State.Get(ctx, &got)
	if got.ID.ValueInt64() != 7 || got.Role.ValueString() != "admin" {
		t.Errorf("unexpected state after adoption: id=%d role=%q", got.ID.ValueInt64(), got.Role.ValueString())
	}
}

// Matching roles need no write: the API is idempotent, but a pointless PATCH
// is still a pointless PATCH.
func TestOrganizationMemberCreate_SkipsThePatchWhenTheRoleMatches(t *testing.T) {
	var patches int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches++
		}
		json.NewEncoder(w).Encode(membersEnvelope(map[string]interface{}{
			"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "member",
		}))
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := &resource.CreateResponse{State: emptyState(t, s)}
	(&organizationMemberResource{client: c}).Create(ctx, resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if patches != 0 {
		t.Errorf("expected no PATCH when the role already matches, got %d", patches)
	}
}

// Ownership can be neither assigned nor removed by any API surface, so an owner
// must be refused at adoption rather than at the first destructive apply.
func TestOrganizationMemberCreate_RefusesTheOwner(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			t.Error("an owner must be refused before any write is attempted")
		}
		json.NewEncoder(w).Encode(membersEnvelope(map[string]interface{}{
			"id": 1, "user_id": 2, "email": "founder@acme.com", "role": "owner",
		}))
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		Email: types.StringValue("founder@acme.com"), Role: types.StringValue("admin"),
	})

	resp := &resource.CreateResponse{State: emptyState(t, s)}
	(&organizationMemberResource{client: c}).Create(ctx, resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected adopting the organization owner to be refused")
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "owner") {
		t.Errorf("expected the diagnostic to say why, got %q", detail)
	}
}

// A membership that left the roster is drift: the person was offboarded
// elsewhere, and the next plan should offer to adopt them again if they return.
func TestOrganizationMemberRead_RemovesAMembershipGoneFromTheRoster(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(membersEnvelope())
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := &resource.ReadResponse{State: state}
	(&organizationMemberResource{client: c}).Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a departed member is drift, not an error: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Error("expected the membership to be removed from state")
	}
}

func TestOrganizationMemberDelete_ToleratesAnAlreadyOffboardedMember(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not found", "request_id": "req-404"})
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{ID: types.Int64Value(7)})

	resp := &resource.DeleteResponse{State: state}
	(&organizationMemberResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("destroying a member already gone must succeed: %v", resp.Diagnostics.Errors())
	}
}

// The mirror, and the one that matters most here: swallowing a refusal would
// drop a live member from state while they still hold access.
func TestOrganizationMemberDelete_SurfacesARefusal(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "You cannot remove an owner", "request_id": "req-403",
		})
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{ID: types.Int64Value(1)})

	resp := &resource.DeleteResponse{State: state}
	(&organizationMemberResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused removal must surface — the member still has access")
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "req-403") {
		t.Errorf("expected the request_id in the diagnostic, got %q", detail)
	}
}

func TestOrganizationMemberImportState(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(membersEnvelope(map[string]interface{}{
			"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "member",
		}))
	})
	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)

	t.Run("by membership id", func(t *testing.T) {
		resp := &resource.ImportStateResponse{State: emptyState(t, s)}
		(&organizationMemberResource{client: c}).ImportState(ctx,
			resource.ImportStateRequest{ID: "7"}, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
		}
		var id types.Int64
		resp.State.GetAttribute(ctx, path.Root("id"), &id)
		if id.ValueInt64() != 7 {
			t.Errorf("expected membership id 7, got %d", id.ValueInt64())
		}
	})

	t.Run("by email address", func(t *testing.T) {
		resp := &resource.ImportStateResponse{State: emptyState(t, s)}
		(&organizationMemberResource{client: c}).ImportState(ctx,
			resource.ImportStateRequest{ID: "Engineer@Acme.com"}, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("an address should resolve, case-insensitively: %v", resp.Diagnostics.Errors())
		}
		var id types.Int64
		resp.State.GetAttribute(ctx, path.Root("id"), &id)
		if id.ValueInt64() != 7 {
			t.Errorf("expected the address to resolve to membership 7, got %d", id.ValueInt64())
		}
	})

	t.Run("neither", func(t *testing.T) {
		resp := &resource.ImportStateResponse{State: emptyState(t, s)}
		(&organizationMemberResource{client: c}).ImportState(ctx,
			resource.ImportStateRequest{ID: "nobody@acme.com"}, resp)

		if !resp.Diagnostics.HasError() {
			t.Fatal("expected an unknown address to be refused")
		}
	})
}

// --- fivenines_organization_invitation ---

// An invitation leaves the pending list the moment it is taken up. Read has to
// tell that apart from a revocation, because re-inviting a member is a 422 and
// leaving a revoked invite in state means it is never re-sent.
func TestOrganizationInvitationRead_DistinguishesAcceptedFromRevoked(t *testing.T) {
	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	seed := func() tfsdk.State {
		return seededState(t, s, &organizationInvitationModel{
			ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
			Role: types.StringValue("member"), Status: types.StringValue("pending"),
		})
	}

	t.Run("accepted: the address is now on the roster", func(t *testing.T) {
		c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/members") {
				json.NewEncoder(w).Encode(membersEnvelope(map[string]interface{}{
					// Case differs deliberately: the roster is authoritative,
					// and the match has to be case-insensitive.
					"id": 8, "user_id": 92, "email": "NewHire@acme.com", "role": "member",
				}))
				return
			}
			json.NewEncoder(w).Encode(invitationsEnvelope())
		})

		state := seed()
		resp := &resource.ReadResponse{State: state}
		(&organizationInvitationResource{client: c}).Read(ctx, resource.ReadRequest{State: state}, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
		}
		if resp.State.Raw.IsNull() {
			t.Fatal("an accepted invitation did its job — it must not be dropped and re-sent")
		}
		var got organizationInvitationModel
		resp.State.Get(ctx, &got)
		if got.Status.ValueString() != statusAccepted {
			t.Errorf("expected status %q, got %q", statusAccepted, got.Status.ValueString())
		}
	})

	t.Run("revoked: gone from both", func(t *testing.T) {
		c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/members") {
				json.NewEncoder(w).Encode(membersEnvelope())
				return
			}
			json.NewEncoder(w).Encode(invitationsEnvelope())
		})

		state := seed()
		resp := &resource.ReadResponse{State: state}
		(&organizationInvitationResource{client: c}).Read(ctx, resource.ReadRequest{State: state}, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("a revoked invitation is drift, not an error: %v", resp.Diagnostics.Errors())
		}
		if !resp.State.Raw.IsNull() {
			t.Error("expected a revoked invitation to be dropped so the next apply re-sends it")
		}
	})

	t.Run("roster unreadable: neither answer is guessed", func(t *testing.T) {
		c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/members") {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{"error": "Insufficient role"})
				return
			}
			json.NewEncoder(w).Encode(invitationsEnvelope())
		})

		state := seed()
		resp := &resource.ReadResponse{State: state}
		(&organizationInvitationResource{client: c}).Read(ctx, resource.ReadRequest{State: state}, resp)

		if !resp.Diagnostics.HasError() {
			t.Fatal("expected the ambiguity to surface rather than be guessed")
		}
	})
}

// Changing the role re-issues the invitation: the create endpoint is an upsert,
// so this refreshes the row rather than colliding with it.
func TestOrganizationInvitationUpdate_ReissuesWithTheNewRole(t *testing.T) {
	var postedRole string
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Invitation struct {
				Role string `json:"role"`
			} `json:"invitation"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		postedRole = body.Invitation.Role
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"invitation": map[string]interface{}{
			"id": 12, "email": "newhire@acme.com", "role": "admin", "status": "pending",
		}})
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	plan := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"), Role: types.StringValue("admin"),
	})
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue("pending"),
	})

	resp := &resource.UpdateResponse{State: state}
	(&organizationInvitationResource{client: c}).Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if postedRole != "admin" {
		t.Errorf("expected the new role to be re-issued, got %q", postedRole)
	}
}

// Once accepted, the role lives on the membership. Saying so beats relaying the
// API's "address is already a member" 422.
func TestOrganizationInvitationUpdate_RefusesOnceAccepted(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an accepted invitation must be refused before any request")
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	plan := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"), Role: types.StringValue("admin"),
	})
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue(statusAccepted),
	})

	resp := &resource.UpdateResponse{State: state}
	(&organizationInvitationResource{client: c}).Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: state,
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected changing the role of an accepted invitation to be refused")
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "fivenines_organization_member") {
		t.Errorf("expected the diagnostic to name the member resource, got %q", detail)
	}
}

// Destroying an accepted invitation still ATTEMPTS the revoke. If it really was
// accepted the row is gone and the API answers 404, which is tolerated; and if
// the acceptance verdict was ever wrong — the address is on the roster for an
// unrelated reason — this is the call that retires the live acceptance link and
// frees the seat. Skipping it on a guess is the only version that can leave one
// live while Terraform reports the invitation destroyed.
func TestOrganizationInvitationDelete_AcceptedStillAttemptsTheRevoke(t *testing.T) {
	var deletes int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not found", "request_id": "req-404"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Status: types.StringValue(statusAccepted),
	})

	resp := &resource.DeleteResponse{State: state}
	(&organizationInvitationResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("the 404 from an already-consumed invitation must be tolerated: %v", resp.Diagnostics.Errors())
	}
	if deletes != 1 {
		t.Errorf("expected the revoke to be attempted exactly once, got %d", deletes)
	}
}

func TestOrganizationInvitationDelete_ToleratesAnAlreadyRevokedInvitation(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not found", "request_id": "req-404"})
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Status: types.StringValue("pending"),
	})

	resp := &resource.DeleteResponse{State: state}
	(&organizationInvitationResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("revoking an invitation already gone must succeed: %v", resp.Diagnostics.Errors())
	}
}

func TestOrganizationInvitationDelete_SurfacesRealErrors(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "boom", "request_id": "req-500"})
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Status: types.StringValue("pending"),
	})

	resp := &resource.DeleteResponse{State: state}
	(&organizationInvitationResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a 500 on revoke must surface, not be swallowed as already-revoked")
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "req-500") {
		t.Errorf("expected the request_id in the diagnostic, got %q", detail)
	}
}

func TestOrganizationInvitationImportState(t *testing.T) {
	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)

	t.Run("by id", func(t *testing.T) {
		resp := &resource.ImportStateResponse{State: emptyState(t, s)}
		(&organizationInvitationResource{}).ImportState(ctx,
			resource.ImportStateRequest{ID: "12"}, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
		}
		var id types.Int64
		resp.State.GetAttribute(ctx, path.Root("id"), &id)
		if id.ValueInt64() != 12 {
			t.Errorf("expected id 12, got %d", id.ValueInt64())
		}
	})

	t.Run("unparseable", func(t *testing.T) {
		resp := &resource.ImportStateResponse{State: emptyState(t, s)}
		(&organizationInvitationResource{}).ImportState(ctx,
			resource.ImportStateRequest{ID: "newhire@acme.com"}, resp)

		if !resp.Diagnostics.HasError() {
			t.Fatal("expected a non-numeric invitation id to be refused")
		}
	})
}

// --- ModifyPlan: the X-Dry-Run pre-flight ---

// modifyMemberPlan drives ModifyPlan with the given plan and state, both
// already seeded, and returns the diagnostics it produced.
func modifyMemberPlan(t *testing.T, c *client.Client, plan, state tfsdk.State) *resource.ModifyPlanResponse {
	t.Helper()
	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}}
	(&organizationMemberResource{client: c}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:   tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw},
		State:  state,
		Config: tfsdk.Config{Schema: plan.Schema, Raw: plan.Raw},
	}, resp)
	return resp
}

// Pointing the resource at a different address destroys this membership before
// adopting the new one — so it is the REMOVAL that needs pre-validating, not
// the role change that happens to come with it.
func TestOrganizationMemberModifyPlan_ReplacementDryRunsTheRemoval(t *testing.T) {
	var dryRunDeletes int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.Header.Get("X-Dry-Run") == "true" {
			dryRunDeletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPatch {
			t.Error("a replacement destroys the old membership — the role change is not what to validate")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("someone.else@acme.com"), Role: types.StringValue("admin"),
	})
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := modifyMemberPlan(t, c, plan, state)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if dryRunDeletes != 1 {
		t.Errorf("expected exactly one dry-run removal, got %d", dryRunDeletes)
	}
}

// Re-casing is the same person: nothing to pre-validate, and nothing to send.
func TestOrganizationMemberModifyPlan_RecasingSendsNothing(t *testing.T) {
	var requests int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	})

	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("Engineer@Acme.com"), Role: types.StringValue("member"),
	})

	resp := modifyMemberPlan(t, c, plan, state)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if requests != 0 {
		t.Errorf("re-casing an address changes nothing — expected no requests, got %d", requests)
	}
}

// The membership is already gone. The apply's Delete tolerates that, so the
// plan must not fail on it either.
func TestOrganizationMemberModifyPlan_AlreadyGoneIsNotAPlanError(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not found", "request_id": "req-404"})
	})

	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{}
	(&organizationMemberResource{client: c}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		// A null plan is a destroy.
		Plan:  tfsdk.Plan{Schema: s},
		State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a membership already gone must not fail the plan: %v", resp.Diagnostics.Errors())
	}
	if resp.Diagnostics.WarningsCount() != 0 {
		t.Errorf("expected silence, got %v", resp.Diagnostics.Warnings())
	}
}

// A pre-flight that cannot reach the API says so and steps aside. Failing the
// plan on a network blip would make the guard worse than no guard.
func TestOrganizationMemberModifyPlan_TransportFailureWarnsButDoesNotBlock(t *testing.T) {
	// A server that is closed before use: every request fails at the transport
	// layer, which is not an *APIError.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	c := client.NewClient(srv.URL, "test-key")

	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{}
	(&organizationMemberResource{client: c}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: s},
		State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("an unreachable API must not fail the plan: %v", resp.Diagnostics.Errors())
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Fatal("expected a warning that the pre-flight could not run")
	}
	if summary := resp.Diagnostics.Warnings()[0].Summary(); !strings.Contains(summary, "pre-validate") {
		t.Errorf("expected the warning to name the pre-flight, got %q", summary)
	}
}

// The escape hatch: with the pre-flight off, no dry-run request is made at all.
func TestOrganizationMemberModifyPlan_SkipPlanValidationSendsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("skip_plan_validation is set — no pre-flight request may be sent")
	}))
	t.Cleanup(srv.Close)
	c := client.NewClient(srv.URL, "test-key")
	c.SkipPlanValidation = true

	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{}
	(&organizationMemberResource{client: c}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: s},
		State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
}

// The destroy branch is the one that matters most and was the one nothing
// guarded: dropping WithDryRun here makes `terraform plan -destroy` issue a REAL
// DELETE, offboarding the person and destroying their API tokens before anyone
// has seen the plan. Five reviewers were fooled by exactly that mutation, so the
// header is now asserted on every pre-flight path, not just the replacement one.
func TestOrganizationMemberModifyPlan_DestroyDryRunsTheRemoval(t *testing.T) {
	var dryRunDeletes, liveDeletes int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if r.Header.Get("X-Dry-Run") == "true" {
				dryRunDeletes++
			} else {
				liveDeletes++
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})

	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{}
	(&organizationMemberResource{client: c}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: s}, // a null plan is a destroy
		State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if liveDeletes != 0 {
		t.Fatalf("planning a destroy issued %d LIVE DELETE(s) — that offboards the person during plan", liveDeletes)
	}
	if dryRunDeletes != 1 {
		t.Errorf("expected exactly one dry-run removal, got %d", dryRunDeletes)
	}
}

// And the refusal that the destroy pre-flight exists to surface: removing
// yourself or the owner has to fail the PLAN, not the apply.
func TestOrganizationMemberModifyPlan_DestroyRefusalFailsThePlan(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.Header.Get("X-Dry-Run") != "true" {
			t.Error("a plan-time removal must carry X-Dry-Run")
		}
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "You cannot remove an owner", "code": "forbidden", "request_id": "req-403",
		})
	})

	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(1), Email: types.StringValue("founder@acme.com"), Role: types.StringValue("admin"),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{}
	(&organizationMemberResource{client: c}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: s},
		State: state,
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused removal must fail the plan, not wait for the apply")
	}
	summary := resp.Diagnostics.Errors()[0].Summary()
	if !strings.Contains(summary, "removal") {
		t.Errorf("expected the diagnostic to name the removal, got %q", summary)
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(detail, "req-403") {
		t.Errorf("expected the request_id in the diagnostic, got %q", detail)
	}
	// The escape hatch must not be offered as a way past a refusal it cannot fix.
	if !strings.Contains(detail, "the apply would be refused too") {
		t.Errorf("expected the diagnostic to say skipping the pre-flight will not help, got %q", detail)
	}
}

// The role-change pre-flight carries the header too.
func TestOrganizationMemberModifyPlan_RoleChangeDryRunsThePatch(t *testing.T) {
	var dryRunPatches, livePatches int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			if r.Header.Get("X-Dry-Run") == "true" {
				dryRunPatches++
			} else {
				livePatches++
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"member": map[string]interface{}{
			"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "admin",
		}})
	})

	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("admin"),
	})
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := modifyMemberPlan(t, c, plan, state)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if livePatches != 0 {
		t.Fatalf("planning a role change issued %d LIVE PATCH(es)", livePatches)
	}
	if dryRunPatches != 1 {
		t.Errorf("expected exactly one dry-run role change, got %d", dryRunPatches)
	}
}

// An invitation recorded as accepted is terminal: it was consumed when somebody
// joined, and refresh must not re-derive that verdict. Re-deriving it costs two
// list walks AND — the reason this matters — makes a later roster miss look like
// a revocation. See the regression below.
func TestOrganizationInvitationRead_AcceptedIsTerminal(t *testing.T) {
	var calls int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(invitationsEnvelope())
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue(statusAccepted),
	})

	resp := &resource.ReadResponse{State: state}
	(&organizationInvitationResource{client: c}).Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if calls != 0 {
		t.Errorf("an accepted invitation is terminal — expected no API calls, got %d", calls)
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("an accepted invitation must stay in state")
	}
	var got organizationInvitationModel
	resp.State.Get(ctx, &got)
	if got.Status.ValueString() != statusAccepted {
		t.Errorf("expected the accepted status to survive, got %q", got.Status.ValueString())
	}
}

// REGRESSION: offboarding a member must not make Terraform re-invite them.
//
// The provider's own example declares an invitation AND a member for the same
// address. Offboarding deletes the person's account; if refresh then re-derived
// the invitation's verdict from the roster, the miss would read as "revoked",
// the resource would leave state, and the next apply would POST a fresh
// invitation — mailing a live 7-day acceptance link to the person the previous
// apply just removed. Under -auto-approve nobody sees the "1 to add".
func TestOrganizationInvitationRead_OffboardedMemberIsNotReInvited(t *testing.T) {
	var posts int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		// The person is gone: absent from the invitation list AND the roster.
		if strings.HasSuffix(r.URL.Path, "/members") {
			json.NewEncoder(w).Encode(membersEnvelope())
			return
		}
		json.NewEncoder(w).Encode(invitationsEnvelope())
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("departed@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue(statusAccepted),
	})

	resp := &resource.ReadResponse{State: state}
	(&organizationInvitationResource{client: c}).Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("dropping a spent invitation because the person was offboarded makes the next apply re-invite them")
	}
	if posts != 0 {
		t.Errorf("no invitation may be re-sent during a refresh, got %d POSTs", posts)
	}
}

// A PENDING invitation that vanished from both lists really was revoked, and
// must be dropped so the next apply re-sends it. This is the branch the
// regression above must not break.
func TestOrganizationInvitationRead_PendingRevokedIsStillDropped(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/members") {
			json.NewEncoder(w).Encode(membersEnvelope())
			return
		}
		json.NewEncoder(w).Encode(invitationsEnvelope())
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue("pending"),
	})

	resp := &resource.ReadResponse{State: state}
	(&organizationInvitationResource{client: c}).Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a revoked invitation is drift, not an error: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Error("a revoked PENDING invitation must be dropped so the next apply re-sends it")
	}
}

// organizationSchemaForTest lives in notfound_removal_test.go, with the drift
// table that uses it. These two are only ever used here.

func organizationMemberSchemaForTest(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationMemberResource().(*organizationMemberResource).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func organizationInvitationSchemaForTest(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationInvitationResource().(*organizationInvitationResource).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// --- adversarial-review regressions ---

// Repointing the resource at a different address destroys the incumbent BEFORE
// adopting the successor. If the successor has not accepted yet — the normal
// state of affairs — the apply would delete somebody's account and every token
// they owned, and then fail having achieved nothing. Plan must refuse first.
func TestOrganizationMemberModifyPlan_ReplacementRequiresAnAdoptableSuccessor(t *testing.T) {
	var dryRunDeletes int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			dryRunDeletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// The successor has not joined.
		json.NewEncoder(w).Encode(membersEnvelope(map[string]interface{}{
			"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "member",
		}))
	})

	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("newhire@acme.com"), Role: types.StringValue("member"),
	})
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := modifyMemberPlan(t, c, plan, state)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the plan to refuse a replacement whose successor cannot be adopted")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(detail, "engineer@acme.com") || !strings.Contains(detail, "newhire@acme.com") {
		t.Errorf("expected the diagnostic to name both the incumbent and the successor, got %q", detail)
	}
	if dryRunDeletes != 1 {
		t.Errorf("the removal half should still have been dry-run once, got %d", dryRunDeletes)
	}
}

// The same replacement, with a successor who HAS joined, must plan cleanly.
func TestOrganizationMemberModifyPlan_ReplacementAllowedWhenSuccessorHasJoined(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(membersEnvelope(
			map[string]interface{}{"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "member"},
			map[string]interface{}{"id": 8, "user_id": 92, "email": "newhire@acme.com", "role": "member"},
		))
	})

	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("newhire@acme.com"), Role: types.StringValue("member"),
	})
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := modifyMemberPlan(t, c, plan, state)
	if resp.Diagnostics.HasError() {
		t.Fatalf("an adoptable successor must plan cleanly: %v", resp.Diagnostics.Errors())
	}
}

// A 5xx is the API being unwell, not a policy answer. Rendering it as a refusal
// would point the operator at skip_plan_validation as the remedy for an outage.
func TestOrganizationMemberModifyPlan_ServerErrorWarnsRatherThanRefuses(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "upstream unavailable", "request_id": "req-502"})
	})

	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{}
	(&organizationMemberResource{client: c}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: s},
		State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a 502 must not read as a policy refusal: %v", resp.Diagnostics.Errors())
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Fatal("expected a warning that the pre-flight could not run")
	}
	if d := resp.Diagnostics.Warnings()[0].Detail(); strings.Contains(d, "skip_plan_validation") {
		t.Errorf("a transient server error must not recommend disabling the interlock, got %q", d)
	}
}

// The membership id is not durable. A 404 on offboarding means "no membership
// with THAT id" — if the person is still on the roster under a newer one, they
// still have access and Terraform must not report the offboarding done.
func TestOrganizationMemberDelete_RefusesToClaimSuccessWhileTheMemberRemains(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not found", "request_id": "req-404"})
			return
		}
		// Same person, rejoined under a new membership id.
		json.NewEncoder(w).Encode(membersEnvelope(map[string]interface{}{
			"id": 12, "user_id": 91, "email": "engineer@acme.com", "role": "member",
		}))
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := &resource.DeleteResponse{State: state}
	(&organizationMemberResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("reporting a completed offboarding while the person still has access is the failure to prevent")
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "12") {
		t.Errorf("expected the diagnostic to name the membership they actually hold, got %q", detail)
	}
}

// The genuine already-gone case still has to succeed, or a destroy strands the
// resource in state with no way out but `terraform state rm`.
func TestOrganizationMemberDelete_ToleratesAGenuinelyDepartedMember(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not found", "request_id": "req-404"})
			return
		}
		json.NewEncoder(w).Encode(membersEnvelope())
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := &resource.DeleteResponse{State: state}
	(&organizationMemberResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a member who really is gone must destroy cleanly: %v", resp.Diagnostics.Errors())
	}
}

// Changing the role of an accepted invitation has to fail at PLAN time. The same
// refusal in Update lands mid-apply and is not self-clearing: every later plan
// fails identically until somebody hand-edits the configuration.
func TestOrganizationInvitationModifyPlan_AcceptedRoleChangeFailsThePlan(t *testing.T) {
	s := organizationInvitationSchemaForTest(t)
	plan := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"), Role: types.StringValue("admin"),
	})
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue(statusAccepted),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}}
	(&organizationInvitationResource{}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw},
		State: state,
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the plan to refuse a role change on a spent invitation")
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "fivenines_organization_member") {
		t.Errorf("expected the diagnostic to name the member resource, got %q", detail)
	}
}

// A pending invitation's role change must still plan cleanly.
func TestOrganizationInvitationModifyPlan_PendingRoleChangeIsAllowed(t *testing.T) {
	s := organizationInvitationSchemaForTest(t)
	plan := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"), Role: types.StringValue("admin"),
	})
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue("pending"),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}}
	(&organizationInvitationResource{}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw},
		State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a pending invitation may still be re-issued with a new role: %v", resp.Diagnostics.Errors())
	}
}

// --- Codex adversarial regressions ---

// Acceptance and revocation both end with the invitation row gone, so both
// answer 404. Reporting "revoked" when the person actually got IN would tell an
// operator access was withdrawn at the moment it was granted.
func TestOrganizationInvitationDelete_WarnsWhenAcceptanceWonTheRace(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Not found", "request_id": "req-404"})
			return
		}
		json.NewEncoder(w).Encode(membersEnvelope(map[string]interface{}{
			"id": 8, "user_id": 92, "email": "newhire@acme.com", "role": "admin",
		}))
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("admin"), Status: types.StringValue("pending"),
	})

	resp := &resource.DeleteResponse{State: state}
	(&organizationInvitationResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	// A warning, not an error: the destroy must complete or the resource is
	// stranded in state with no way out but `terraform state rm`.
	if resp.Diagnostics.HasError() {
		t.Fatalf("the destroy must still complete: %v", resp.Diagnostics.Errors())
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Fatal("expected a warning that the invitation was accepted rather than revoked")
	}
	if d := resp.Diagnostics.Warnings()[0].Detail(); !strings.Contains(d, "fivenines_organization_member") {
		t.Errorf("expected the warning to say how to actually withdraw access, got %q", d)
	}
}

// A genuinely revoked invitation is silent — the warning above must not fire on
// the ordinary path.
func TestOrganizationInvitationDelete_RevokedIsSilent(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(membersEnvelope())
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue("pending"),
	})

	resp := &resource.DeleteResponse{State: state}
	(&organizationInvitationResource{client: c}).Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 0 {
		t.Fatalf("an ordinary revoke must be silent: %v %v", resp.Diagnostics.Errors(), resp.Diagnostics.Warnings())
	}
}

// Re-casing the address reaches Update with the role unchanged. Re-issuing there
// would send a second email and invalidate the acceptance link the invitee is
// holding — a live side effect from a cosmetic edit.
func TestOrganizationInvitationUpdate_RecasingDoesNotResend(t *testing.T) {
	var posts int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"invitation": map[string]interface{}{
			"id": 12, "email": "newhire@acme.com", "role": "member", "status": "pending",
		}})
	})

	ctx := context.Background()
	s := organizationInvitationSchemaForTest(t)
	plan := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("NewHire@Acme.com"), Role: types.StringValue("member"),
	})
	state := seededState(t, s, &organizationInvitationModel{
		ID: types.Int64Value(12), Email: types.StringValue("newhire@acme.com"),
		Role: types.StringValue("member"), Status: types.StringValue("pending"),
	})

	resp := &resource.UpdateResponse{State: state}
	(&organizationInvitationResource{client: c}).Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if posts != 0 {
		t.Errorf("re-casing an address sent %d invitation email(s) and invalidated the live link", posts)
	}
}

// The same rule on the member side: nothing to write when the role is unchanged.
func TestOrganizationMemberUpdate_RecasingDoesNotPatch(t *testing.T) {
	var patches int
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches++
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"member": map[string]interface{}{
			"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": "member",
		}})
	})

	ctx := context.Background()
	s := organizationMemberSchemaForTest(t)
	plan := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("Engineer@Acme.com"), Role: types.StringValue("member"),
	})
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	resp := &resource.UpdateResponse{State: state}
	(&organizationMemberResource{client: c}).Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if patches != 0 {
		t.Errorf("expected no PATCH for a case-only edit, got %d", patches)
	}
}

// A 429 survives the client's own retry budget and would otherwise read as a
// policy refusal — congestion is not a verdict, and a big plan can rate-limit
// itself into "you are not allowed to do this".
func TestOrganizationMemberModifyPlan_RateLimitWarnsRatherThanRefuses(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "rate limited", "request_id": "req-429"})
	})

	s := organizationMemberSchemaForTest(t)
	state := seededState(t, s, &organizationMemberModel{
		ID: types.Int64Value(7), Email: types.StringValue("engineer@acme.com"), Role: types.StringValue("member"),
	})

	ctx := context.Background()
	resp := &resource.ModifyPlanResponse{}
	(&organizationMemberResource{client: c}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: s},
		State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a 429 must not read as a policy refusal: %v", resp.Diagnostics.Errors())
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Error("expected a warning that the pre-flight could not run")
	}
}
