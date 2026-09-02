package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Enrollment tokens carry the same write-once hazard as api_tokens — the value
// exists for one response and state is the only copy — plus one they do not
// share: destroy is a real DELETE, which the API refuses once the token has
// enrolled a host. The unit tests can drive both paths, but only a plan test can
// see what Terraform does with the result: whether a refresh keeps the value,
// and whether a revoked token drops out of state or sits there being reported as
// a working credential.

// enrollmentTokenHandler serves the four endpoints the resource uses — index,
// create, revoke and delete — with the server's own rules baked in:
//
//   - the value appears only in the create response; index and revoke render
//     metadata, exactly as the serializer's include_token flag does.
//   - DELETE refuses with 422 once hosts_registered_count > 0, and the message
//     points at revoke.
//   - revoke keeps the row and flips active to false.
//   - there is no show endpoint, matching the real API — Read walks the index.
func enrollmentTokenHandler() func(http.ResponseWriter, *http.Request) {
	var mu sync.Mutex
	type stored struct {
		id       int64
		name     string
		revoked  bool
		hosts    int64
		lastUsed interface{}
	}
	tokens := map[int64]*stored{}
	var nextID int64

	render := func(t *stored, raw string) map[string]interface{} {
		out := map[string]interface{}{
			"id": t.id, "name": t.name, "active": !t.revoked,
			"hosts_registered_count": t.hosts, "last_used_at": t.lastUsed,
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}
		if raw != "" {
			out["token"] = raw
			out["install_command"] = "wget -T 3 -q https://releases.fivenines.io/latest/fivenines_setup.sh && " +
				"sudo sh fivenines_setup.sh " + raw
		}
		return out
	}

	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/revoke"):
			t, ok := tokens[enrollmentPathID(strings.TrimSuffix(r.URL.Path, "/revoke"))]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
				return
			}
			t.revoked = true
			json.NewEncoder(w).Encode(map[string]interface{}{"enrollment_token": render(t, "")})

		case r.Method == http.MethodDelete:
			id := enrollmentPathID(r.URL.Path)
			t, ok := tokens[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
				return
			}
			// hosts.dependent: :nullify — deleting a used token would orphan the
			// hosts it enrolled, so the API refuses and points at revoke.
			if t.hosts > 0 {
				w.WriteHeader(http.StatusUnprocessableEntity)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "Cannot delete a token that has registered hosts. Revoke it instead.",
				})
				return
			}
			delete(tokens, id)
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost:
			var body struct {
				EnrollmentToken struct {
					Name string `json:"name"`
				} `json:"enrollment_token"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			nextID++
			t := &stored{id: nextID, name: body.EnrollmentToken.Name}
			tokens[t.id] = t
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_token": render(t, fmt.Sprintf("tok%dsecretvalue", t.id)),
			})

		default: // index
			list := []map[string]interface{}{}
			for _, t := range tokens {
				list = append(list, render(t, ""))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_tokens": list,
				"meta": map[string]int{
					"current_page": 1, "total_pages": 1,
					"total_count": len(list), "per_page": 100,
				},
			})
		}
	}
}

func enrollmentPathID(path string) int64 {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	id, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return id
}

// The whole point of the resource: mint a token, hand its value to cloud-init,
// and have the value still be there on the next run. A refresh re-reads the
// index, which never carries it, so the mapping has to leave the stored one
// alone — wiping it would empty `terraform output` without changing a plan.
func TestEnrollmentTokenPlan_ValueSurvivesRefresh(t *testing.T) {
	planTest(t, enrollmentTokenHandler())

	cfg := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "name", "web fleet"),
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "token", "tok1secretvalue"),
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "install_command",
						"wget -T 3 -q https://releases.fivenines.io/latest/fivenines_setup.sh && "+
							"sudo sh fivenines_setup.sh tok1secretvalue"),
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "hosts_registered_count", "0"),
					resource.TestCheckNoResourceAttr("fivenines_enrollment_token.test", "last_used_at"),
				),
			},
			{Config: cfg, PlanOnly: true},
			{
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "token", "tok1secretvalue"),
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "install_command",
						"wget -T 3 -q https://releases.fivenines.io/latest/fivenines_setup.sh && "+
							"sudo sh fivenines_setup.sh tok1secretvalue"),
				),
			},
			// And the refresh did not leave a diff behind either.
			{Config: cfg, PlanOnly: true},
		},
	})
}

// The rule this resource inherits from fivenines_api_token (#40): a token
// revoked outside Terraform enrolls nothing and cannot be un-revoked, so the
// refresh drops it and the next plan mints a replacement. Left in state it would
// be a dead credential Terraform reports as healthy — every new host silently
// failing to enroll while the plan says no changes.
//
// api_token has to exempt EXPIRY from this, because recreating an expired token
// re-sends a past expires_at and wedges every future plan. Enrollment tokens
// have no expiry: the API carries `active` and no `expires_at`, so there is no
// second way to be inactive and no exemption to make.
func TestEnrollmentTokenPlan_RevokedOutOfBandIsRecreated(t *testing.T) {
	var revokeNext atomic.Bool
	inner := enrollmentTokenHandler()
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && revokeNext.Load() {
			// Serve the row as the dashboard would after a revoke: still listed,
			// still carrying its host attribution, inactive.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_tokens": []map[string]interface{}{{
					"id": 1, "name": "web fleet", "active": false,
					"hosts_registered_count": 2, "last_used_at": "2026-01-02T00:00:00Z",
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				}},
				"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
			})
			return
		}
		inner(w, r)
	})

	cfg := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() { revokeNext.Store(true) },
				Config:    cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_enrollment_token.test", plancheck.ResourceActionCreate),
					},
				},
				// The refresh drops the revoked row, so the plan is a create.
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// A token still enrolling hosts is NOT dropped. This is the other half of the
// rule: `active` is the only signal, so a mapping that keyed state removal off
// anything else — a non-zero hosts_registered_count, say — would recycle a
// working fleet credential on every refresh.
func TestEnrollmentTokenPlan_ActiveUsedTokenIsNotRecreated(t *testing.T) {
	var used atomic.Bool
	inner := enrollmentTokenHandler()
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && used.Load() {
			// Hosts have enrolled since the last read: the counters move, the
			// token stays active.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_tokens": []map[string]interface{}{{
					"id": 1, "name": "web fleet", "active": true,
					"hosts_registered_count": 12, "last_used_at": "2026-03-01T09:00:00Z",
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
				}},
				"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
			})
			return
		}
		inner(w, r)
	})

	cfg := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig:    func() { used.Store(true) },
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "hosts_registered_count", "12"),
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "last_used_at", "2026-03-01T09:00:00Z"),
					// Still the same credential, value intact.
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "token", "tok1secretvalue"),
				),
			},
			// Enrollment activity is not drift: a fleet onboarding hosts all day
			// must not propose replacing the token they are enrolling through.
			{Config: cfg, PlanOnly: true},
		},
	})
}

// The API can create, revoke and delete a token but not edit one, so a rename
// has to replace it — and a replacement mints a new value, which is the whole
// reason `name` forces replacement rather than silently no-op'ing.
func TestEnrollmentTokenPlan_RenameReplaces(t *testing.T) {
	planTest(t, enrollmentTokenHandler())

	before := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet"
}`
	after := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet (eu)"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: before,
				Check:  resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "token", "tok1secretvalue"),
			},
			{
				Config: after,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_enrollment_token.test",
							plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "name", "web fleet (eu)"),
					// A fresh token, not the old value carried across.
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "token", "tok2secretvalue"),
				),
			},
			{Config: after, PlanOnly: true},
		},
	})
}

// Destroying a token that has enrolled hosts cannot delete it — the API refuses
// rather than orphan them. The destroy has to fall back to revoke rather than
// fail, because a destroy that stops halfway leaves a live enrollment credential
// in the fleet, which is the outcome destroying the resource exists to produce.
func TestEnrollmentTokenPlan_DestroyRevokesTokenWithHosts(t *testing.T) {
	var revoked atomic.Bool
	var sawDelete atomic.Bool
	inner := enrollmentTokenHandler()
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			// The real API's refusal: this token has registered hosts.
			sawDelete.Store(true)
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Cannot delete a token that has registered hosts. Revoke it instead.",
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/revoke"):
			revoked.Store(true)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_token": map[string]interface{}{
					"id": 1, "name": "web fleet", "active": false,
					"hosts_registered_count": 4, "last_used_at": "2026-02-01T00:00:00Z",
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-02-02T00:00:00Z",
				},
			})
		default:
			inner(w, r)
		}
	})

	cfg := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			// Destroy runs at the end of the case; the assertions below run after it.
		},
		CheckDestroy: func(*terraform.State) error {
			if !sawDelete.Load() {
				return fmt.Errorf("destroy never attempted a DELETE")
			}
			if !revoked.Load() {
				return fmt.Errorf("destroy did not fall back to revoke after the API refused the delete; " +
					"the token is still able to enroll hosts")
			}
			return nil
		},
	})
}

// Import brings the metadata under management. The value is the one thing it
// cannot bring: it existed for one response and the API has no endpoint that
// returns it, so `token` and `install_command` are null on an imported token and
// stay null across a refresh.
func TestEnrollmentTokenPlan_ImportHasNoValue(t *testing.T) {
	planTest(t, enrollmentTokenHandler())

	// Two tokens, and the one under management is the second: an import that
	// ignored the id it was handed would silently adopt the wrong credential.
	cfg := providerConfig + `
resource "fivenines_enrollment_token" "other" {
  name = "unrelated"
}

resource "fivenines_enrollment_token" "test" {
  name       = "web fleet"
  depends_on = [fivenines_enrollment_token.other]
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "id", "2"),
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "token", "tok2secretvalue"),
				),
			},
			{
				ResourceName:      "fivenines_enrollment_token.test",
				ImportState:       true,
				ImportStateVerify: false, // token/install_command cannot round-trip
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported state, got %d", len(states))
					}
					s := states[0]
					if s.Attributes["name"] != "web fleet" {
						return fmt.Errorf("imported the wrong token: name = %q", s.Attributes["name"])
					}
					if v, ok := s.Attributes["token"]; ok && v != "" {
						return fmt.Errorf("import must not invent a token value, got %q", v)
					}
					if v, ok := s.Attributes["install_command"]; ok && v != "" {
						return fmt.Errorf("import must not invent an install_command, got %q", v)
					}
					return nil
				},
			},
		},
	})
}

// An enrollment token id is numeric, and a non-numeric import ID has to say so.
// Parsing it loosely would import id 0 — a token that does not exist — and the
// next refresh would drop it from state, reporting a successful import of
// nothing.
func TestEnrollmentTokenPlan_ImportRejectsNonNumericID(t *testing.T) {
	planTest(t, enrollmentTokenHandler())

	cfg := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:  "fivenines_enrollment_token.test",
				ImportState:   true,
				ImportStateId: "web-fleet",
				ExpectError:   regexp.MustCompile(`Invalid ID`),
			},
		},
	})
}

// The API validates the name's presence and answers 422, so this is UX: fail in
// the plan, before an apply reaches the network, rather than half way through
// one.
func TestEnrollmentTokenPlan_EmptyNameIsRejectedAtPlanTime(t *testing.T) {
	planTest(t, enrollmentTokenHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = ""
}`,
				ExpectError: regexp.MustCompile(`Invalid Attribute Value Length|string length must be at least 1`),
			},
		},
	})
}

// A token deleted outside Terraform between the refresh and the destroy — or a
// destroy run with -refresh=false — reaches DELETE on a token that is already
// gone. That is the goal state, so the 404 has to be success: failing here would
// wedge the destroy and leave the practitioner to `terraform state rm` a
// resource that no longer exists anywhere.
func TestEnrollmentTokenPlan_DestroyToleratesAlreadyDeletedToken(t *testing.T) {
	var vanish atomic.Bool
	inner := enrollmentTokenHandler()
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		// The index keeps listing it, so the refresh finds it and Terraform goes
		// on to call DELETE — which is the race this covers.
		if r.Method == http.MethodDelete && vanish.Load() {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
			return
		}
		inner(w, r)
	})

	cfg := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:    cfg,
				PreConfig: func() { vanish.Store(true) },
			},
			// The implicit destroy at the end of the case hits the 404 and must
			// still succeed.
		},
	})
}

// install_command is rendered alongside the value and nowhere else, so it is
// Computed and unknown until the create response resolves it. A response that
// carries the value without the command has to leave it NULL, not unknown: an
// unknown surviving an apply is a hard framework error, and Terraform discards
// the state it was about to write — leaking a live enrollment token the provider
// created and can never read back.
//
// The token value itself is guarded in the client, so this is the reachable half.
func TestEnrollmentTokenPlan_CreateWithoutInstallCommandStillApplies(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			// The value, and no install_command.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_token": map[string]interface{}{
					"id": 1, "name": "web fleet", "active": true,
					"hosts_registered_count": 0, "last_used_at": nil,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
					"token": "tok1secretvalue",
				},
			})
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enrollment_tokens": []map[string]interface{}{{
					"id": 1, "name": "web fleet", "active": true,
					"hosts_registered_count": 0, "last_used_at": nil,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
				}},
				"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
			})
		}
	})

	cfg := providerConfig + `
resource "fivenines_enrollment_token" "test" {
  name = "web fleet"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_enrollment_token.test", "token", "tok1secretvalue"),
					resource.TestCheckNoResourceAttr("fivenines_enrollment_token.test", "install_command"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}
