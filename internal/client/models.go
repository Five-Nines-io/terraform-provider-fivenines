package client

import "fmt"

// PaginationMeta represents pagination metadata in list responses.
type PaginationMeta struct {
	Count  int `json:"count"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
}

// Instance represents a monitored server (Host).
type Instance struct {
	ID                  string  `json:"id"` // UUID
	DisplayName         string  `json:"display_name"`
	Hostname            string  `json:"hostname"`
	Enabled             bool    `json:"enabled"`
	MaintenanceMode     bool    `json:"maintenance_mode"`
	OperatingSystemName string  `json:"operating_system_name"`
	KernelVersion       string  `json:"kernel_version"`
	CPUArchitecture     string  `json:"cpu_architecture"`
	CPUModel            string  `json:"cpu_model"`
	CPUCount            int     `json:"cpu_count"`
	MemorySize          int64   `json:"memory_size"`
	IPv4                string  `json:"ipv4"`
	IPv6                string  `json:"ipv6"`
	Source              string  `json:"source"`
	ClientVersion       string  `json:"client_version"`
	Status              string  `json:"status"`
	LastSyncAt          *string `json:"last_sync_at"`
	FirstSyncAt         *string `json:"first_sync_at"`
	LastRequestAt       *string `json:"last_request_at"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

// CreateInstanceInput is the request body for creating an instance.
type CreateInstanceInput struct {
	DisplayName     string `json:"display_name"`
	Description     string `json:"description,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	MaintenanceMode *bool  `json:"maintenance_mode,omitempty"`
}

// UpdateInstanceInput is the request body for updating an instance.
type UpdateInstanceInput struct {
	DisplayName     *string `json:"display_name,omitempty"`
	Description     *string `json:"description,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	MaintenanceMode *bool   `json:"maintenance_mode,omitempty"`
}

// Task represents a cron/heartbeat monitor.
type Task struct {
	ID                 string  `json:"id"` // UUID
	Name               string  `json:"name"`
	ScheduleType       string  `json:"schedule_type"`
	Schedule           string  `json:"schedule"`
	IntervalSeconds    *int64  `json:"interval_seconds"`
	TimeZone           string  `json:"time_zone"`
	GracePeriodMinutes int     `json:"grace_period_minutes"`
	Status             string  `json:"status"`
	MonitoringStatus   string  `json:"monitoring_status"`
	PingKey            string  `json:"ping_key"`
	PingURL            string  `json:"ping_url"`
	HostID             *string `json:"host_id"`
	ExpectedPingAt     *string `json:"expected_ping_at"`
	LastPingAt         *string `json:"last_ping_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

// CreateTaskInput is the request body for creating a task.
type CreateTaskInput struct {
	Name               string `json:"name"`
	ScheduleType       string `json:"schedule_type"`
	Schedule           string `json:"schedule,omitempty"`
	IntervalSeconds    *int64 `json:"interval_seconds,omitempty"`
	GracePeriodMinutes *int   `json:"grace_period_minutes,omitempty"`
	TimeZone           string `json:"time_zone,omitempty"`
	HostID             string `json:"host_id,omitempty"`
}

// UpdateTaskInput is the request body for updating a task.
type UpdateTaskInput struct {
	Name               *string `json:"name,omitempty"`
	ScheduleType       *string `json:"schedule_type,omitempty"`
	Schedule           *string `json:"schedule,omitempty"`
	IntervalSeconds    *int64  `json:"interval_seconds,omitempty"`
	GracePeriodMinutes *int    `json:"grace_period_minutes,omitempty"`
	TimeZone           *string `json:"time_zone,omitempty"`
	HostID             *string `json:"host_id,omitempty"`
}

// Workflow represents an automation definition.
type Workflow struct {
	ID                 int64             `json:"id"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Status             string            `json:"status"`
	IntervalSeconds    *int64            `json:"interval_seconds"`
	TriggerType        string            `json:"trigger_type"`
	TriggerTypeLabel   string            `json:"trigger_type_label"`
	PublishedVersionID *int64            `json:"published_version_id"`
	NextEvaluationAt   *string           `json:"next_evaluation_at"`
	LastEvaluationAt   *string           `json:"last_evaluation_at"`
	CreatedAt          string            `json:"created_at"`
	UpdatedAt          string            `json:"updated_at"`
	Versions           []WorkflowVersion `json:"versions,omitempty"`
}

// WorkflowVersion represents a versioned snapshot of a workflow.
type WorkflowVersion struct {
	ID             int64                  `json:"id"`
	VersionNumber  int                    `json:"version_number"`
	ExecutionGraph map[string]interface{} `json:"execution_graph"`
	CreatedAt      string                 `json:"created_at"`
}

// CreateWorkflowInput is the request body for creating a workflow.
type CreateWorkflowInput struct {
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	IntervalSeconds *int64 `json:"interval_seconds,omitempty"`
}

// UpdateWorkflowInput is the request body for updating a workflow.
type UpdateWorkflowInput struct {
	Name            *string `json:"name,omitempty"`
	Description     *string `json:"description,omitempty"`
	IntervalSeconds *int64  `json:"interval_seconds,omitempty"`
}

// WorkflowRun represents a single execution of a workflow.
type WorkflowRun struct {
	ID          int64   `json:"id"`
	Status      string  `json:"status"`
	ResourceKey string  `json:"resource_key"`
	StartedAt   *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
}

// CreateWorkflowVersionInput is the request body for creating a workflow version.
type CreateWorkflowVersionInput struct {
	ExecutionGraph map[string]interface{} `json:"execution_graph"`
}

// UptimeMonitor represents an uptime monitoring check.
type UptimeMonitor struct {
	ID                  string  `json:"id"` // UUID
	Name                string  `json:"name"`
	Protocol            string  `json:"protocol"`
	Status              string  `json:"status"`
	URL                 string  `json:"url"`
	Hostname            string  `json:"hostname"`
	Port                *int    `json:"port"`
	HTTPMethod          string  `json:"http_method"`
	IPVersion           string  `json:"ip_version"`
	IntervalSeconds     int     `json:"interval_seconds"`
	TimeoutSeconds      int     `json:"timeout_seconds"`
	ConfirmationCount   int     `json:"confirmation_count"`
	Keyword             string  `json:"keyword"`
	KeywordAbsent       bool    `json:"keyword_absent"`
	FollowRedirects     bool    `json:"follow_redirects"`
	ExpectedStatusCodes []int   `json:"expected_status_codes"`
	ProbeRegionIDs      []int64 `json:"probe_region_ids"`
	// DNS protocol fields
	DNSRecordType      string   `json:"dns_record_type"`
	DNSExpectedRecords []string `json:"dns_expected_records"`
	// Custom HTTP fields
	CustomHeaders map[string]string `json:"custom_headers"`
	CustomBody    string            `json:"custom_body"`
	ContentType   string            `json:"content_type"`
	// Recovery
	RecoveryCount int `json:"recovery_count"`
	// Read-only
	SSLExpiresAt *string `json:"ssl_expires_at"`
	LastError    *string `json:"last_error"`
	NextCheckAt  *string `json:"next_check_at"`
	LastCheckAt  *string `json:"last_check_at"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// CreateUptimeMonitorInput is the request body for creating an uptime monitor.
type CreateUptimeMonitorInput struct {
	Name                string            `json:"name"`
	Protocol            string            `json:"protocol"`
	URL                 string            `json:"url,omitempty"`
	Hostname            string            `json:"hostname,omitempty"`
	Port                *int              `json:"port,omitempty"`
	HTTPMethod          string            `json:"http_method,omitempty"`
	IPVersion           string            `json:"ip_version,omitempty"`
	IntervalSeconds     *int              `json:"interval_seconds,omitempty"`
	TimeoutSeconds      *int              `json:"timeout_seconds,omitempty"`
	ConfirmationCount   *int              `json:"confirmation_count,omitempty"`
	Keyword             string            `json:"keyword,omitempty"`
	KeywordAbsent       *bool             `json:"keyword_absent,omitempty"`
	FollowRedirects     *bool             `json:"follow_redirects,omitempty"`
	ExpectedStatusCodes []int             `json:"expected_status_codes,omitempty"`
	ProbeRegionIDs      []int64           `json:"probe_region_ids,omitempty"`
	DNSRecordType       string            `json:"dns_record_type,omitempty"`
	DNSExpectedRecords  []string          `json:"dns_expected_records,omitempty"`
	CustomHeaders       map[string]string `json:"custom_headers,omitempty"`
	CustomBody          string            `json:"custom_body,omitempty"`
	ContentType         string            `json:"content_type,omitempty"`
	RecoveryCount       *int              `json:"recovery_count,omitempty"`
}

// UpdateUptimeMonitorInput is the request body for updating an uptime monitor.
type UpdateUptimeMonitorInput struct {
	Name                *string            `json:"name,omitempty"`
	URL                 *string            `json:"url,omitempty"`
	Hostname            *string            `json:"hostname,omitempty"`
	Port                *int               `json:"port,omitempty"`
	HTTPMethod          *string            `json:"http_method,omitempty"`
	IPVersion           *string            `json:"ip_version,omitempty"`
	IntervalSeconds     *int               `json:"interval_seconds,omitempty"`
	TimeoutSeconds      *int               `json:"timeout_seconds,omitempty"`
	ConfirmationCount   *int               `json:"confirmation_count,omitempty"`
	Keyword             *string            `json:"keyword,omitempty"`
	KeywordAbsent       *bool              `json:"keyword_absent,omitempty"`
	FollowRedirects     *bool              `json:"follow_redirects,omitempty"`
	ExpectedStatusCodes []int              `json:"expected_status_codes,omitempty"`
	ProbeRegionIDs      []int64            `json:"probe_region_ids,omitempty"`
	DNSRecordType       *string            `json:"dns_record_type,omitempty"`
	DNSExpectedRecords  []string           `json:"dns_expected_records,omitempty"`
	CustomHeaders       *map[string]string `json:"custom_headers,omitempty"`
	CustomBody          *string            `json:"custom_body,omitempty"`
	ContentType         *string            `json:"content_type,omitempty"`
	RecoveryCount       *int               `json:"recovery_count,omitempty"`
}

// ProbeRegion represents a monitoring probe region.
type ProbeRegion struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

// Integration represents a notification channel.
type Integration struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Enabled   bool   `json:"enabled"`
	Verified  bool   `json:"verified"`
	CreatedAt string `json:"created_at"`
}

// Incident represents an alert triggered by a workflow or manually.
type Incident struct {
	ID              int64   `json:"id"`
	Title           string  `json:"title"`
	Summary         string  `json:"summary"`
	Status          string  `json:"status"`
	HostID          *string `json:"host_id"`
	WorkflowID      *int64  `json:"workflow_id"`
	TaskID          *string `json:"task_id"`
	StartedAt       *string `json:"started_at"`
	EndedAt         *string `json:"ended_at"`
	DurationSeconds *int64  `json:"duration_seconds"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// NetworkDevice represents a monitored network device (SNMP).
type NetworkDevice struct {
	ID                string  `json:"id"` // UUID
	Name              string  `json:"name"`
	IPAddress         string  `json:"ip_address"`
	PollingHostID     *string `json:"polling_host_id"`
	DeviceType        string  `json:"device_type"`
	PollingInterval   int     `json:"polling_interval"`
	SNMPVersion       string  `json:"snmp_version"`
	SNMPUsername      string  `json:"snmp_username"`
	SNMPSecurityLevel string  `json:"snmp_security_level"`
	SNMPAuthProtocol  string  `json:"snmp_auth_protocol"`
	SNMPPrivProtocol  string  `json:"snmp_priv_protocol"`
	MaintenanceMode   bool    `json:"maintenance_mode"`
	Status            string  `json:"status"`
	Vendor            string  `json:"vendor"`
	Model             string  `json:"model"`
	SysName           string  `json:"sys_name"`
	LastPolledAt      *string `json:"last_polled_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

// CreateNetworkDeviceInput is the request body for creating a network device.
type CreateNetworkDeviceInput struct {
	Name              string  `json:"name"`
	IPAddress         string  `json:"ip_address"`
	PollingHostID     *string `json:"polling_host_id,omitempty"`
	DeviceType        string  `json:"device_type,omitempty"`
	PollingInterval   *int    `json:"polling_interval,omitempty"`
	SNMPVersion       string  `json:"snmp_version,omitempty"`
	SNMPCommunity     string  `json:"snmp_community,omitempty"`
	SNMPUsername      string  `json:"snmp_username,omitempty"`
	SNMPSecurityLevel string  `json:"snmp_security_level,omitempty"`
	SNMPAuthProtocol  string  `json:"snmp_auth_protocol,omitempty"`
	SNMPAuthPassword  string  `json:"snmp_auth_password,omitempty"`
	SNMPPrivProtocol  string  `json:"snmp_priv_protocol,omitempty"`
	SNMPPrivPassword  string  `json:"snmp_priv_password,omitempty"`
}

// UpdateNetworkDeviceInput is the request body for updating a network device.
type UpdateNetworkDeviceInput struct {
	Name              *string `json:"name,omitempty"`
	IPAddress         *string `json:"ip_address,omitempty"`
	PollingHostID     *string `json:"polling_host_id,omitempty"`
	DeviceType        *string `json:"device_type,omitempty"`
	PollingInterval   *int    `json:"polling_interval,omitempty"`
	SNMPVersion       *string `json:"snmp_version,omitempty"`
	SNMPCommunity     *string `json:"snmp_community,omitempty"`
	SNMPUsername      *string `json:"snmp_username,omitempty"`
	SNMPSecurityLevel *string `json:"snmp_security_level,omitempty"`
	SNMPAuthProtocol  *string `json:"snmp_auth_protocol,omitempty"`
	SNMPAuthPassword  *string `json:"snmp_auth_password,omitempty"`
	SNMPPrivProtocol  *string `json:"snmp_priv_protocol,omitempty"`
	SNMPPrivPassword  *string `json:"snmp_priv_password,omitempty"`
}

// StatusPage represents a public status page.
type StatusPage struct {
	ID                      int64            `json:"id"`
	Name                    string           `json:"name"`
	Slug                    string           `json:"slug"`
	Description             string           `json:"description"`
	Public                  bool             `json:"public"`
	Uptime                  bool             `json:"uptime"`
	CustomDomain            string           `json:"custom_domain"`
	CustomDomainEnabled     bool             `json:"custom_domain_enabled"`
	CustomFooter            string           `json:"custom_footer"`
	CustomFooterEnabled     bool             `json:"custom_footer_enabled"`
	IncidentsHistoryEnabled bool             `json:"incidents_history_enabled"`
	ThemeVariant            string           `json:"theme_variant"`
	Items                   []StatusPageItem `json:"items"`
	CreatedAt               string           `json:"created_at"`
	UpdatedAt               string           `json:"updated_at"`
}

// StatusPageItem represents an item on a status page.
type StatusPageItem struct {
	ItemType string `json:"item_type"`
	ItemID   string `json:"item_id"`
	Position int    `json:"position"`
}

// CreateStatusPageInput is the request body for creating a status page.
type CreateStatusPageInput struct {
	Name                    string           `json:"name"`
	Description             string           `json:"description,omitempty"`
	Public                  *bool            `json:"public,omitempty"`
	Uptime                  *bool            `json:"uptime,omitempty"`
	CustomDomain            string           `json:"custom_domain,omitempty"`
	CustomDomainEnabled     *bool            `json:"custom_domain_enabled,omitempty"`
	CustomFooter            string           `json:"custom_footer,omitempty"`
	CustomFooterEnabled     *bool            `json:"custom_footer_enabled,omitempty"`
	IncidentsHistoryEnabled *bool            `json:"incidents_history_enabled,omitempty"`
	ThemeVariant            string           `json:"theme_variant,omitempty"`
	Items                   []StatusPageItem `json:"items,omitempty"`
}

// UpdateStatusPageInput is the request body for updating a status page.
type UpdateStatusPageInput struct {
	Name                    *string          `json:"name,omitempty"`
	Description             *string          `json:"description,omitempty"`
	Public                  *bool            `json:"public,omitempty"`
	Uptime                  *bool            `json:"uptime,omitempty"`
	CustomDomain            *string          `json:"custom_domain,omitempty"`
	CustomDomainEnabled     *bool            `json:"custom_domain_enabled,omitempty"`
	CustomFooter            *string          `json:"custom_footer,omitempty"`
	CustomFooterEnabled     *bool            `json:"custom_footer_enabled,omitempty"`
	IncidentsHistoryEnabled *bool            `json:"incidents_history_enabled,omitempty"`
	ThemeVariant            *string          `json:"theme_variant,omitempty"`
	Items                   []StatusPageItem `json:"items,omitempty"`
}

// --- Security (vulnerabilities & container images) ---
//
// Every endpoint below is plan-gated behind `security_details` and answers 403
// on a plan without it. The gate is deliberate: an empty list is what a build
// gate reads as PASSED, so the API refuses rather than answering with one.

// FindingPageMeta is the pagination envelope the security indexes return.
// It is null on a response the API refused to answer - see VulnerabilityList.
//
// NOTE: this is the current API's pagination shape (current_page/total_pages),
// not PaginationMeta's count/offset - the older list methods still speak the
// stale one (tracked separately in #5).
type FindingPageMeta struct {
	CurrentPage int `json:"current_page"`
	TotalPages  int `json:"total_pages"`
	TotalCount  int `json:"total_count"`
	PerPage     int `json:"per_page"`
}

// Vulnerability is one OSV finding: an (advisory, package, subject) row.
// Exactly one subject pair is populated - HostID/HostName/Ecosystem on the
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
	// which is distinct from a CVE id - an advisory can alias several, or none.
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
	// Vulnerabilities is nil - not empty - when the API refused to answer,
	// which it does for an instance or image that was never scanned. An empty
	// non-nil slice is a real all-clear from a subject that WAS scanned; the
	// difference is the whole point of these endpoints and is carried through
	// to Terraform as a null list rather than an empty one.
	Vulnerabilities []Vulnerability
	// Scan is nil on the per-image drill-down, which reports its subject's
	// state through DockerImage instead.
	Scan *VulnerabilityScan
	// DockerImage is set only by ListDockerImageVulnerabilities, and always -
	// including on the refusal, where its State says why there are no findings.
	DockerImage *DockerImage
}

// VulnerabilityFilters narrows a findings index. Zero values are omitted from
// the query string: the API rejects an unknown or empty filter with a 400
// rather than silently answering an unfiltered list.
type VulnerabilityFilters struct {
	// Severity is sent comma-separated (severity=Critical,High) and matches
	// each FINDING's own score.
	Severity        []string
	Patchable       *bool
	FixState        string
	PackageName     string
	VulnerabilityID string
	// Ecosystem is accepted by the org-wide index only: the nested routes
	// already fix their subject's ecosystem and 400 on the parameter.
	Ecosystem string
	Query     string
}

// DockerImage is one container image in the organization, with its scan
// verdict. It is org-scoped: one image running on 50 hosts is one row.
type DockerImage struct {
	ID             string `json:"id"` // UUID
	OrganizationID int64  `json:"organization_id"`
	// ImageID is the sha256: config digest - the durable, content-addressed
	// identity the scan is keyed on. ID is the Fivenines row.
	ImageID     string `json:"image_id"`
	ShortDigest string `json:"short_digest"`
	// DisplayName is a tag if the image carries one, else the short digest -
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
	// scanned", never "zero vulnerabilities" - the underlying columns are NOT
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
	// container off this image.
	RunningHostCount *int64 `json:"running_host_count"`
}

// DockerImagePosture counts images per scan state across the WHOLE
// organization. It is never narrowed by the list filters - it is the answer to
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

// DockerImageFilters narrows the container-image index.
type DockerImageFilters struct {
	State             string
	Ecosystem         string
	PackagesTruncated *bool
	Query             string
}

// APIError represents an error response from the API.
type APIError struct {
	StatusCode int
	Message    string   `json:"error"`
	Errors     []string `json:"errors"`
}

func (e *APIError) Error() string {
	if len(e.Errors) > 0 {
		return fmt.Sprintf("API error %d: %v", e.StatusCode, e.Errors)
	}
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}
