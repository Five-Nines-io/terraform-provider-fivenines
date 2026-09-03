package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
)

// requestedPage echoes the page the caller asked for. morePages compares
// meta.CurrentPage against meta.TotalPages, so a stub that hardcodes
// current_page while claiming several total_pages walks until maxListPages
// instead of fetching the pages it advertised.
func requestedPage(r *http.Request) int {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// --- Vulnerabilities ---

func TestClient_ListVulnerabilities_PaginationAndFilters(t *testing.T) {
	var gotQueries []string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/vulnerabilities" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotQueries = append(gotQueries, r.URL.RawQuery)

		page := requestedPage(r)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{
				map[string]interface{}{
					"id": 84213, "host_id": "host-uuid", "host_name": "web-01",
					"ecosystem": "Ubuntu:22.04", "package_name": "openssl",
					"installed_version": "3.0.2-0ubuntu1.10",
					"vulnerability_id":  "UBUNTU-CVE-2024-2511",
					"cve_ids":           []string{"CVE-2024-2511"},
					"summary":           "openssl: unbounded memory growth",
					"advisory_url":      "https://ubuntu.com/security/CVE-2024-2511",
					"cvss_score":        9.8, "severity": "Critical",
					"patchable": true, "fix_state": "fixed",
					"fix_version": "3.0.2-0ubuntu1.15",
					"vendor":      "Canonical", "vendor_note": "fixed in 22.04 LTS",
					"detected_at": "2026-08-30T12:00:00Z",
				},
			},
			"scan": map[string]interface{}{
				"oldest_checked_at":       "2026-08-29T00:00:00Z",
				"instances_never_checked": 2,
			},
			"meta": map[string]int{
				"current_page": page, "total_pages": 2, "total_count": 2, "per_page": 100,
			},
		})
	})

	patchable := true
	list, err := c.ListVulnerabilities(context.Background(), VulnerabilityListOptions{
		Severity:  []string{"Critical", "High"},
		Patchable: &patchable,
		Ecosystem: "Ubuntu:22.04",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gotQueries) != 2 {
		t.Fatalf("expected 2 requests for total_pages=2, got %d: %v", len(gotQueries), gotQueries)
	}
	if len(list.Vulnerabilities) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(list.Vulnerabilities))
	}

	q, err := url.ParseQuery(gotQueries[0])
	if err != nil {
		t.Fatalf("parsing query: %v", err)
	}
	// The API takes several severities as ONE comma-separated value.
	if got := q.Get("severity"); got != "Critical,High" {
		t.Errorf("expected severity 'Critical,High', got %q", got)
	}
	if got := q.Get("patchable"); got != "true" {
		t.Errorf("expected patchable 'true', got %q", got)
	}
	if got := q.Get("ecosystem"); got != "Ubuntu:22.04" {
		t.Errorf("expected ecosystem 'Ubuntu:22.04', got %q", got)
	}
	if got := q.Get("per_page"); got != "100" {
		t.Errorf("expected per_page '100', got %q", got)
	}
	if q2, _ := url.ParseQuery(gotQueries[1]); q2.Get("page") != "2" {
		t.Errorf("expected second request on page 2, got %q", q2.Get("page"))
	}

	v := list.Vulnerabilities[0]
	if v.Severity != "Critical" || v.CVSSScore == nil || *v.CVSSScore != 9.8 {
		t.Errorf("unexpected finding: %+v", v)
	}
	if len(v.CVEIDs) != 1 || v.CVEIDs[0] != "CVE-2024-2511" {
		t.Errorf("expected one CVE alias, got %v", v.CVEIDs)
	}
	if v.HostName == nil || *v.HostName != "web-01" {
		t.Errorf("expected host_name web-01, got %v", v.HostName)
	}
	if v.FixVersion == nil || *v.FixVersion != "3.0.2-0ubuntu1.15" {
		t.Errorf("expected a fix version, got %v", v.FixVersion)
	}
	if list.Scan == nil || list.Scan.InstancesNeverChecked == nil || *list.Scan.InstancesNeverChecked != 2 {
		t.Errorf("expected scan.instances_never_checked 2, got %+v", list.Scan)
	}
}

// An unset filter must never reach the API: it rejects an unknown or empty
// filter with a 400 rather than answering an unfiltered list, and on a security
// index a silently widened question is the failure that matters.
func TestClient_ListVulnerabilities_OmitsUnsetFilters(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{},
			"meta": map[string]int{
				"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100,
			},
		})
	})

	if _, err := c.ListVulnerabilities(context.Background(), VulnerabilityListOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, key := range []string{
		"severity", "patchable", "fix_state", "package_name",
		"vulnerability_id", "ecosystem", "q", "updated_since", "order", "direction",
	} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %q to be omitted, got %q", key, gotQuery.Get(key))
		}
	}
}

// A scanned subject with nothing wrong is an empty list, and it must stay
// distinguishable from the refusal below.
func TestClient_ListVulnerabilities_EmptyIsNotNil(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{},
			"scan": map[string]interface{}{
				"oldest_checked_at": "2026-08-30T12:00:00Z", "instances_never_checked": 0,
			},
			"meta": map[string]int{
				"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100,
			},
		})
	})

	list, err := c.ListVulnerabilities(context.Background(), VulnerabilityListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities == nil {
		t.Fatal("expected an empty non-nil slice for a scanned fleet with no findings")
	}
	if len(list.Vulnerabilities) != 0 {
		t.Errorf("expected 0 findings, got %d", len(list.Vulnerabilities))
	}
}

// THE REFUSAL: a never-scanned instance answers `vulnerabilities: null` beside
// `meta: null`, and that must not collapse into an empty list.
func TestClient_ListInstanceVulnerabilities_NeverScanned(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/instances/host-uuid/vulnerabilities" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": nil,
			"scan":            map[string]interface{}{"last_checked_at": nil, "never_checked": true},
			"meta":            nil,
		})
	})

	list, err := c.ListInstanceVulnerabilities(context.Background(), "host-uuid", VulnerabilityListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities != nil {
		t.Errorf("expected nil findings for a never-scanned instance, got %v", list.Vulnerabilities)
	}
	if list.Scan == nil || list.Scan.NeverChecked == nil || !*list.Scan.NeverChecked {
		t.Errorf("expected scan.never_checked true, got %+v", list.Scan)
	}
}

// A HALF-null response is malformed, and the safe reading of it is the refusal.
// A null array beside a valid `meta` would otherwise decode to an empty list
// and read as an all-clear for a subject that just said it had no answer.
func TestClient_ListVulnerabilities_NullArrayWithValidMetaIsRefused(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": nil,
			"meta": map[string]int{
				"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100,
			},
		})
	})

	list, err := c.ListVulnerabilities(context.Background(), VulnerabilityListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities != nil {
		t.Errorf("expected the refusal, got an empty list that reads as an all-clear")
	}
}

// The same for an omitted key, which decodes to nil identically.
func TestClient_ListVulnerabilities_MissingArrayIsRefused(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"meta": map[string]int{
				"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100,
			},
		})
	})

	list, err := c.ListVulnerabilities(context.Background(), VulnerabilityListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities != nil {
		t.Errorf("expected the refusal for a missing array, got %v", list.Vulnerabilities)
	}
}

// A scanned instance with findings pages normally and carries the host pair of
// the scan block.
func TestClient_ListInstanceVulnerabilities_Scanned(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{
				map[string]interface{}{"id": 1, "package_name": "zlib1g", "severity": "Unknown"},
			},
			"scan": map[string]interface{}{
				"last_checked_at": "2026-08-30T12:00:00Z", "never_checked": false,
			},
			"meta": map[string]int{
				"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100,
			},
		})
	})

	list, err := c.ListInstanceVulnerabilities(context.Background(), "host-uuid", VulnerabilityListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Vulnerabilities) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(list.Vulnerabilities))
	}
	if list.Scan.NeverChecked == nil || *list.Scan.NeverChecked {
		t.Error("expected never_checked false on a scanned instance")
	}
	// A missing score is Unknown, never a zero.
	if list.Vulnerabilities[0].CVSSScore != nil {
		t.Errorf("expected a nil score, got %v", *list.Vulnerabilities[0].CVSSScore)
	}
}

// `ecosystem` is an org-wide-only filter; the nested routes 400 on it.
func TestClient_ListInstanceVulnerabilities_DropsEcosystemFilter(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vulnerabilities": []interface{}{},
			"scan":            map[string]interface{}{"never_checked": false},
			"meta": map[string]int{
				"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100,
			},
		})
	})

	_, err := c.ListInstanceVulnerabilities(context.Background(), "host-uuid", VulnerabilityListOptions{
		Ecosystem: "Ubuntu:22.04",
		Severity:  []string{"Critical"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := gotQuery["ecosystem"]; ok {
		t.Error("expected ecosystem to be dropped on the nested route")
	}
	if got := gotQuery.Get("severity"); got != "Critical" {
		t.Errorf("expected severity to survive, got %q", got)
	}
}

func TestClient_ListDockerImageVulnerabilities_DropsEcosystemFilter(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_image":    map[string]interface{}{"id": "image-uuid", "state": "scanned", "countable": true},
			"vulnerabilities": []interface{}{},
			"meta": map[string]int{
				"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100,
			},
		})
	})

	if _, err := c.ListDockerImageVulnerabilities(context.Background(), "image-uuid", VulnerabilityListOptions{
		Ecosystem: "Debian:12",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := gotQuery["ecosystem"]; ok {
		t.Error("expected ecosystem to be dropped on the image route")
	}
}

// A non-scanned image refuses the same way, and returns the image so a caller
// can tell WHY there are no findings.
func TestClient_ListDockerImageVulnerabilities_NotScanned(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/docker_images/image-uuid/vulnerabilities" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_image": map[string]interface{}{
				"id": "image-uuid", "state": "pending",
				"state_reason": "inventory not received", "countable": false,
				"vulnerability_count": nil, "critical_vulnerability_count": nil,
				"packages_truncated": false, "finding_count_is_floor": false,
			},
			"vulnerabilities": nil,
			"meta":            nil,
		})
	})

	list, err := c.ListDockerImageVulnerabilities(context.Background(), "image-uuid", VulnerabilityListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities != nil {
		t.Errorf("expected nil findings for a pending image, got %v", list.Vulnerabilities)
	}
	if list.DockerImage == nil {
		t.Fatal("expected the image to come back on the refusal")
	}
	if list.DockerImage.State != "pending" {
		t.Errorf("expected state pending, got %q", list.DockerImage.State)
	}
	if list.DockerImage.VulnerabilityCount != nil {
		t.Errorf("expected a null count on a non-scanned image, got %d", *list.DockerImage.VulnerabilityCount)
	}
}

// A scan that moves mid-request refuses on a later page too, and the partial
// set is discarded rather than reported as complete.
func TestClient_ListDockerImageVulnerabilities_RefusesMidPage(t *testing.T) {
	var calls int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"docker_image": map[string]interface{}{"id": "image-uuid", "state": "scanned", "countable": true},
				"vulnerabilities": []interface{}{
					map[string]interface{}{"id": 1, "package_name": "openssl", "severity": "Critical"},
				},
				"meta": map[string]int{
					"current_page": 1, "total_pages": 2, "total_count": 2, "per_page": 100,
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_image":    map[string]interface{}{"id": "image-uuid", "state": "pending", "countable": false},
			"vulnerabilities": nil,
			"meta":            nil,
		})
	})

	list, err := c.ListDockerImageVulnerabilities(context.Background(), "image-uuid", VulnerabilityListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Vulnerabilities != nil {
		t.Errorf("expected the partial page to be discarded, got %d findings", len(list.Vulnerabilities))
	}
	// The image reported must be the one the refusal describes, not the
	// "scanned" snapshot page 1 carried.
	if list.DockerImage == nil || list.DockerImage.State != "pending" {
		t.Errorf("expected the refusal's own image state, got %+v", list.DockerImage)
	}
}

// The plan gate answers 403, never an empty list.
func TestClient_ListVulnerabilities_Forbidden(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Vulnerability details (CVEs, scores, fix versions) require the Pro plan or above.",
		})
	})

	list, err := c.ListVulnerabilities(context.Background(), VulnerabilityListOptions{})
	if err == nil {
		t.Fatalf("expected an error, got %+v", list)
	}
	if !IsForbidden(err) {
		t.Errorf("expected IsForbidden to be true for %v", err)
	}
	if IsForbidden(fmt.Errorf("network down")) {
		t.Error("expected IsForbidden to be false for a non-API error")
	}
	if IsForbidden(&APIError{StatusCode: 404}) {
		t.Error("expected IsForbidden to be false for a 404")
	}
}

func TestClient_ListDockerImages_Forbidden(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "requires the Pro plan"})
	})

	if _, err := c.ListDockerImages(context.Background(), DockerImageListOptions{}); !IsForbidden(err) {
		t.Errorf("expected a 403 from the image index, got %v", err)
	}
}

// --- Container images ---

func TestClient_ListDockerImages(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// The ORG-WIDE index, not /instances/{id}/docker_images.
		if r.URL.Path != "/api/v1/docker_images" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_images": []interface{}{
				map[string]interface{}{
					"id": "image-uuid", "organization_id": 42,
					"image_id": "sha256:1f2e3d", "short_digest": "1f2e3d4c5b6a",
					"display_name": "nginx:1.27", "tags": []string{"nginx:1.27"},
					"repo_digests": []string{"nginx@sha256:1f2e3d"},
					"distro":       "debian:12", "ecosystem": "Debian:12",
					"state": "scanned", "countable": true,
					"vulnerability_count": 12, "critical_vulnerability_count": 3,
					"packages_truncated": true, "finding_count_is_floor": true,
					"last_seen_at": "2026-08-30T12:00:00Z", "running_host_count": 4,
				},
				map[string]interface{}{
					"id": "image-uuid-2", "state": "unscannable",
					"state_reason": "extraction failed", "state_error_type": "api_error",
					"countable": false, "vulnerability_count": nil,
					"critical_vulnerability_count": nil,
				},
			},
			"posture": map[string]int{"pending": 1, "scanned": 7, "unsupported": 2, "unscannable": 1},
			"meta": map[string]int{
				"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100,
			},
		})
	})

	truncated := true
	list, err := c.ListDockerImages(context.Background(), DockerImageListOptions{
		State:             "scanned",
		PackagesTruncated: &truncated,
		Query:             "nginx",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := gotQuery.Get("state"); got != "scanned" {
		t.Errorf("expected state 'scanned', got %q", got)
	}
	if got := gotQuery.Get("packages_truncated"); got != "true" {
		t.Errorf("expected packages_truncated 'true', got %q", got)
	}
	if got := gotQuery.Get("q"); got != "nginx" {
		t.Errorf("expected q 'nginx', got %q", got)
	}

	if len(list.Images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(list.Images))
	}
	scanned := list.Images[0]
	if scanned.VulnerabilityCount == nil || *scanned.VulnerabilityCount != 12 {
		t.Errorf("expected 12 vulnerabilities, got %v", scanned.VulnerabilityCount)
	}
	if !scanned.FindingCountIsFloor || !scanned.PackagesTruncated {
		t.Error("expected the truncated image's count to be marked a floor")
	}
	if scanned.RunningHostCount == nil || *scanned.RunningHostCount != 4 {
		t.Errorf("expected running_host_count 4, got %v", scanned.RunningHostCount)
	}

	// THE HONESTY CONTRACT: a non-scanned image carries no count at all.
	unscannable := list.Images[1]
	if unscannable.VulnerabilityCount != nil || unscannable.CriticalVulnerabilityCount != nil {
		t.Error("expected null counts on an unscannable image")
	}
	if unscannable.Countable {
		t.Error("expected countable false on an unscannable image")
	}

	if list.Posture.Pending != 1 || list.Posture.Scanned != 7 ||
		list.Posture.Unsupported != 2 || list.Posture.Unscannable != 1 {
		t.Errorf("unexpected posture: %+v", list.Posture)
	}
}

func TestClient_ListDockerImages_Pagination(t *testing.T) {
	var calls int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		page := requestedPage(r)
		// Posture DIFFERS per page here, which the real API never does — it is
		// how this test pins "taken once, from the first page" rather than
		// merely observing that every page happened to agree.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_images": []interface{}{
				map[string]interface{}{"id": fmt.Sprintf("image-%d", page), "state": "pending"},
			},
			"posture": map[string]int{"pending": 3 + page},
			"meta": map[string]int{
				"current_page": page, "total_pages": 3, "total_count": 3, "per_page": 100,
			},
		})
	})

	list, err := c.ListDockerImages(context.Background(), DockerImageListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 requests for total_pages=3, got %d", got)
	}
	if len(list.Images) != 3 {
		t.Fatalf("expected 3 images across 3 pages, got %d", len(list.Images))
	}
	// Posture is org-wide and identical on every page in production, so it is
	// taken once, from the FIRST page. The stub above varies it to prove that.
	if list.Posture.Pending != 4 {
		t.Errorf("expected page 1's posture.pending (4), got %d", list.Posture.Pending)
	}
}

// An unrecognised meta envelope must not truncate silently — morePages walks
// until an empty page rather than trusting counters it cannot read.
func TestClient_ListDockerImages_UnrecognisedMeta(t *testing.T) {
	var calls int32
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&calls, 1)
		images := []interface{}{}
		if page == 1 {
			images = append(images, map[string]interface{}{"id": "image-1", "state": "pending"})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_images": images,
			"posture":       map[string]int{"pending": 1},
			// The pre-2026-09 envelope: every field decodes to zero.
			"meta": map[string]int{"count": 1, "total": 1, "offset": 0},
		})
	})

	list, err := c.ListDockerImages(context.Background(), DockerImageListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(list.Images))
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected the walk to over-fetch by one empty page, got %d requests", got)
	}
}
