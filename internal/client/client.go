package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
func (c *Client) doRequest(method, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
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

	return resp, nil
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

func (c *Client) ListInstances() ([]Instance, error) {
	var all []Instance
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/instances?page=%d&per_page=100", page)
		resp, err := c.doRequest("GET", path, nil, nil)
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

func (c *Client) GetInstance(id string) (*Instance, string, error) {
	resp, err := c.doRequest("GET", "/api/v1/instances/"+id, nil, nil)
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

func (c *Client) CreateInstance(input CreateInstanceInput) (*Instance, error) {
	body := map[string]interface{}{"instance": input}
	resp, err := c.doRequest("POST", "/api/v1/instances", body, nil)
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

func (c *Client) UpdateInstance(id string, etag string, input UpdateInstanceInput) (*Instance, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"instance": input}
	resp, err := c.doRequest("PATCH", "/api/v1/instances/"+id, body, headers)
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

func (c *Client) DeleteInstance(id string) error {
	resp, err := c.doRequest("DELETE", "/api/v1/instances/"+id, nil, nil)
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

func (c *Client) EnableInstance(id string) error {
	return c.instanceAction(id, "enable")
}

func (c *Client) DisableInstance(id string) error {
	return c.instanceAction(id, "disable")
}

func (c *Client) EnterMaintenanceInstance(id string) error {
	return c.instanceAction(id, "enter_maintenance")
}

func (c *Client) ExitMaintenanceInstance(id string) error {
	return c.instanceAction(id, "exit_maintenance")
}

func (c *Client) instanceAction(id, action string) error {
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v1/instances/%s/%s", id, action), nil, nil)
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

func (c *Client) ListTasks() ([]Task, error) {
	var all []Task
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/tasks?page=%d&per_page=100", page)
		resp, err := c.doRequest("GET", path, nil, nil)
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

func (c *Client) GetTask(id string) (*Task, string, error) {
	resp, err := c.doRequest("GET", "/api/v1/tasks/"+id, nil, nil)
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

func (c *Client) CreateTask(input CreateTaskInput) (*Task, error) {
	body := map[string]interface{}{"task": input}
	resp, err := c.doRequest("POST", "/api/v1/tasks", body, nil)
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

func (c *Client) UpdateTask(id string, etag string, input UpdateTaskInput) (*Task, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"task": input}
	resp, err := c.doRequest("PATCH", "/api/v1/tasks/"+id, body, headers)
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

func (c *Client) DeleteTask(id string) error {
	resp, err := c.doRequest("DELETE", "/api/v1/tasks/"+id, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) PauseTask(id string) error {
	return c.taskAction(id, "pause")
}

func (c *Client) ResumeTask(id string) error {
	return c.taskAction(id, "resume")
}

func (c *Client) taskAction(id string, action string) error {
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v1/tasks/%s/%s", id, action), nil, nil)
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

func (c *Client) ListWorkflows() ([]Workflow, error) {
	var all []Workflow
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/workflows?page=%d&per_page=100", page)
		resp, err := c.doRequest("GET", path, nil, nil)
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
		all = append(all, result.Workflows...)
		if result.Meta.Count+result.Meta.Offset >= result.Meta.Total {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetWorkflow(id int64) (*Workflow, string, error) {
	resp, err := c.doRequest("GET", "/api/v1/workflows/"+strconv.FormatInt(id, 10), nil, nil)
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

func (c *Client) CreateWorkflow(input CreateWorkflowInput) (*Workflow, error) {
	body := map[string]interface{}{"workflow": input}
	resp, err := c.doRequest("POST", "/api/v1/workflows", body, nil)
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

func (c *Client) UpdateWorkflow(id int64, etag string, input UpdateWorkflowInput) (*Workflow, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"workflow": input}
	resp, err := c.doRequest("PATCH", "/api/v1/workflows/"+strconv.FormatInt(id, 10), body, headers)
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

func (c *Client) DeleteWorkflow(id int64) error {
	resp, err := c.doRequest("DELETE", "/api/v1/workflows/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) ActivateWorkflow(id int64) error {
	return c.workflowAction(id, "activate")
}

func (c *Client) PauseWorkflow(id int64) error {
	return c.workflowAction(id, "pause")
}

func (c *Client) workflowAction(id int64, action string) error {
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v1/workflows/%d/%s", id, action), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) CreateWorkflowVersion(workflowID int64, input CreateWorkflowVersionInput) (*WorkflowVersion, error) {
	body := input
	path := fmt.Sprintf("/api/v1/workflows/%d/versions", workflowID)
	resp, err := c.doRequest("POST", path, body, nil)
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

func (c *Client) PublishWorkflowVersion(workflowID int64, versionID int64) error {
	body := map[string]interface{}{"version_id": versionID}
	path := fmt.Sprintf("/api/v1/workflows/%d/publish", workflowID)
	resp, err := c.doRequest("POST", path, body, nil)
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

func (c *Client) ListUptimeMonitors() ([]UptimeMonitor, error) {
	var all []UptimeMonitor
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/uptime_monitors?page=%d&per_page=100", page)
		resp, err := c.doRequest("GET", path, nil, nil)
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

func (c *Client) GetUptimeMonitor(id string) (*UptimeMonitor, string, error) {
	resp, err := c.doRequest("GET", "/api/v1/uptime_monitors/"+id, nil, nil)
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

func (c *Client) CreateUptimeMonitor(input CreateUptimeMonitorInput) (*UptimeMonitor, error) {
	body := map[string]interface{}{"uptime_monitor": input}
	resp, err := c.doRequest("POST", "/api/v1/uptime_monitors", body, nil)
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

func (c *Client) UpdateUptimeMonitor(id string, etag string, input UpdateUptimeMonitorInput) (*UptimeMonitor, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"uptime_monitor": input}
	resp, err := c.doRequest("PATCH", "/api/v1/uptime_monitors/"+id, body, headers)
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

func (c *Client) DeleteUptimeMonitor(id string) error {
	resp, err := c.doRequest("DELETE", "/api/v1/uptime_monitors/"+id, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) PauseUptimeMonitor(id string) error {
	return c.uptimeMonitorAction(id, "pause")
}

func (c *Client) ResumeUptimeMonitor(id string) error {
	return c.uptimeMonitorAction(id, "resume")
}

func (c *Client) uptimeMonitorAction(id string, action string) error {
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v1/uptime_monitors/%s/%s", id, action), nil, nil)
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

func (c *Client) ListProbeRegions() ([]ProbeRegion, error) {
	resp, err := c.doRequest("GET", "/api/v1/probe_regions", nil, nil)
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

func (c *Client) ListIntegrations() ([]Integration, error) {
	resp, err := c.doRequest("GET", "/api/v1/integrations", nil, nil)
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

func (c *Client) GetIntegration(id int64) (*Integration, error) {
	resp, err := c.doRequest("GET", "/api/v1/integrations/"+strconv.FormatInt(id, 10), nil, nil)
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
