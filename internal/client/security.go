package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The organization's security surfaces: OSV vulnerability findings and the
// container-image inventory they hang off.
//
// Every endpoint here is plan-gated behind `security_details` and answers 403
// on a plan without it. The gate is deliberate and so is its shape: an empty
// list is what a build gate reads as PASSED, so the API refuses rather than
// answering with one. IsForbidden is the predicate for that case.

// securityPerPage is the maximum per_page these indexes accept. A data source
// reads them in full, so the fewest round trips wins.
const securityPerPage = 100

// Vulnerability is one OSV finding: an (advisory, package, subject) row.
// Exactly one subject pair is populated — HostID/HostName/Ecosystem on the
// instance indexes, DockerImageID/ImageName on the container-image one.
type Vulnerability struct {
	// ID is NOT a stable per-CVE identity: a scan rewrites a subject's findings
	// wholesale, so it is minted fresh on every rewrite. Reconcile across scans
	// on (subject, PackageName, VulnerabilityID).
	ID               int64   `json:"id"`
	HostID           *string `json:"host_id"`
	HostName         *string `json:"host_name"`
	Ecosystem        *string `json:"ecosystem"`
	DockerImageID    *string `json:"docker_image_id"`
	ImageName        *string `json:"image_name"`
	PackageName      string  `json:"package_name"`
	InstalledVersion string  `json:"installed_version"`
	// VulnerabilityID is the OSV advisory id (UBUNTU-CVE-2024-2511, DSA-1234-1),
	// which is distinct from a CVE id — an advisory can alias several, or none.
	VulnerabilityID string   `json:"vulnerability_id"`
	CVEIDs          []string `json:"cve_ids"`
	Summary         *string  `json:"summary"`
	AdvisoryURL     *string  `json:"advisory_url"`
	// CVSSScore is null when no usable score was recorded, which reads as
	// Severity "Unknown". A null is not a zero.
	CVSSScore *float64 `json:"cvss_score"`
	// Severity bands CVSSScore: Critical >= 9, High >= 7, Medium >= 4, Low is
	// everything else that HAS a score, Unknown is a missing one.
	Severity string `json:"severity"`
	// Patchable is the work-queue flag: a fix exists AND this subject can
	// install it. Strictly narrower than FixState == "fixed", which also covers
	// fixes gated behind a paid channel (see RequiresSubscription).
	Patchable            bool    `json:"patchable"`
	FixVersion           *string `json:"fix_version"`
	FixState             string  `json:"fix_state"`
	RequiresSubscription bool    `json:"requires_subscription"`
	Vendor               *string `json:"vendor"`
	VendorNote           *string `json:"vendor_note"`
	// DetectedAt is when the scan that WROTE this row ran, not when the CVE was
	// published and not a durable "first seen".
	DetectedAt *string `json:"detected_at"`
	UpdatedAt  *string `json:"updated_at"`
}

// VulnerabilityScan says what an empty findings list means. The org-wide index
// fills the fleet pair (OldestCheckedAt, InstancesNeverChecked); the
// per-instance index fills the host pair (LastCheckedAt, NeverChecked).
type VulnerabilityScan struct {
	OldestCheckedAt       *string `json:"oldest_checked_at"`
	InstancesNeverChecked *int64  `json:"instances_never_checked"`
	LastCheckedAt         *string `json:"last_checked_at"`
	NeverChecked          *bool   `json:"never_checked"`
}

// VulnerabilityList is one findings query with the context that qualifies it.
type VulnerabilityList struct {
	// Vulnerabilities is nil — not empty — when the API refused to answer,
	// which it does for an instance or image that was never scanned. An empty
	// non-nil slice is a real all-clear from a subject that WAS scanned; the
	// difference is the whole point of these endpoints and is carried through
	// to Terraform as a null list rather than an empty one.
	Vulnerabilities []Vulnerability
	// Scan is nil on the per-image drill-down, which reports its subject's
	// state through DockerImage instead.
	Scan *VulnerabilityScan
	// DockerImage is set only by ListDockerImageVulnerabilities, and always —
	// including on the refusal, where its State says why there are no findings.
	DockerImage *DockerImage
}

// VulnerabilityListOptions narrows a findings index. An empty value stays out
// of the query string entirely: the API rejects an unknown or empty filter with
// a 400 rather than silently answering an unfiltered list.
type VulnerabilityListOptions struct {
	// Severity is sent comma-separated (severity=Critical,High) and matches
	// each FINDING's own score, not its package's worst.
	Severity  []string
	Patchable *bool
	FixState  string
	// PackageName is exact. A gate scoped to openssl must not widen to
	// openssl-dev, so there is no substring form here — that is Query.
	PackageName     string
	VulnerabilityID string
	// Ecosystem is accepted by the org-wide index only: the nested routes
	// already fix their subject's ecosystem and 400 on the parameter.
	Ecosystem    string
	Query        string
	UpdatedSince string
	Order        string
	Direction    string
}

func (o VulnerabilityListOptions) query() map[string]string {
	q := map[string]string{
		"fix_state":        o.FixState,
		"package_name":     o.PackageName,
		"vulnerability_id": o.VulnerabilityID,
		"ecosystem":        o.Ecosystem,
		"q":                o.Query,
		"updated_since":    o.UpdatedSince,
		"order":            o.Order,
		"direction":        o.Direction,
		"severity":         strings.Join(o.Severity, ","),
	}
	if o.Patchable != nil {
		q["patchable"] = strconv.FormatBool(*o.Patchable)
	}
	return q
}

// DockerImage is one container image in the organization, with its scan
// verdict. It is org-scoped: one image running on 50 hosts is one row.
type DockerImage struct {
	ID             string `json:"id"` // UUID
	OrganizationID int64  `json:"organization_id"`
	// ImageID is the sha256: config digest — the durable, content-addressed
	// identity the scan is keyed on. ID is the FiveNines row.
	ImageID     string `json:"image_id"`
	ShortDigest string `json:"short_digest"`
	// DisplayName is a tag if the image carries one, else the short digest —
	// a label, not an id.
	DisplayName string   `json:"display_name"`
	Tags        []string `json:"tags"`
	RepoDigests []string `json:"repo_digests"`
	Distro      *string  `json:"distro"`
	Ecosystem   *string  `json:"ecosystem"`
	// State is pending | scanned | unsupported | unscannable, and is the field
	// to read BEFORE the counts.
	State          string  `json:"state"`
	StateReason    *string `json:"state_reason"`
	StateErrorType *string `json:"state_error_type"`
	// Countable is whether the counts below mean anything at all: false for
	// every non-scanned state.
	Countable bool `json:"countable"`
	// VulnerabilityCount is null unless the image is scanned. A null is "not
	// scanned", never "zero vulnerabilities" — the underlying columns are NOT
	// NULL default 0, which is exactly the reading this nulling prevents.
	VulnerabilityCount         *int64 `json:"vulnerability_count"`
	CriticalVulnerabilityCount *int64 `json:"critical_vulnerability_count"`
	// PackagesTruncated marks an image whose package list the agent capped, so
	// the counts are a FLOOR rather than a total. Not a fifth state: the scan
	// really did complete. FindingCountIsFloor is the two conditions together.
	PackagesTruncated   bool    `json:"packages_truncated"`
	FindingCountIsFloor bool    `json:"finding_count_is_floor"`
	LastSeenAt          *string `json:"last_seen_at"`
	InventoryReceivedAt *string `json:"inventory_received_at"`
	LastScannedAt       *string `json:"last_scanned_at"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
	// RunningHostCount is the blast radius: instances currently running a
	// container off this image. Only the org-wide shapes carry it.
	RunningHostCount *int64 `json:"running_host_count"`
}

// DockerImagePosture counts images per scan state across the WHOLE
// organization. It is never narrowed by the list filters — it is the answer to
// "is this list the complete picture", which a filtered count could not be.
type DockerImagePosture struct {
	Pending     int64 `json:"pending"`
	Scanned     int64 `json:"scanned"`
	Unsupported int64 `json:"unsupported"`
	Unscannable int64 `json:"unscannable"`
}

// DockerImageList is the image inventory plus the posture that qualifies it.
type DockerImageList struct {
	Images  []DockerImage
	Posture DockerImagePosture
}

// DockerImageListOptions narrows the org-wide container-image index.
type DockerImageListOptions struct {
	State             string
	Ecosystem         string
	PackagesTruncated *bool
	Query             string
	UpdatedSince      string
	Order             string
	Direction         string
}

func (o DockerImageListOptions) query() map[string]string {
	q := map[string]string{
		"state":         o.State,
		"ecosystem":     o.Ecosystem,
		"q":             o.Query,
		"updated_since": o.UpdatedSince,
		"order":         o.Order,
		"direction":     o.Direction,
	}
	if o.PackagesTruncated != nil {
		q["packages_truncated"] = strconv.FormatBool(*o.PackagesTruncated)
	}
	return q
}

// ListVulnerabilities returns every OSV finding across the organization.
func (c *Client) ListVulnerabilities(ctx context.Context, opts VulnerabilityListOptions) (*VulnerabilityList, error) {
	return c.listFindings(ctx, "/api/v1/vulnerabilities", opts.query())
}

// ListInstanceVulnerabilities returns one instance's findings. An instance that
// has never been scanned answers with a nil Vulnerabilities slice — see
// VulnerabilityList.
func (c *Client) ListInstanceVulnerabilities(ctx context.Context, instanceID string, opts VulnerabilityListOptions) (*VulnerabilityList, error) {
	// Ecosystem is an org-wide-only filter; the nested route 400s on it.
	opts.Ecosystem = ""
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/vulnerabilities"
	return c.listFindings(ctx, path, opts.query())
}

// ListDockerImageVulnerabilities returns one container image's findings. An
// image whose scan did not complete answers with a nil Vulnerabilities slice
// and a DockerImage whose State says why.
func (c *Client) ListDockerImageVulnerabilities(ctx context.Context, imageID string, opts VulnerabilityListOptions) (*VulnerabilityList, error) {
	opts.Ecosystem = ""
	path := "/api/v1/docker_images/" + url.PathEscape(imageID) + "/vulnerabilities"
	return c.listFindings(ctx, path, opts.query())
}

// ListDockerImages returns the organization's container image inventory,
// including the images that could not be scanned — they are the point, so they
// are never filtered out.
//
// Distinct from ListInventory(instanceID, "docker_images", ...), which returns
// the slice ONE host runs. A DockerImage is organization-owned, so this is the
// list whose ids feed ListDockerImageVulnerabilities.
func (c *Client) ListDockerImages(ctx context.Context, opts DockerImageListOptions) (*DockerImageList, error) {
	result := &DockerImageList{Images: []DockerImage{}}
	filters := opts.query()

	for page := 1; ; page++ {
		var body struct {
			DockerImages []DockerImage      `json:"docker_images"`
			Posture      DockerImagePosture `json:"posture"`
			Meta         PaginationMeta     `json:"meta"`
		}
		if err := c.getSecurityPage(ctx, "/api/v1/docker_images", filters, page, &body); err != nil {
			return nil, err
		}

		// Posture is org-wide and identical on every page.
		if page == 1 {
			result.Posture = body.Posture
		}
		result.Images = append(result.Images, body.DockerImages...)

		more, err := morePages(len(body.DockerImages), body.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			return result, nil
		}
	}
}

// listFindings walks every page of one findings index.
func (c *Client) listFindings(ctx context.Context, path string, filters map[string]string) (*VulnerabilityList, error) {
	// Non-nil: a scanned subject with nothing wrong must read as an empty list
	// and not as the refusal below.
	result := &VulnerabilityList{Vulnerabilities: []Vulnerability{}}

	for page := 1; ; page++ {
		var body struct {
			Vulnerabilities []Vulnerability    `json:"vulnerabilities"`
			Scan            *VulnerabilityScan `json:"scan"`
			DockerImage     *DockerImage       `json:"docker_image"`
			// A POINTER, unlike every other index in this client: `meta: null`
			// is a meaningful answer here, not a missing envelope, and
			// morePages cannot distinguish the two from a value.
			Meta *PaginationMeta `json:"meta"`
		}
		if err := c.getSecurityPage(ctx, path, filters, page, &body); err != nil {
			return nil, err
		}

		if page == 1 {
			result.Scan = body.Scan
			result.DockerImage = body.DockerImage
		}

		// THE REFUSAL. A subject that was never scanned — and an image whose
		// scan moved while the page was being built — answers `vulnerabilities:
		// null` beside `meta: null` rather than an empty page, because an empty
		// page is what a build gate reads as PASSED for something nothing ever
		// looked at. Discard whatever earlier pages held: a partial set reported
		// as complete is the same lie in the other direction.
		if body.Meta == nil {
			result.Vulnerabilities = nil
			// Take the refusal's own subject, not the one page 1 described: an
			// image whose scan moved mid-request comes back with its NEW state,
			// and State is the field a caller branches on. Reporting "scanned"
			// beside a null list would be the contradiction this whole protocol
			// exists to prevent.
			if body.Scan != nil {
				result.Scan = body.Scan
			}
			if body.DockerImage != nil {
				result.DockerImage = body.DockerImage
			}
			return result, nil
		}

		result.Vulnerabilities = append(result.Vulnerabilities, body.Vulnerabilities...)

		more, err := morePages(len(body.Vulnerabilities), *body.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			return result, nil
		}
	}
}

// getSecurityPage issues one page of a security index and decodes it.
func (c *Client) getSecurityPage(ctx context.Context, path string, filters map[string]string, page int, target interface{}) error {
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(securityPerPage))
	for key, value := range filters {
		if value != "" {
			query.Set(key, value)
		}
	}

	resp, err := c.doRequest(ctx, "GET", path+"?"+query.Encode(), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	if err := decodeResponse(resp, target); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}
