package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

const userAgent = "terraform-provider-fivenines/0.1.0"

// Client is the FiveNines API client.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewClient creates a new FiveNines API client.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doRequest executes an HTTP request and returns the response.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	// Retry on 429 Too Many Requests with exponential backoff
	for attempt := 0; resp.StatusCode == http.StatusTooManyRequests && attempt < 5; attempt++ {
		resp.Body.Close()

		// Parse Retry-After header (seconds) or use exponential backoff
		wait := time.Duration(math.Pow(2, float64(attempt+1))) * time.Second
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if secs, err := strconv.Atoi(retryAfter); err == nil {
				wait = time.Duration(secs) * time.Second
			}
		} else if resetAt := resp.Header.Get("X-RateLimit-Reset"); resetAt != "" {
			if resetTime, err := strconv.ParseInt(resetAt, 10, 64); err == nil {
				wait = time.Until(time.Unix(resetTime, 0))
				if wait < time.Second {
					wait = time.Second
				}
			}
		}

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		// Rebuild the request (body may have been consumed)
		var retryBody io.Reader
		if body != nil {
			jsonBody, _ := json.Marshal(body)
			retryBody = bytes.NewReader(jsonBody)
		}
		retryReq, err := http.NewRequestWithContext(ctx, method, url, retryBody)
		if err != nil {
			return nil, fmt.Errorf("creating retry request: %w", err)
		}
		retryReq.Header = req.Header
		resp, err = c.HTTPClient.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("executing retry request: %w", err)
		}
	}

	return resp, nil
}

// IsPreconditionFailed returns true if the error is a 412 Precondition Failed.
func IsPreconditionFailed(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == 412
	}
	return false
}

// parseError reads an error response body into an APIError.
func parseError(resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	apiErr := &APIError{StatusCode: resp.StatusCode}
	if err := json.Unmarshal(body, apiErr); err != nil {
		apiErr.Message = string(body)
	}
	return apiErr
}

// decodeResponse reads and decodes a JSON response body.
func decodeResponse(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

// listResponse is a generic list response envelope.
type listResponse struct {
	Meta PaginationMeta `json:"meta"`
}

// --- Instances ---

func (c *Client) ListInstances(ctx context.Context) ([]Instance, error) {
	var all []Instance
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/instances?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}

		var result struct {
			Instances []Instance     `json:"instances"`
			Meta      PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.Instances...)
		if result.Meta.Count+result.Meta.Offset >= result.Meta.Total {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetInstance(ctx context.Context, id string) (*Instance, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/instances/"+id, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}

	etag := resp.Header.Get("ETag")
	var result struct {
		Instance Instance `json:"instance"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.Instance, etag, nil
}

func (c *Client) CreateInstance(ctx context.Context, input CreateInstanceInput) (*Instance, error) {
	body := map[string]interface{}{"instance": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/instances", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}

	var result struct {
		Instance Instance `json:"instance"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Instance, nil
}

func (c *Client) UpdateInstance(ctx context.Context, id string, etag string, input UpdateInstanceInput) (*Instance, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"instance": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/instances/"+id, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Instance Instance `json:"instance"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Instance, nil
}

func (c *Client) DeleteInstance(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/instances/"+id, nil, nil)
	if err != nil {
		return err
	}
	// 202 Accepted (async) or 204 No Content
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) EnableInstance(ctx context.Context, id string) error {
	return c.instanceAction(ctx, id, "enable")
}

func (c *Client) DisableInstance(ctx context.Context, id string) error {
	return c.instanceAction(ctx, id, "disable")
}

func (c *Client) EnterMaintenanceInstance(ctx context.Context, id string) error {
	return c.instanceAction(ctx, id, "enter_maintenance")
}

func (c *Client) ExitMaintenanceInstance(ctx context.Context, id string) error {
	return c.instanceAction(ctx, id, "exit_maintenance")
}

func (c *Client) instanceAction(ctx context.Context, id, action string) error {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/instances/%s/%s", id, action), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Tasks ---

func (c *Client) ListTasks(ctx context.Context) ([]Task, error) {
	var all []Task
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/tasks?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}

		var result struct {
			Tasks []Task         `json:"tasks"`
			Meta  PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.Tasks...)
		if result.Meta.Count+result.Meta.Offset >= result.Meta.Total {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetTask(ctx context.Context, id string) (*Task, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/tasks/"+id, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}

	etag := resp.Header.Get("ETag")
	var result struct {
		Task Task `json:"task"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.Task, etag, nil
}

func (c *Client) CreateTask(ctx context.Context, input CreateTaskInput) (*Task, error) {
	body := map[string]interface{}{"task": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/tasks", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}

	var result struct {
		Task Task `json:"task"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Task, nil
}

func (c *Client) UpdateTask(ctx context.Context, id string, etag string, input UpdateTaskInput) (*Task, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"task": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/tasks/"+id, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Task Task `json:"task"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Task, nil
}

func (c *Client) DeleteTask(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/tasks/"+id, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) PauseTask(ctx context.Context, id string) error {
	return c.taskAction(ctx, id, "pause")
}

func (c *Client) ResumeTask(ctx context.Context, id string) error {
	return c.taskAction(ctx, id, "resume")
}

func (c *Client) taskAction(ctx context.Context, id string, action string) error {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/tasks/%s/%s", id, action), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Workflows ---

func (c *Client) ListWorkflows(ctx context.Context) ([]Workflow, error) {
	var all []Workflow
	page := 1
	for {
		// The API already excludes archived workflows, but filter client-side as well
		path := fmt.Sprintf("/api/v1/workflows?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}

		var result struct {
			Workflows []Workflow     `json:"workflows"`
			Meta      PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		for _, w := range result.Workflows {
			if w.Status != "archived" {
				all = append(all, w)
			}
		}
		if result.Meta.Count+result.Meta.Offset >= result.Meta.Total {
			break
		}
		page++
	}
	return all, nil
}

// --- Workflow Runs ---

func (c *Client) ListWorkflowRuns(ctx context.Context, workflowID int64) ([]WorkflowRun, error) {
	var all []WorkflowRun
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/workflows/%d/runs?page=%d&per_page=100", workflowID, page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}

		var result struct {
			Runs []WorkflowRun  `json:"runs"`
			Meta PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.Runs...)
		if result.Meta.Count+result.Meta.Offset >= result.Meta.Total {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetWorkflow(ctx context.Context, id int64) (*Workflow, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/workflows/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}

	etag := resp.Header.Get("ETag")
	var result struct {
		Workflow Workflow          `json:"workflow"`
		Versions []WorkflowVersion `json:"versions"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	result.Workflow.Versions = result.Versions
	return &result.Workflow, etag, nil
}

func (c *Client) CreateWorkflow(ctx context.Context, input CreateWorkflowInput) (*Workflow, error) {
	body := map[string]interface{}{"workflow": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/workflows", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}

	var result struct {
		Workflow Workflow `json:"workflow"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Workflow, nil
}

func (c *Client) UpdateWorkflow(ctx context.Context, id int64, etag string, input UpdateWorkflowInput) (*Workflow, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"workflow": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/workflows/"+strconv.FormatInt(id, 10), body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Workflow Workflow `json:"workflow"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Workflow, nil
}

func (c *Client) DeleteWorkflow(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/workflows/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) ActivateWorkflow(ctx context.Context, id int64) error {
	return c.workflowAction(ctx, id, "activate")
}

func (c *Client) PauseWorkflow(ctx context.Context, id int64) error {
	return c.workflowAction(ctx, id, "pause")
}

func (c *Client) workflowAction(ctx context.Context, id int64, action string) error {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/workflows/%d/%s", id, action), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) CreateWorkflowVersion(ctx context.Context, workflowID int64, input CreateWorkflowVersionInput) (*WorkflowVersion, error) {
	body := input
	path := fmt.Sprintf("/api/v1/workflows/%d/versions", workflowID)
	resp, err := c.doRequest(ctx, "POST", path, body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}

	var result struct {
		Version WorkflowVersion `json:"version"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Version, nil
}

func (c *Client) PublishWorkflowVersion(ctx context.Context, workflowID int64, versionID int64) error {
	body := map[string]interface{}{"version_id": versionID}
	path := fmt.Sprintf("/api/v1/workflows/%d/publish", workflowID)
	resp, err := c.doRequest(ctx, "POST", path, body, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Uptime Monitors ---

func (c *Client) ListUptimeMonitors(ctx context.Context) ([]UptimeMonitor, error) {
	var all []UptimeMonitor
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/uptime_monitors?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}

		var result struct {
			UptimeMonitors []UptimeMonitor `json:"uptime_monitors"`
			Meta           PaginationMeta  `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.UptimeMonitors...)
		if result.Meta.Count+result.Meta.Offset >= result.Meta.Total {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetUptimeMonitor(ctx context.Context, id string) (*UptimeMonitor, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/uptime_monitors/"+id, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}

	etag := resp.Header.Get("ETag")
	var result struct {
		UptimeMonitor UptimeMonitor `json:"uptime_monitor"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.UptimeMonitor, etag, nil
}

func (c *Client) CreateUptimeMonitor(ctx context.Context, input CreateUptimeMonitorInput) (*UptimeMonitor, error) {
	body := map[string]interface{}{"uptime_monitor": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/uptime_monitors", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}

	var result struct {
		UptimeMonitor UptimeMonitor `json:"uptime_monitor"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.UptimeMonitor, nil
}

func (c *Client) UpdateUptimeMonitor(ctx context.Context, id string, etag string, input UpdateUptimeMonitorInput) (*UptimeMonitor, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"uptime_monitor": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/uptime_monitors/"+id, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		UptimeMonitor UptimeMonitor `json:"uptime_monitor"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.UptimeMonitor, nil
}

func (c *Client) DeleteUptimeMonitor(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/uptime_monitors/"+id, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) PauseUptimeMonitor(ctx context.Context, id string) error {
	return c.uptimeMonitorAction(ctx, id, "pause")
}

func (c *Client) ResumeUptimeMonitor(ctx context.Context, id string) error {
	return c.uptimeMonitorAction(ctx, id, "resume")
}

func (c *Client) uptimeMonitorAction(ctx context.Context, id string, action string) error {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/uptime_monitors/%s/%s", id, action), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Probe Regions ---

func (c *Client) ListProbeRegions(ctx context.Context) ([]ProbeRegion, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/probe_regions", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		ProbeRegions []ProbeRegion `json:"probe_regions"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result.ProbeRegions, nil
}

// --- Integrations ---

func (c *Client) ListIntegrations(ctx context.Context) ([]Integration, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/integrations", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Integrations []Integration `json:"integrations"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result.Integrations, nil
}

// --- Incidents ---

func (c *Client) ListIncidents(ctx context.Context) ([]Incident, error) {
	var all []Incident
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/incidents?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}

		var result struct {
			Incidents []Incident     `json:"incidents"`
			Meta      PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.Incidents...)
		if result.Meta.Count+result.Meta.Offset >= result.Meta.Total {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetIncident(ctx context.Context, id int64) (*Incident, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/incidents/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Incident Incident `json:"incident"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Incident, nil
}

func (c *Client) GetIntegration(ctx context.Context, id int64) (*Integration, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/integrations/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Integration Integration `json:"integration"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Integration, nil
}
