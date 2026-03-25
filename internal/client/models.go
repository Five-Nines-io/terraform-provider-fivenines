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
