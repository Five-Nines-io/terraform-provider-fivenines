package provider_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The security data sources exist to make one reading impossible: an
// unexamined subject read as a clean one. Terraform is where that promise is
// actually kept or broken, and the unit tests cannot see it — they drive Read
// directly, so they never watch Terraform render a nil slice, never evaluate a
// precondition, and never run a validator. Every assertion below is one the
// whole unit suite passes without.

const instanceUUID = "3cac0e44-0000-4000-8000-000000000001"

func criticalFinding() map[string]interface{} {
	return map[string]interface{}{
		"id": 84213, "host_id": instanceUUID, "host_name": "web-01",
		"ecosystem": "Ubuntu:22.04", "package_name": "openssl",
		"installed_version": "3.0.2-0ubuntu1.10",
		"vulnerability_id":  "UBUNTU-CVE-2024-2511",
		"cve_ids":           []string{"CVE-2024-2511"},
		"summary":           "openssl: unbounded memory growth",
		"advisory_url":      "https://ubuntu.com/security/CVE-2024-2511",
		"cvss_score":        9.8, "severity": "Critical",
		"patchable": true, "fix_state": "fixed",
		"fix_version": "3.0.2-0ubuntu1.15", "requires_subscription": false,
		"vendor": "Canonical", "vendor_note": "fixed in 22.04 LTS",
		"detected_at": "2026-08-30T12:00:00Z", "updated_at": "2026-08-30T12:00:00Z",
	}
}

func fullPage() map[string]int {
	return map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100}
}

// --- The refusal, end to end ---

// THE ACCEPTANCE CRITERION OF THIS FEATURE. A never-scanned instance answers
// `vulnerabilities: null`, which has to reach the practitioner as a NULL list:
// `length(null)` fails the plan, where an empty list would have passed it and
// reported a host nothing ever looked at as clean.
//
// Only a plan test can prove this. The client returns a nil slice either way,
// and the framework's reflection is what turns nil into null rather than `[]`
// — a detail no direct Read call exercises.
func TestVulnerabilitiesPlan_NeverScannedInstanceFailsTheGate(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": nil,
			"scan":            map[string]interface{}{"last_checked_at": nil, "never_checked": true},
			"meta":            nil,
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "unscanned" {
  instance_id = "` + instanceUUID + `"
}

output "count" {
  value = length(data.fivenines_vulnerabilities.unscanned.vulnerabilities)
}`,
			// Terraform refuses to call length() on a null. That is the whole
			// design: the gate fails loudly instead of passing quietly.
			ExpectError: regexp.MustCompile(`(?s)Invalid function argument|argument must not be null`),
		}},
	})
}

// The other half of the contract: a scanned instance with nothing wrong is a
// real all-clear and must read as an EMPTY list, so the same gate passes.
func TestVulnerabilitiesPlan_ScannedAndCleanPassesTheGate(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{},
			"scan":            map[string]interface{}{"last_checked_at": "2026-08-30T12:00:00Z", "never_checked": false},
			"meta":            map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "clean" {
  instance_id = "` + instanceUUID + `"
}

output "count" {
  value = length(data.fivenines_vulnerabilities.clean.vulnerabilities)
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.clean", "vulnerabilities.#", "0"),
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.clean", "scan.never_checked", "false"),
				resource.TestCheckOutput("count", "0"),
			),
		}},
	})
}

// A non-scanned IMAGE refuses the same way, and the image comes back in full so
// a caller can branch on `state` rather than on the null.
func TestVulnerabilitiesPlan_PendingImageRefusesButExplainsWhy(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_image": map[string]interface{}{
				"id": "image-uuid", "organization_id": 42,
				"image_id": "sha256:1f2e3d", "short_digest": "1f2e3d4c5b6a",
				"display_name": "nginx:1.27", "tags": []string{"nginx:1.27"},
				"repo_digests": []string{}, "state": "pending",
				"state_reason": "inventory not received", "countable": false,
				"vulnerability_count": nil, "critical_vulnerability_count": nil,
				"packages_truncated": false, "finding_count_is_floor": false,
				"created_at": "2026-08-30T12:00:00Z", "updated_at": "2026-08-30T12:00:00Z",
			},
			"vulnerabilities": nil,
			"meta":            nil,
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "image" {
  docker_image_id = "image-uuid"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				// Null, not an empty list.
				resource.TestCheckNoResourceAttr("data.fivenines_vulnerabilities.image", "vulnerabilities.#"),
				// The branchable answer that says why.
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.image", "docker_image.state", "pending"),
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.image", "docker_image.countable", "false"),
				resource.TestCheckNoResourceAttr("data.fivenines_vulnerabilities.image", "docker_image.vulnerability_count"),
			),
		}},
	})
}

// --- Filters ---

// A filter silently dropped reaches the API as an unfiltered list the
// practitioner never asked for — on a security index, a widened question whose
// answer still looks like an answer.
func TestVulnerabilitiesPlan_FiltersReachTheAPIAndRoundTrip(t *testing.T) {
	var got url.Values
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{criticalFinding()},
			"scan": map[string]interface{}{
				"oldest_checked_at": "2026-08-29T00:00:00Z", "instances_never_checked": 0,
			},
			"meta": fullPage(),
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "critical" {
  severity     = ["Critical", "High"]
  patchable    = true
  fix_state    = "fixed"
  package_name = "openssl"
  ecosystem    = "Ubuntu:22.04"
  order        = "cvss_score"
  direction    = "desc"
}

output "packages" {
  value = join(",", [for v in data.fivenines_vulnerabilities.critical.vulnerabilities : v.package_name])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.critical", "vulnerabilities.#", "1"),
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.critical", "vulnerabilities.0.severity", "Critical"),
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.critical", "vulnerabilities.0.cvss_score", "9.8"),
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.critical", "vulnerabilities.0.cve_ids.0", "CVE-2024-2511"),
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.critical", "vulnerabilities.0.patchable", "true"),
				// The fleet half of the scan block, which the org-wide gate reads.
				resource.TestCheckResourceAttr("data.fivenines_vulnerabilities.critical", "scan.instances_never_checked", "0"),
				resource.TestCheckOutput("packages", "openssl"),
			),
		}},
	})

	// The filters have to reach the API, not just survive into state. Severity
	// travels as ONE comma-separated value, which is the shape the API parses.
	for key, want := range map[string]string{
		"severity":     "Critical,High",
		"patchable":    "true",
		"fix_state":    "fixed",
		"package_name": "openssl",
		"ecosystem":    "Ubuntu:22.04",
		"order":        "cvss_score",
		"direction":    "desc",
		"per_page":     "100",
	} {
		if got.Get(key) != want {
			t.Errorf("query %s = %q, want %q", key, got.Get(key), want)
		}
	}
}

// --- Validators ---

// The two scopes are mutually exclusive: a nested route with a second,
// contradictory subject is not a query the API has.
func TestVulnerabilitiesPlan_InstanceAndImageScopesConflict(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "both" {
  instance_id     = "` + instanceUUID + `"
  docker_image_id = "image-uuid"
}`,
			ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination`),
		}},
	})
}

// `ecosystem` is an org-wide-only filter. Catching it at plan time beats the
// API's 400 at apply time, and beats silently dropping it — which would answer
// a wider question than the config asked.
func TestVulnerabilitiesPlan_EcosystemConflictsWithAScopedQuery(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "scoped" {
  instance_id = "` + instanceUUID + `"
  ecosystem   = "Ubuntu:22.04"
}`,
			ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination`),
		}},
	})
}

func TestVulnerabilitiesPlan_SeverityVocabularyIsClosed(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "typo" {
  severity = ["critical"]
}`,
			ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Match|value must be one of`),
		}},
	})
}

// An empty string filter is dropped by the shared filter helpers, which on a
// security index would silently widen the question — and an empty `instance_id`
// would build /api/v1/instances//vulnerabilities. Both fail at plan time.
func TestVulnerabilitiesPlan_EmptyFilterIsRejected(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be reached: the config is invalid")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "empty" {
  package_name = ""
}`,
			ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Length|at least 1`),
		}},
	})
}

// The scope arguments specifically: an empty one is not a widened query but a
// malformed URL, /api/v1/instances//vulnerabilities.
func TestVulnerabilitiesPlan_EmptyScopeIsRejected(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be reached: the config is invalid")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "empty_scope" {
  instance_id = ""
}`,
			ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Length|at least 1`),
		}},
	})
}

func TestOrganizationDockerImagesPlan_EmptyFilterIsRejected(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be reached: the config is invalid")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_organization_docker_images" "empty" {
  q = ""
}`,
			ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Length|at least 1`),
		}},
	})
}

// --- The plan gate ---

// A plan without security_details answers 403. It must surface as a diagnostic
// naming the plan, never as an empty result — an empty result is what a build
// gate reads as PASSED.
func TestVulnerabilitiesPlan_ForbiddenNamesThePlanGate(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Vulnerability details (CVEs, scores, fix versions) require the Pro plan or above.",
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_vulnerabilities" "gated" {}`,
			ExpectError: regexp.MustCompile(`(?s)security_details`),
		}},
	})
}

// --- Container images ---

// The honesty contract at the Terraform layer: a pending image must carry NO
// count, and the org-wide posture must survive the round trip — it is the
// answer to "is this list the whole picture", which the filters never narrow.
func TestOrganizationDockerImagesPlan_NullCountsAndPosture(t *testing.T) {
	var got url.Values
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_images": []interface{}{
				map[string]interface{}{
					"id": "image-scanned", "organization_id": 42,
					"image_id": "sha256:1f2e3d", "short_digest": "1f2e3d4c5b6a",
					"display_name": "nginx:1.27", "tags": []string{"nginx:1.27"},
					"repo_digests": []string{"nginx@sha256:1f2e3d"},
					"distro":       "debian:12", "ecosystem": "Debian:12",
					"state": "scanned", "countable": true,
					"vulnerability_count": 12, "critical_vulnerability_count": 3,
					"packages_truncated": true, "finding_count_is_floor": true,
					"last_seen_at": "2026-08-30T12:00:00Z", "running_host_count": 4,
					"created_at": "2026-08-01T00:00:00Z", "updated_at": "2026-08-30T12:00:00Z",
				},
				map[string]interface{}{
					"id": "image-pending", "organization_id": 42,
					"image_id": "sha256:9a8b7c", "short_digest": "9a8b7c6d5e4f",
					"display_name": "scratch", "tags": []string{},
					"repo_digests": []string{}, "state": "pending",
					"countable": false, "vulnerability_count": nil,
					"critical_vulnerability_count": nil,
					"packages_truncated":           false, "finding_count_is_floor": false,
					"created_at": "2026-08-01T00:00:00Z", "updated_at": "2026-08-30T12:00:00Z",
				},
			},
			"posture": map[string]int{"pending": 1, "scanned": 7, "unsupported": 2, "unscannable": 1},
			"meta":    map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_organization_docker_images" "all" {
  state              = "scanned"
  packages_truncated = true
  q                  = "nginx"
}

# state before counts: a null count is not comparable to a number, so the
# scanned set has to be split off before anything compares it.
output "criticals" {
  value = join(",", [
    for i in data.fivenines_organization_docker_images.all.images :
    "${i.display_name}=${i.critical_vulnerability_count}"
    if i.state == "scanned"
  ])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_organization_docker_images.all", "images.#", "2"),
				// Scanned: counts present, and flagged as a floor.
				resource.TestCheckResourceAttr("data.fivenines_organization_docker_images.all", "images.0.vulnerability_count", "12"),
				resource.TestCheckResourceAttr("data.fivenines_organization_docker_images.all", "images.0.finding_count_is_floor", "true"),
				resource.TestCheckResourceAttr("data.fivenines_organization_docker_images.all", "images.0.running_host_count", "4"),
				// Pending: NO count at all. A zero here would be the lie.
				resource.TestCheckNoResourceAttr("data.fivenines_organization_docker_images.all", "images.1.vulnerability_count"),
				resource.TestCheckNoResourceAttr("data.fivenines_organization_docker_images.all", "images.1.critical_vulnerability_count"),
				resource.TestCheckResourceAttr("data.fivenines_organization_docker_images.all", "images.1.countable", "false"),
				// Posture is org-wide and deliberately unfiltered.
				resource.TestCheckResourceAttr("data.fivenines_organization_docker_images.all", "posture.pending", "1"),
				resource.TestCheckResourceAttr("data.fivenines_organization_docker_images.all", "posture.scanned", "7"),
				resource.TestCheckOutput("criticals", "nginx:1.27=3"),
			),
		}},
	})

	for key, want := range map[string]string{
		"state": "scanned", "packages_truncated": "true", "q": "nginx", "per_page": "100",
	} {
		if got.Get(key) != want {
			t.Errorf("query %s = %q, want %q", key, got.Get(key), want)
		}
	}
}

func TestOrganizationDockerImagesPlan_StateVocabularyIsClosed(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_organization_docker_images" "typo" {
  state = "scanning"
}`,
			ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Match|value must be one of`),
		}},
	})
}
