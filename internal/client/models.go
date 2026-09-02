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

// --- Dashboards ---

// Dashboard is the full dashboard definition: its sections, its panels and the
// grid each panel sits in.
type Dashboard struct {
	ID                 int64              `json:"id"`
	Name               *string            `json:"name"`
	Description        *string            `json:"description"`
	Shared             bool               `json:"shared"`
	ShareURL           *string            `json:"share_url"`
	SectionCount       int64              `json:"section_count"`
	VisualizationCount int64              `json:"visualization_count"`
	Sections           []DashboardSection `json:"sections"`
	Visualizations     []Visualization    `json:"visualizations"`
	CreatedAt          string             `json:"created_at"`
	UpdatedAt          string             `json:"updated_at"`
}

// DashboardSection is a collapsible group of panels (a Grafana-style row).
type DashboardSection struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Position  int64  `json:"position"`
	Collapsed bool   `json:"collapsed"`
}

// Visualization is one panel on a dashboard, in the API's published shape:
// layout + targets + an allowlisted options object, with chart_type hoisted to
// the top level.
type Visualization struct {
	ID             int64                `json:"id"`
	Title          *string              `json:"title"`
	Description    *string              `json:"description"`
	Metric         string               `json:"metric"`
	TargetKind     string               `json:"target_kind"`
	ChartType      string               `json:"chart_type"`
	Section        *string              `json:"section"`
	Layout         VisualizationLayout  `json:"layout"`
	Targets        VisualizationTargets `json:"targets"`
	Options        VisualizationOptions `json:"options"`
	QueryResources []string             `json:"query_resources"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
}

// VisualizationLayout is a panel's grid geometry in 24-column gridstack space,
// relative to the panel's own section. Omitted fields fall back to the
// dashboard's own placement on create.
type VisualizationLayout struct {
	X *int64 `json:"x,omitempty"`
	Y *int64 `json:"y,omitempty"`
	W *int64 `json:"w,omitempty"`
	H *int64 `json:"h,omitempty"`
}

// VisualizationTargets is the read shape of a panel's entities. All five keys
// are always present; at most one is ever non-empty.
type VisualizationTargets struct {
	Hosts          []string `json:"hosts"`
	UptimeMonitors []string `json:"uptime_monitors"`
	Tasks          []string `json:"tasks"`
	NetworkDevices []string `json:"network_devices"`
	CephClusters   []string `json:"ceph_clusters"`
}

// VisualizationTargetsInput is the write shape of a panel's entities. The
// pointers are load-bearing: on PATCH a kind that is omitted is left alone and
// a kind that is sent is replaced, so clearing one requires sending an explicit
// empty array rather than dropping the key.
type VisualizationTargetsInput struct {
	Hosts          *[]string `json:"hosts,omitempty"`
	UptimeMonitors *[]string `json:"uptime_monitors,omitempty"`
	Tasks          *[]string `json:"tasks,omitempty"`
	NetworkDevices *[]string `json:"network_devices,omitempty"`
	CephClusters   *[]string `json:"ceph_clusters,omitempty"`
}

// VisualizationOptions is a panel's settings, on read and on write alike.
//
// None of these carry omitempty on purpose. The API's null contract says an
// option the panel does not store reads back as null and an explicit null on
// write clears it back to the metric's default, so a nil pointer must marshal
// to `null` rather than vanish - that is what makes the block declarative.
type VisualizationOptions struct {
	Reducer         *string  `json:"reducer"`
	GroupBy         *string  `json:"group_by"`
	Dimensions      []string `json:"dimensions"`
	Limit           *int64   `json:"limit"`
	Stacked         *bool    `json:"stacked"`
	IncidentOverlay *bool    `json:"incident_overlay"`
	Sparkline       *bool    `json:"sparkline"`
	Max             *float64 `json:"max"`
}

// CreateDashboardInput is the request body for creating a dashboard. Sections
// and panels are deliberately not included: they have their own endpoints, and
// the provider models them as their own resources.
type CreateDashboardInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

// UpdateDashboardInput is the request body for updating a dashboard. Only the
// dashboard's own fields - a body carrying sections or visualizations is
// refused with a 422.
type UpdateDashboardInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

// DashboardSectionInput is the request body for creating or updating a section.
// Position is the index the section should end up at, not a relative move.
type DashboardSectionInput struct {
	Name      string `json:"name,omitempty"`
	Collapsed *bool  `json:"collapsed,omitempty"`
	Position  *int64 `json:"position,omitempty"`
}

// VisualizationInput is the request body for creating or updating a panel.
// Metric is required on create only.
type VisualizationInput struct {
	Title       *string                    `json:"title"`
	Description *string                    `json:"description"`
	Metric      string                     `json:"metric,omitempty"`
	ChartType   string                     `json:"chart_type,omitempty"`
	Section     *string                    `json:"section"`
	Layout      *VisualizationLayout       `json:"layout,omitempty"`
	Targets     *VisualizationTargetsInput `json:"targets,omitempty"`
	Options     *VisualizationOptions      `json:"options,omitempty"`
}

// DashboardTemplate is one entry in the dashboard-template catalog.
type DashboardTemplate struct {
	Slug              string   `json:"slug"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Category          string   `json:"category"`
	Icon              string   `json:"icon"`
	TargetKinds       []string `json:"target_kinds"`
	PanelCount        int64    `json:"panel_count"`
	SectionCount      int64    `json:"section_count"`
	Available         bool     `json:"available"`
	UnavailableReason *string  `json:"unavailable_reason"`
}

// InstantiateDashboardTemplateInput is the request body for building a template.
type InstantiateDashboardTemplateInput struct {
	Slug        string `json:"slug"`
	Name        string `json:"name,omitempty"`
	DashboardID *int64 `json:"dashboard_id,omitempty"`
}

// DashboardTemplateResult is what a template instantiation actually built.
// Skipped is the honest half: a template declares panels for software the
// organization may not run, and those are dropped rather than created blank.
type DashboardTemplateResult struct {
	Dashboard    Dashboard               `json:"dashboard"`
	Section      *DashboardSection       `json:"section"`
	CreatedCount int64                   `json:"created_count"`
	Skipped      []DashboardTemplateSkip `json:"skipped"`
	SkipSummary  []string                `json:"skip_summary"`
}

// DashboardTemplateSkip is one panel a template instantiation did not create.
type DashboardTemplateSkip struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
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
