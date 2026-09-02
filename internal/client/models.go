package client

import "fmt"

// PaginationMeta represents pagination metadata in list responses.
//
// These are the current field names. The envelope used to be count/total/offset;
// when it changed, the old struct decoded to all zeros and the `count+offset >=
// total` exit condition read `0 >= 0`, so every list in the provider silently
// stopped after one page. See morePages for the guard that keeps a future rename
// from truncating instead of erroring.
type PaginationMeta struct {
	CurrentPage int `json:"current_page"`
	TotalPages  int `json:"total_pages"`
	TotalCount  int `json:"total_count"`
	PerPage     int `json:"per_page"`
}

// Instance represents a monitored server (Host).
//
// Fields the API documents as nullable are pointers so a null round-trips to
// types.StringNull()/types.Int64Null() instead of collapsing to "" or 0.
type Instance struct {
	ID                  string  `json:"id"` // UUID
	DisplayName         string  `json:"display_name"`
	Hostname            *string `json:"hostname"`
	Enabled             bool    `json:"enabled"`
	MaintenanceMode     bool    `json:"maintenance_mode"`
	OperatingSystemName *string `json:"operating_system_name"`
	KernelVersion       *string `json:"kernel_version"`
	CPUArchitecture     *string `json:"cpu_architecture"`
	CPUModel            *string `json:"cpu_model"`
	CPUCount            *int    `json:"cpu_count"`
	MemorySize          *int64  `json:"memory_size"`
	IPv4                *string `json:"ipv4"`
	IPv6                *string `json:"ipv6"`
	Source              *string `json:"source"`
	ClientVersion       *string `json:"client_version"`
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
	Schedule           *string `json:"schedule"`
	IntervalSeconds    *int64  `json:"interval_seconds"`
	TimeZone           *string `json:"time_zone"`
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
//
// host_id carries NO omitempty, following the same convention as the
// protocol-scoped uptime monitor fields: it is Optional-only, so the provider
// owns it end to end and a nil pointer marshals as an explicit null that clears
// the association. schedule/interval_seconds are Optional+Computed — the API
// keeps the counterpart it already stored across a schedule_type switch (#8) —
// so they keep omitempty and are omitted when unset.
type UpdateTaskInput struct {
	Name               *string `json:"name,omitempty"`
	ScheduleType       *string `json:"schedule_type,omitempty"`
	Schedule           *string `json:"schedule,omitempty"`
	IntervalSeconds    *int64  `json:"interval_seconds,omitempty"`
	GracePeriodMinutes *int    `json:"grace_period_minutes,omitempty"`
	TimeZone           *string `json:"time_zone,omitempty"`
	HostID             *string `json:"host_id"`
}

// Workflow represents an automation definition.
type Workflow struct {
	ID                 int64             `json:"id"`
	Name               string            `json:"name"`
	Description        *string           `json:"description"`
	Status             string            `json:"status"`
	IntervalSeconds    *int64            `json:"interval_seconds"`
	TriggerType        *string           `json:"trigger_type"`
	TriggerTypeLabel   *string           `json:"trigger_type_label"`
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

// StatusPaused is the status both tasks and uptime monitors report while their
// checks are suspended. Uptime monitors report one of: unknown, up, down,
// paused, recovering.
const StatusPaused = "paused"

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
//
// The protocol-scoped fields below deliberately carry NO omitempty: they are
// always serialised, and a nil pointer marshals as an explicit JSON null, which
// the API reads as "clear this". That is what makes `protocol` updatable in
// place — switching an https monitor to tcp has to actively clear `keyword` and
// `content_type`, or the server keeps echoing values the Terraform plan says are
// null and every apply fails with "Provider produced inconsistent result after
// apply". Terraform resolves Optional-only attributes before calling Update, so
// the plan value is always known by the time it reaches this struct.
//
// The remaining fields keep omitempty: they are Computed or defaulted, so the
// plan always carries a concrete value and there is nothing to clear.
type UpdateUptimeMonitorInput struct {
	Name                *string `json:"name,omitempty"`
	Protocol            *string `json:"protocol,omitempty"`
	URL                 *string `json:"url,omitempty"`
	Hostname            *string `json:"hostname,omitempty"`
	HTTPMethod          *string `json:"http_method,omitempty"`
	IPVersion           *string `json:"ip_version,omitempty"`
	IntervalSeconds     *int    `json:"interval_seconds,omitempty"`
	TimeoutSeconds      *int    `json:"timeout_seconds,omitempty"`
	ConfirmationCount   *int    `json:"confirmation_count,omitempty"`
	KeywordAbsent       *bool   `json:"keyword_absent,omitempty"`
	FollowRedirects     *bool   `json:"follow_redirects,omitempty"`
	ExpectedStatusCodes []int   `json:"expected_status_codes,omitempty"`
	RecoveryCount       *int    `json:"recovery_count,omitempty"`

	// Explicitly clearable for the same reason as the protocol-scoped fields
	// below: `probe_region_ids = []` is a legal config with no validator to
	// shield it, and omitting the key would leave the server's old region set in
	// place while the plan holds a known [].
	ProbeRegionIDs *[]int64 `json:"probe_region_ids,omitempty"`

	// Protocol-scoped: nil marshals as null and clears the stored value.
	Port               *int               `json:"port"`
	Keyword            *string            `json:"keyword"`
	DNSRecordType      *string            `json:"dns_record_type"`
	DNSExpectedRecords *[]string          `json:"dns_expected_records"`
	CustomHeaders      *map[string]string `json:"custom_headers"`
	CustomBody         *string            `json:"custom_body"`
	ContentType        *string            `json:"content_type"`
}

// UptimeMonitorStatus is the lightweight payload returned by
// GET /api/v1/uptime_monitors/{id}/status. It is intended as a cheap polling
// target: it carries the liveness fields only, not the monitor configuration.
//
// Every optional field is a pointer so that a key the API omits decodes to nil
// and surfaces as null rather than as a misleading zero value.
type UptimeMonitorStatus struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	LastCheckAt  *string `json:"last_check_at"`
	NextCheckAt  *string `json:"next_check_at"`
	LastError    *string `json:"last_error"`
	SSLExpiresAt *string `json:"ssl_expires_at"`
}

// ListUptimeMonitorsOptions holds the index filters accepted by
// GET /api/v1/uptime_monitors. Zero-valued fields are not sent.
type ListUptimeMonitorsOptions struct {
	// Status filters by current status: unknown, up, down, paused or recovering.
	Status string
	// Protocol filters by protocol: https, tcp, icmp or dns.
	Protocol string
	// Query is a free-text search over name, url and hostname (the "q" param).
	Query string
	// UpdatedSince returns only monitors updated at or after this ISO8601 timestamp.
	UpdatedSince string
	// Order is the column to sort by, Direction is "asc" or "desc".
	Order     string
	Direction string
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
	DeviceType        *string `json:"device_type"`
	PollingInterval   int     `json:"polling_interval"`
	SNMPVersion       *string `json:"snmp_version"`
	SNMPUsername      *string `json:"snmp_username"`
	SNMPSecurityLevel *string `json:"snmp_security_level"`
	SNMPAuthProtocol  *string `json:"snmp_auth_protocol"`
	SNMPPrivProtocol  *string `json:"snmp_priv_protocol"`
	MaintenanceMode   bool    `json:"maintenance_mode"`
	Status            *string `json:"status"`
	Vendor            *string `json:"vendor"`
	Model             *string `json:"model"`
	SysName           *string `json:"sys_name"`
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
//
// polling_host_id and snmp_username carry NO omitempty: they are Optional-only,
// so a nil pointer marshals as an explicit null and clears the stored value.
//
// The SNMP credentials keep omitempty on purpose. They are write-only — the API
// never returns them and treats a blank value as "keep what is stored" — so an
// unset plan value must omit the key. Sending null there would wipe a working
// credential on every unrelated update.
type UpdateNetworkDeviceInput struct {
	Name              *string `json:"name,omitempty"`
	IPAddress         *string `json:"ip_address,omitempty"`
	PollingHostID     *string `json:"polling_host_id"`
	DeviceType        *string `json:"device_type,omitempty"`
	PollingInterval   *int    `json:"polling_interval,omitempty"`
	SNMPVersion       *string `json:"snmp_version,omitempty"`
	SNMPCommunity     *string `json:"snmp_community,omitempty"`
	SNMPUsername      *string `json:"snmp_username"`
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
	Description             *string          `json:"description"`
	Public                  bool             `json:"public"`
	Uptime                  bool             `json:"uptime"`
	CustomDomain            *string          `json:"custom_domain"`
	CustomDomainEnabled     bool             `json:"custom_domain_enabled"`
	CustomFooter            *string          `json:"custom_footer"`
	CustomFooterEnabled     bool             `json:"custom_footer_enabled"`
	IncidentsHistoryEnabled bool             `json:"incidents_history_enabled"`
	ThemeVariant            *string          `json:"theme_variant"`
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
//
// Items is a pointer to a slice, not a plain slice: a nil pointer omits the key
// while a pointer to an empty slice marshals as an explicit []. Emptying a page
// needs that [] — a plain `[]StatusPageItem` with omitempty marshals an empty
// list to nothing, which the API reads as "preserve the current items".
type UpdateStatusPageInput struct {
	Name                    *string           `json:"name,omitempty"`
	Description             *string           `json:"description,omitempty"`
	Public                  *bool             `json:"public,omitempty"`
	Uptime                  *bool             `json:"uptime,omitempty"`
	CustomDomain            *string           `json:"custom_domain,omitempty"`
	CustomDomainEnabled     *bool             `json:"custom_domain_enabled,omitempty"`
	CustomFooter            *string           `json:"custom_footer,omitempty"`
	CustomFooterEnabled     *bool             `json:"custom_footer_enabled,omitempty"`
	IncidentsHistoryEnabled *bool             `json:"incidents_history_enabled,omitempty"`
	ThemeVariant            *string           `json:"theme_variant,omitempty"`
	Items                   *[]StatusPageItem `json:"items,omitempty"`
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
