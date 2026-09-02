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

// For api_tokens the hazard plan tests exist to catch is that two attributes are
// re-rendered by the server before they come back. `scopes` gains the implicit
// "read" floor, so a config of ["write"] is stored as ["read", "write"];
// `expires_at` is re-rendered as ISO 8601, so "2026-12-01" comes back as
// "2026-12-01T00:00:00Z". Both attributes force replacement, so getting either
// wrong does not merely drift — every plan proposes destroying a live credential
// to mint an identical one, and no unit test can see it:
//
//	Error: Provider produced inconsistent result after apply
//	.scopes: was cty.SetVal([cty.StringVal("write")]), but now
//	cty.SetVal([cty.StringVal("read"), cty.StringVal("write")])
func apiTokenPlanTest(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	planTest(t, respond)
}

// apiTokenHandler serves the three endpoints the resource uses — index, create
// and revoke — with the server's own normalisation baked in:
//
//   - "read" is folded into scopes in `requested | ["read"]` order, so a write
//     token comes back as ["write", "read"] and not sorted.
//   - expires_at is re-rendered as UTC ISO 8601, so a bare date gains a time.
//   - the value appears only in the create response.
//   - DELETE revokes: the row survives with revoked_at set and active false.
//
// There is no show endpoint, matching the real API — Read walks the index.
func apiTokenHandler() func(http.ResponseWriter, *http.Request) {
	var mu sync.Mutex
	type stored struct {
		id        int64
		name      string
		scopes    []string
		expiresAt interface{}
		revokedAt interface{}
		prefix    string
	}
	tokens := map[int64]*stored{}
	var nextID int64

	render := func(t *stored, raw string) map[string]interface{} {
		out := map[string]interface{}{
			"id": t.id, "name": t.name, "token_prefix": t.prefix,
			"scopes": t.scopes, "expires_at": t.expiresAt,
			"last_used_at": nil, "revoked_at": t.revokedAt,
			"active":     t.revokedAt == nil,
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}
		if raw != "" {
			out["token"] = raw
		}
		return out
	}

	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodDelete:
			id := pathID(r.URL.Path)
			t, ok := tokens[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
				return
			}
			t.revokedAt = "2026-01-02T00:00:00Z"
			json.NewEncoder(w).Encode(map[string]interface{}{"api_token": render(t, "")})

		case r.Method == http.MethodPost:
			var body struct {
				APIToken struct {
					Name      string   `json:"name"`
					Scopes    []string `json:"scopes"`
					ExpiresAt *string  `json:"expires_at"`
				} `json:"api_token"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			// requested | ["read"] — the read floor lands LAST, and the set is
			// never sorted. Sorting it here would hide the ordering hazard.
			scopes := []string{}
			hasRead := false
			for _, s := range body.APIToken.Scopes {
				if s == "read" {
					hasRead = true
				}
				scopes = append(scopes, s)
			}
			if !hasRead {
				scopes = append(scopes, "read")
			}

			var expires interface{}
			if body.APIToken.ExpiresAt != nil {
				// A bare date gains the time the server renders it with.
				v := *body.APIToken.ExpiresAt
				if len(v) == 10 {
					v += "T00:00:00Z"
				}
				expires = v
			}

			nextID++
			t := &stored{
				id: nextID, name: body.APIToken.Name, scopes: scopes,
				expiresAt: expires, prefix: fmt.Sprintf("fn_0000%d", nextID),
			}
			tokens[t.id] = t
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"api_token": render(t, fmt.Sprintf("fn_0000%dsecretvalue", t.id)),
			})

		default: // index
			list := []map[string]interface{}{}
			for _, t := range tokens {
				list = append(list, render(t, ""))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"api_tokens": list,
				"meta": map[string]int{
					"current_page": 1, "total_pages": 1,
					"total_count": len(list), "per_page": 100,
				},
			})
		}
	}
}

func pathID(path string) int64 {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	id, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return id
}

// The read floor, end to end. The configuration asks for ["write"], the API
// stores ["read", "write"], and the two have to reconcile — or the second plan
// offers to destroy the credential the first one minted.
func TestAPITokenPlan_WriteScopeIsStable(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	cfg := providerConfig + `
resource "fivenines_api_token" "test" {
  name   = "CI deploy key"
  scopes = ["write"]
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					// State keeps what the practitioner wrote, not the API's echo.
					resource.TestCheckResourceAttr("fivenines_api_token.test", "scopes.#", "1"),
					resource.TestCheckTypeSetElemAttr("fivenines_api_token.test", "scopes.*", "write"),
					// The value exists exactly once, in the create response.
					resource.TestCheckResourceAttr("fivenines_api_token.test", "token", "fn_00001secretvalue"),
					resource.TestCheckResourceAttr("fivenines_api_token.test", "token_prefix", "fn_00001"),
					resource.TestCheckResourceAttr("fivenines_api_token.test", "active", "true"),
				),
			},
			{Config: cfg, PlanOnly: true},
			// A refresh re-reads the index, which never carries the value. The
			// mapping has to leave the stored one alone: wiping it here would
			// empty `terraform output ci_token` without changing a single plan.
			{
				RefreshState: true,
				Check:        resource.TestCheckResourceAttr("fivenines_api_token.test", "token", "fn_00001secretvalue"),
			},
		},
	})
}

// ["read", "write"] and ["write"] describe one token, so rewriting the
// configuration from one to the other must not plan anything at all — least of
// all the replacement that would mint a new secret.
func TestAPITokenPlan_EquivalentScopeRewriteIsNoOp(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	implicit := providerConfig + `
resource "fivenines_api_token" "test" {
  name   = "CI deploy key"
  scopes = ["write"]
}`
	explicit := providerConfig + `
resource "fivenines_api_token" "test" {
  name   = "CI deploy key"
  scopes = ["read", "write"]
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: implicit},
			// Spelling the floor out changes nothing on the wire or in state.
			{Config: explicit, PlanOnly: true},
		},
	})
}

// Narrowing the scopes is a real change, and the only way to apply it is a new
// token — the API cannot edit one.
func TestAPITokenPlan_NarrowingScopesReplaces(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	write := providerConfig + `
resource "fivenines_api_token" "test" {
  name   = "CI deploy key"
  scopes = ["write"]
}`
	readOnly := providerConfig + `
resource "fivenines_api_token" "test" {
  name   = "CI deploy key"
  scopes = ["read"]
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: write,
				Check:  resource.TestCheckResourceAttr("fivenines_api_token.test", "token", "fn_00001secretvalue"),
			},
			{
				Config: readOnly,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_api_token.test", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// A replacement mints a fresh value; the old one is not carried over.
					resource.TestCheckResourceAttr("fivenines_api_token.test", "token", "fn_00002secretvalue"),
					resource.TestCheckResourceAttr("fivenines_api_token.test", "scopes.#", "1"),
					resource.TestCheckTypeSetElemAttr("fivenines_api_token.test", "scopes.*", "read"),
				),
			},
			{Config: readOnly, PlanOnly: true},
		},
	})
}

// A bare date is a legal expires_at, and the API answers with its own rendering
// of the same instant. Storing that instead would fail the apply outright and
// then propose a replacement on every plan afterwards.
func TestAPITokenPlan_DateOnlyExpiryIsStable(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	cfg := providerConfig + `
resource "fivenines_api_token" "test" {
  name       = "expiring"
  expires_at = "2026-12-01"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_api_token.test", "expires_at", "2026-12-01"),
					// Omitted scopes default to the read floor.
					resource.TestCheckResourceAttr("fivenines_api_token.test", "scopes.#", "1"),
					resource.TestCheckTypeSetElemAttr("fivenines_api_token.test", "scopes.*", "read"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// A token revoked outside Terraform authenticates nothing and cannot be
// un-revoked, so the refresh has to drop it and the next plan has to mint a
// replacement. Left in state it would be a dead credential Terraform reports as
// healthy.
func TestAPITokenPlan_RevokedOutOfBandIsRecreated(t *testing.T) {
	var revokeNext atomic.Bool
	inner := apiTokenHandler()
	apiTokenPlanTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && revokeNext.Load() {
			// Serve the row as the dashboard would after a revoke: still listed,
			// stamped, inactive.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"api_tokens": []map[string]interface{}{{
					"id": 1, "name": "CI deploy key", "token_prefix": "fn_00001",
					"scopes": []string{"read"}, "expires_at": nil, "last_used_at": nil,
					"revoked_at": "2026-01-02T00:00:00Z", "active": false,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				}},
				"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
			})
			return
		}
		inner(w, r)
	})

	cfg := providerConfig + `
resource "fivenines_api_token" "test" {
  name = "CI deploy key"
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
						plancheck.ExpectResourceAction("fivenines_api_token.test", plancheck.ResourceActionCreate),
					},
				},
				// The refresh drops the revoked row, so the plan is a create.
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// An expired token is NOT dropped: recreating it would re-send the same past
// expires_at, which the API refuses, and the configuration would be wedged into
// a plan that fails on every run. It stays in state reporting active = false.
func TestAPITokenPlan_ExpiredTokenStaysInState(t *testing.T) {
	var expire atomic.Bool
	inner := apiTokenHandler()
	apiTokenPlanTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && expire.Load() {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"api_tokens": []map[string]interface{}{{
					"id": 1, "name": "expiring", "token_prefix": "fn_00001",
					"scopes": []string{"read"}, "expires_at": "2026-12-01T00:00:00Z",
					"last_used_at": nil, "revoked_at": nil,
					// Past its expiry: dead, but never revoked.
					"active":     false,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
				}},
				"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
			})
			return
		}
		inner(w, r)
	})

	cfg := providerConfig + `
resource "fivenines_api_token" "test" {
  name       = "expiring"
  expires_at = "2026-12-01"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig:    func() { expire.Store(true) },
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Still managed, and honest about being dead.
					resource.TestCheckResourceAttr("fivenines_api_token.test", "active", "false"),
					resource.TestCheckResourceAttr("fivenines_api_token.test", "expires_at", "2026-12-01"),
				),
			},
			// And it does not churn: an expired token plans no replacement.
			{Config: cfg, PlanOnly: true},
		},
	})
}

// Import brings the metadata under management. The value is the one thing it
// cannot bring: the plaintext existed for one response and the server keeps only
// a digest, so `token` is null on an imported token and stays null.
func TestAPITokenPlan_ImportHasNoValue(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	// Two tokens, and the one under management is the second: an import that
	// ignored the id it was handed would silently adopt the wrong credential.
	cfg := providerConfig + `
resource "fivenines_api_token" "other" {
  name = "unrelated"
}

resource "fivenines_api_token" "test" {
  name       = "CI deploy key"
  depends_on = [fivenines_api_token.other]
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Config:            cfg,
				ResourceName:      "fivenines_api_token.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The value cannot round-trip: state has it from the create, an
				// import can never fetch it.
				ImportStateVerifyIgnore: []string{"token"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["token"]; got != "" {
						return fmt.Errorf("imported token carried a value %q; the API cannot return one", got)
					}
					// The second token, not the first: proof the id was used.
					if got := states[0].Attributes["id"]; got != "2" {
						return fmt.Errorf("imported id = %q, want 2", got)
					}
					if got := states[0].Attributes["token_prefix"]; got != "fn_00002" {
						return fmt.Errorf("imported token_prefix = %q, want fn_00002", got)
					}
					return nil
				},
			},
		},
	})
}

// Nothing about a token is editable, so a rename is a new credential. Without
// RequiresReplace this routes to Update, which refuses — the apply fails instead
// of rotating.
func TestAPITokenPlan_RenameReplaces(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	before := providerConfig + `
resource "fivenines_api_token" "test" {
  name = "CI deploy key"
}`
	after := providerConfig + `
resource "fivenines_api_token" "test" {
  name = "CI deploy key v2"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: before,
				Check:  resource.TestCheckResourceAttr("fivenines_api_token.test", "id", "1"),
			},
			{
				Config: after,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_api_token.test", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_api_token.test", "id", "2"),
					resource.TestCheckResourceAttr("fivenines_api_token.test", "token", "fn_00002secretvalue"),
				),
			},
		},
	})
}

// Moving the expiry is the ordinary rotation trigger — a time_rotating input
// advances it — and it has to mint a new token rather than fail the apply.
func TestAPITokenPlan_ExpiryChangeReplaces(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	before := providerConfig + `
resource "fivenines_api_token" "test" {
  name       = "rotating"
  expires_at = "2026-12-01"
}`
	after := providerConfig + `
resource "fivenines_api_token" "test" {
  name       = "rotating"
  expires_at = "2027-03-01"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: before},
			{
				Config: after,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_api_token.test", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_api_token.test", "expires_at", "2027-03-01"),
					resource.TestCheckResourceAttr("fivenines_api_token.test", "token", "fn_00002secretvalue"),
				),
			},
		},
	})
}

// An expiry the provider cannot read is refused at plan time, before anything is
// minted. Left to the API it would be a 422 mid-apply — and a value that parses
// for the server but not for the provider drifts on every plan afterwards.
func TestAPITokenPlan_UnparseableExpiryFailsAtPlanTime(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_api_token" "test" {
  name       = "bad expiry"
  expires_at = "next friday"
}`,
				ExpectError: regexp.MustCompile(`Invalid expires_at`),
				PlanOnly:    true,
			},
		},
	})
}

// allow_self_revoke lives in state and nowhere else, so flipping it is the one
// change this resource can apply in place. It must not touch the credential:
// same id, same value, no replacement.
func TestAPITokenPlan_AllowSelfRevokeTogglesInPlace(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	guarded := providerConfig + `
resource "fivenines_api_token" "test" {
  name = "CI deploy key"
}`
	unguarded := providerConfig + `
resource "fivenines_api_token" "test" {
  name              = "CI deploy key"
  allow_self_revoke = true
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: guarded,
				Check:  resource.TestCheckResourceAttr("fivenines_api_token.test", "allow_self_revoke", "false"),
			},
			{
				Config: unguarded,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_api_token.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_api_token.test", "allow_self_revoke", "true"),
					// The credential is untouched by a local-only edit.
					resource.TestCheckResourceAttr("fivenines_api_token.test", "id", "1"),
					resource.TestCheckResourceAttr("fivenines_api_token.test", "token", "fn_00001secretvalue"),
					resource.TestCheckResourceAttr("fivenines_api_token.test", "created_at", "2026-01-01T00:00:00Z"),
				),
			},
			{Config: unguarded, PlanOnly: true},
		},
	})
}

// The scope vocabulary is closed. A typo has to fail at plan time: sent to the
// API it is a 422 mid-apply, and the provider would have carried the bad value
// through its own normalisation on the way there.
func TestAPITokenPlan_UnknownScopeFailsAtPlanTime(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_api_token" "test" {
  name   = "typo"
  scopes = ["admin"]
}`,
				ExpectError: regexp.MustCompile(`(?s)Attribute scopes.*value must be one of`),
				PlanOnly:    true,
			},
		},
	})
}

// An explicitly empty scope set is a 422 server-side — it is the shape a
// template renders when its variable is empty, and the API refuses to read it as
// "give me the default". Refuse it at plan time for the same reason.
func TestAPITokenPlan_EmptyScopeSetFailsAtPlanTime(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_api_token" "test" {
  name   = "empty"
  scopes = []
}`,
				ExpectError: regexp.MustCompile(`(?s)Attribute scopes.*at least 1`),
				PlanOnly:    true,
			},
		},
	})
}

// A token that leaves the index entirely — the user who owned it was removed, so
// the row went with them — is gone, and the next plan mints a replacement. This
// is the 404 path, distinct from the revoked row that is still listed.
func TestAPITokenPlan_VanishedTokenIsRecreated(t *testing.T) {
	var vanish atomic.Bool
	inner := apiTokenHandler()
	apiTokenPlanTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && vanish.Load() {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"api_tokens": []map[string]interface{}{},
				"meta":       map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
			})
			return
		}
		inner(w, r)
	})

	cfg := providerConfig + `
resource "fivenines_api_token" "test" {
  name = "CI deploy key"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() { vanish.Store(true) },
				Config:    cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_api_token.test", plancheck.ResourceActionCreate),
					},
				},
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// The serializer renders expires_at with no fractional digits, so a sub-second
// expiry cannot round-trip: accepted, it fails the apply on inconsistent result
// and then proposes replacing the credential on every plan after.
func TestAPITokenPlan_SubSecondExpiryFailsAtPlanTime(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_api_token" "test" {
  name       = "precise"
  expires_at = "2026-12-01T00:00:00.500Z"
}`,
				ExpectError: regexp.MustCompile(`sub-second precision`),
				PlanOnly:    true,
			},
		},
	})
}

// C3: rewriting an expiry to an equivalent rendering names the same instant, so
// it must not revoke a live credential to mint an identical one. The same
// hazard scopes has, on the other attribute the server re-renders.
func TestAPITokenPlan_EquivalentExpiryRewriteIsNoOp(t *testing.T) {
	apiTokenPlanTest(t, apiTokenHandler())

	bare := providerConfig + `
resource "fivenines_api_token" "test" {
  name       = "rotating"
  expires_at = "2026-12-01"
}`
	spelled := providerConfig + `
resource "fivenines_api_token" "test" {
  name       = "rotating"
  expires_at = "2026-12-01T00:00:00Z"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: bare,
				Check:  resource.TestCheckResourceAttr("fivenines_api_token.test", "token", "fn_00001secretvalue"),
			},
			{Config: spelled, PlanOnly: true},
		},
	})
}

// C2: an expiry that only resolves at apply time is unknown when ValidateConfig
// runs, so the check has to happen again in Create — before a credential exists
// that state would then disagree with.
//
// The unknown has to be genuine: a constant, a variable with a default, or
// terraform_data over a known input are all resolved during the plan, and a test
// built on one of those passes with the Create-side check deleted. Deriving it
// from another token's created_at — Computed, and unknowable until that token is
// minted — is what makes the plan-time validator actually defer.
func TestAPITokenPlan_UnknownSubSecondExpiryFailsBeforeMinting(t *testing.T) {
	var posts atomic.Int32
	inner := apiTokenHandler()
	apiTokenPlanTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		inner(w, r)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_api_token" "other" {
  name = "unrelated"
}

resource "fivenines_api_token" "test" {
  name       = "interpolated"
  expires_at = "${substr(fivenines_api_token.other.created_at, 0, 19)}.500Z"
}`,
				ExpectError: regexp.MustCompile(`sub-second precision`),
			},
		},
	})

	// One POST, for `other`. A second would mean the refused token was minted
	// anyway — live, unmanaged, and holding an expiry state disagrees with.
	if got := posts.Load(); got != 1 {
		t.Errorf("expected exactly 1 token minted (the unrelated one), got %d", got)
	}
}
