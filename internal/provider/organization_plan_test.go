package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Plan tests for the organization surface. They drive REAL Terraform against a
// fake API, which is the only place the provider's plan is compared against the
// configuration — the unit tests drive Create and Update directly and cannot
// see plan validation at all.

func orgJSON() map[string]interface{} {
	return map[string]interface{}{
		"id": 42, "name": "Acme Corp", "slug": "acme-corp", "display_name": "Acme Corp",
		"plan": "pro", "trialing": false, "seats_used": 4, "seats_total": 10,
		"seats_remaining": 6, "members_count": 3, "pending_invitations_count": 1,
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
	}
}

func memberJSON(role string) map[string]interface{} {
	return map[string]interface{}{
		"id": 7, "user_id": 91, "email": "engineer@acme.com", "role": role,
		"two_factor_enabled": true,
		"joined_at":          "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
	}
}

func membersPage(role string) map[string]interface{} {
	return map[string]interface{}{
		"members": []map[string]interface{}{memberJSON(role)},
		"meta":    map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
	}
}

// The singleton has no create endpoint, so an apply is a PATCH onto whatever
// organization the key already resolves to.
func TestOrganizationPlan_SingletonAdoptsAndRenames(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"org"`)
		json.NewEncoder(w).Encode(map[string]interface{}{"organization": orgJSON()})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_organization" "this" {
  name = "Acme Corp"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("fivenines_organization.this", "name", "Acme Corp"),
				resource.TestCheckResourceAttr("fivenines_organization.this", "slug", "acme-corp"),
				resource.TestCheckResourceAttr("fivenines_organization.this", "seats_remaining", "6"),
			),
		}},
	})
}

// Create adopts an existing membership rather than creating one, because the
// API has no endpoint that creates a member.
func TestOrganizationMemberPlan_AdoptsExistingMembership(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(membersPage("member"))
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_organization_member" "engineer" {
  email = "engineer@acme.com"
  role  = "member"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("fivenines_organization_member.engineer", "id", "7"),
				resource.TestCheckResourceAttr("fivenines_organization_member.engineer", "user_id", "91"),
				resource.TestCheckResourceAttr("fivenines_organization_member.engineer", "two_factor_enabled", "true"),
			),
		}},
	})
}

// Re-casing the configured address is not a different person, and must not plan
// the replacement whose destroy half deletes their account.
func TestOrganizationMemberPlan_RecasingDoesNotReplace(t *testing.T) {
	// The test case's own teardown destroys the resource, so the assertion is
	// that nothing was deleted BEFORE that — checked inside step 2.
	var deletes int
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPatch {
			json.NewEncoder(w).Encode(map[string]interface{}{"member": memberJSON("member")})
			return
		}
		json.NewEncoder(w).Encode(membersPage("member"))
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_organization_member" "engineer" {
  email = "Engineer@Acme.com"
  role  = "member"
}`,
			},
			{
				Config: providerConfig + `
resource "fivenines_organization_member" "engineer" {
  email = "engineer@acme.com"
  role  = "member"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Same membership id across the re-casing: a replacement
					// would have destroyed and re-adopted it.
					resource.TestCheckResourceAttr("fivenines_organization_member.engineer", "id", "7"),
					func(*terraform.State) error {
						if deletes != 0 {
							return fmt.Errorf("re-casing an address issued %d DELETE(s) — that offboards the person and destroys their API tokens", deletes)
						}
						return nil
					},
				),
			},
		},
	})
}

// The headline guard: a role change the API would refuse is caught by the
// X-Dry-Run pre-flight during plan, before any apply starts.
func TestOrganizationMemberPlan_DryRunRefusalFailsThePlan(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			if r.Header.Get("X-Dry-Run") == "true" {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "You cannot change your own role", "code": "forbidden",
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"member": memberJSON("admin")})
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(membersPage("member"))
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_organization_member" "engineer" {
  email = "engineer@acme.com"
  role  = "member"
}`,
			},
			{
				Config: providerConfig + `
resource "fivenines_organization_member" "engineer" {
  email = "engineer@acme.com"
  role  = "admin"
}`,
				ExpectError: regexp.MustCompile(`role change would be refused`),
			},
		},
	})
}

// skip_plan_validation turns the pre-flight off, for the setup where the key
// that plans is not the key that applies. The same refusal must then reach the
// apply instead of failing the plan.
func TestOrganizationMemberPlan_SkipPlanValidationSuppressesTheDryRun(t *testing.T) {
	// Stateful, like the real API: once the role is patched, the roster reads
	// back the new one. A stub frozen at "member" reports drift on every
	// refresh and hides whatever the provider actually did.
	role := "member"
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			if r.Header.Get("X-Dry-Run") == "true" {
				t.Error("skip_plan_validation is set — no dry-run request may be sent")
			}
			var body struct {
				Membership struct {
					Role string `json:"role"`
				} `json:"membership"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			role = body.Membership.Role
			json.NewEncoder(w).Encode(map[string]interface{}{"member": memberJSON(role)})
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(membersPage(role))
	})

	const skipConfig = `
provider "fivenines" {
  skip_plan_validation = true
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: skipConfig + `
resource "fivenines_organization_member" "engineer" {
  email = "engineer@acme.com"
  role  = "member"
}`,
			},
			{
				Config: skipConfig + `
resource "fivenines_organization_member" "engineer" {
  email = "engineer@acme.com"
  role  = "admin"
}`,
				Check: resource.TestCheckResourceAttr("fivenines_organization_member.engineer", "role", "admin"),
			},
		},
	})
}

// Applying the member resource for an address nobody has accepted yet has to
// name the invitation resource, not surface a bare 404.
func TestOrganizationMemberPlan_UnknownAddressPointsAtTheInvitation(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"members": []map[string]interface{}{},
			"meta":    map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_organization_member" "ghost" {
  email = "nobody@acme.com"
  role  = "member"
}`,
			ExpectError: regexp.MustCompile(`fivenines_organization_invitation`),
		}},
	})
}

func TestOrganizationInvitationPlan_CreateDefaultsToMember(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{"invitation": map[string]interface{}{
				"id": 12, "email": "newhire@acme.com", "role": "member", "status": "pending",
				"invited_by": "admin@acme.com", "expires_at": "2026-09-08T00:00:00Z",
				"accepted_at": nil, "created_at": "2026-09-01T00:00:00Z",
				"updated_at": "2026-09-01T00:00:00Z",
			}})
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"invitations": []map[string]interface{}{{
					"id": 12, "email": "newhire@acme.com", "role": "member", "status": "pending",
					"invited_by": "admin@acme.com", "expires_at": "2026-09-08T00:00:00Z",
					"accepted_at": nil, "created_at": "2026-09-01T00:00:00Z",
					"updated_at": "2026-09-01T00:00:00Z",
				}},
				"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
			})
		}
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_organization_invitation" "new_hire" {
  email = "newhire@acme.com"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("fivenines_organization_invitation.new_hire", "role", "member"),
				resource.TestCheckResourceAttr("fivenines_organization_invitation.new_hire", "status", "pending"),
				resource.TestCheckResourceAttr("fivenines_organization_invitation.new_hire", "id", "12"),
			),
		}},
	})
}

// An invalid role has to fail at plan time, not on the API's 422.
func TestOrganizationInvitationPlan_RejectsOwnerRole(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("plan-time validation must reject the role before any request")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_organization_invitation" "boss" {
  email = "boss@acme.com"
  role  = "owner"
}`,
			ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Match`),
		}},
	})
}

func TestOrganizationDataSourcesPlan_Read(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/organization/members":
			json.NewEncoder(w).Encode(membersPage("admin"))
		case "/api/v1/organization/security":
			json.NewEncoder(w).Encode(map[string]interface{}{"security": map[string]interface{}{
				"require_two_factor": true, "two_factor_enforced_at": "2026-08-01T00:00:00Z",
				"members_count": 8, "members_with_two_factor": 5,
				"members_pending_two_factor": 3, "sso_enforced": false,
			}})
		case "/api/v1/organization/saml":
			json.NewEncoder(w).Encode(map[string]interface{}{"saml": map[string]interface{}{
				"configured": true, "enabled": true, "enforce_sso": true,
				"idp_sso_url": "https://idp.acme.com/sso", "idp_certificate_present": true,
				"idp_certificate_expires_at": "2027-01-01T00:00:00Z",
				"session_duration_hours":     24, "domains": []string{"acme.com", "acme.io"},
			}})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{"organization": orgJSON()})
		}
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_organization" "this" {}
data "fivenines_organization_members" "all" {}
data "fivenines_organization_security" "this" {}
data "fivenines_organization_saml" "this" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_organization.this", "plan", "pro"),
				resource.TestCheckResourceAttr("data.fivenines_organization.this", "seats_remaining", "6"),
				resource.TestCheckResourceAttr("data.fivenines_organization_members.all", "members.#", "1"),
				resource.TestCheckResourceAttr("data.fivenines_organization_members.all", "members.0.role", "admin"),
				resource.TestCheckResourceAttr("data.fivenines_organization_security.this", "members_pending_two_factor", "3"),
				// The date surfaced nowhere else: when it passes, everyone is locked out.
				resource.TestCheckResourceAttr("data.fivenines_organization_saml.this", "idp_certificate_expires_at", "2027-01-01T00:00:00Z"),
				resource.TestCheckResourceAttr("data.fivenines_organization_saml.this", "domains.#", "2"),
			),
		}},
	})
}

// An organization that never configured SAML answers with every key null or
// false, so a sweep across tenants never branches on key presence.
func TestOrganizationSamlDataSourcePlan_NeverConfigured(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"saml": map[string]interface{}{
			"configured": false, "enabled": false, "enforce_sso": false,
			"idp_entity_id": nil, "idp_sso_url": nil, "idp_certificate_expires_at": nil,
			"session_duration_hours": nil, "domains": []string{}, "updated_at": nil,
		}})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_organization_saml" "this" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_organization_saml.this", "configured", "false"),
				resource.TestCheckNoResourceAttr("data.fivenines_organization_saml.this", "idp_sso_url"),
				resource.TestCheckResourceAttr("data.fivenines_organization_saml.this", "domains.#", "0"),
			),
		}},
	})
}

// An empty roster has to render as an empty list, not null: a `for` expression
// over a null list fails at plan time, so the "nobody yet" case would break
// every configuration that iterates the members.
func TestOrganizationMembersDataSourcePlan_EmptyRoster(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"members": []map[string]interface{}{},
			"meta":    map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_organization_members" "all" {}

# The iteration that a null list breaks.
output "emails" {
  value = [for m in data.fivenines_organization_members.all.members : m.email]
}`,
			Check: resource.TestCheckResourceAttr("data.fivenines_organization_members.all", "members.#", "0"),
		}},
	})
}
