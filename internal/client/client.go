package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	if isDryRun(ctx) {
		req.Header.Set("X-Dry-Run", dryRunHeaderValue)
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

// dryRunHeaderValue is the token sent for X-Dry-Run. The server parses the
// header as a BOOLEAN rather than testing for presence, and accepts only
// true/1/t/yes/y/on (or their false counterparts) — anything else is a 400
// with code invalid_dry_run_header rather than a guess.
const dryRunHeaderValue = "true"

// dryRunKey is the context key carrying the dry-run opt-in. Unexported and of a
// distinct type so no other package can collide with it.
type dryRunKey struct{}

// WithDryRun returns a context that makes every request built from it send
// X-Dry-Run: true. The server runs the action inside a transaction and rolls it
// back, returning the JSON the real write would have produced — so a caller can
// validate a risky write (org members, workflow graphs) without performing it.
//
// It is context-scoped rather than a field on Client because a dry run is a
// property of one call, not of the client: a Client with dry-run latched on
// would silently discard every write made through it.
func WithDryRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, dryRunKey{}, true)
}

// isDryRun reports whether ctx was marked by WithDryRun.
func isDryRun(ctx context.Context) bool {
	v, _ := ctx.Value(dryRunKey{}).(bool)
	return v
}

// sanitizeETag strips the "-gzip" suffix that Nginx/reverse proxies append
// to strong ETags when gzip-compressing the response body.
func sanitizeETag(etag string) string {
	return strings.Replace(etag, "-gzip\"", "\"", 1)
}

// IsPreconditionFailed returns true if the error is a 412 Precondition Failed:
// an If-Match ETag no longer matches because the resource changed underneath us.
func IsPreconditionFailed(err error) bool {
	apiErr := AsAPIError(err)
	return apiErr != nil && apiErr.StatusCode == http.StatusPreconditionFailed
}

// parseError reads an error response body into an APIError.
func parseError(resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	apiErr := &APIError{StatusCode: resp.StatusCode}
	if err := json.Unmarshal(body, apiErr); err != nil {
		apiErr.Message = strings.TrimSpace(string(body))
	}

	// Not every error path renders request_id INTO the body: a 422 carries only
	// its `errors` array, and the authentication 401s only their message. The
	// X-Request-Id header is set on every response, so prefer the body (which
	// survives a proxy that strips headers) and fall back to the header.
	if apiErr.RequestID == "" {
		apiErr.RequestID = resp.Header.Get("X-Request-Id")
	}
	// A body keyed differently ({"message": ...}, {"detail": ...}) unmarshals
	// without error and leaves both fields empty, which renders as a diagnostic
	// with no reason at all. Fall back to the raw body.
	if apiErr.Message == "" && len(apiErr.Errors) == 0 {
		apiErr.Message = strings.TrimSpace(string(body))
	}
	return apiErr
}

// errMalformedBody marks a response body the client could not parse. It is a
// permanent failure — the same bytes will not parse on a retry — as opposed to
// a read that died in transit, which is worth another attempt.
var errMalformedBody = errors.New("malformed response body")

// decodeResponse reads and decodes a JSON response body.
func decodeResponse(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()
	err := json.NewDecoder(resp.Body).Decode(target)
	if err == nil {
		return nil
	}

	// Only a syntactically bad body is permanent. An unexpected EOF or a reset
	// mid-body is the transport failing, not the server sending garbage.
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return fmt.Errorf("%w: %w", errMalformedBody, err)
	}
	return err
}

// AsyncDeletionTimeout bounds the wait for a 202-accepted deletion to complete.
const AsyncDeletionTimeout = 5 * time.Minute

// Poll pacing for waitForDeletion. Variables rather than constants so tests can
// shrink them: at the production interval every poll test would sleep for real.
var (
	deletionPollInterval    = 500 * time.Millisecond
	deletionPollMaxInterval = 5 * time.Second
)

// deletionDone turns the result of a "does it still exist?" GET into a poll
// verdict: a 404 means the record is gone, anything else is an error for
// waitForDeletion to classify.
func deletionDone(err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return true, nil
	}
	return false, err
}

// retryablePoll reports whether an error from a deletion poll is worth another
// attempt. The DELETE was already accepted, so a proxy 502 or a dropped
// connection mid-teardown should not turn a successful destroy into a failure.
// A 4xx will not fix itself, so those fail immediately.
func retryablePoll(err error) bool {
	// A syntactically bad body will be just as bad next time. Everything else
	// that is not an API error is the transport failing mid-flight — a reset, a
	// truncated read, a DNS blip — and is worth another attempt.
	if errors.Is(err, errMalformedBody) {
		return false
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return true
	}

	// 429 and 408 are the 4xx that fix themselves. doRequest already backs off on
	// 429, but it
	// gives up after five attempts and hands the 429 up: a fleet destroy is
	// exactly the workload that trips the limiter, and the DELETE was accepted
	// already, so failing here would abort an apply over a transient limit.
	switch {
	case apiErr.StatusCode >= 500:
		return true
	case apiErr.StatusCode == http.StatusTooManyRequests,
		apiErr.StatusCode == http.StatusRequestTimeout:
		return true
	default:
		return false
	}
}

// waitForDeletion polls gone until it reports the record has disappeared,
// backing off between attempts. Deletions that answer 202 are asynchronous: the
// record outlives the response, so returning early breaks a delete followed by
// a recreate of the same host in one apply.
func waitForDeletion(ctx context.Context, timeout time.Duration, gone func(context.Context) (bool, error)) error {
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	interval := deletionPollInterval

	// Kept so a poll that only ever saw transient failures reports why, rather
	// than a bare timeout. Two of them: an http.Client timeout also satisfies
	// errors.Is(err, context.DeadlineExceeded), so a deadline must never be
	// allowed to overwrite a real API error worth reporting.
	var lastErr, lastNonContextErr error

	timedOut := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A real API error beats "context deadline exceeded" every time.
		if reportable := lastNonContextErr; reportable != nil {
			return fmt.Errorf("timed out after %s waiting for the deletion to complete; last error: %w", timeout, reportable)
		}
		if lastErr != nil {
			return fmt.Errorf("timed out after %s waiting for the deletion to complete; last error: %w", timeout, lastErr)
		}
		return fmt.Errorf("timed out after %s waiting for the deletion to complete", timeout)
	}

	for {
		done, err := gone(pollCtx)
		// Recorded before the deadline check so the timeout message names it even
		// when the very first poll outlives the deadline.
		if err != nil && retryablePoll(err) {
			lastErr = err
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				lastNonContextErr = err
			}
		}
		switch {
		// A successful "it is gone" wins even if the deadline expired on the
		// same tick: the deletion did finish.
		case err == nil && done:
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		case pollCtx.Err() != nil:
			return timedOut()
		case err != nil && !retryablePoll(err):
			return err
		}

		select {
		case <-time.After(interval):
		case <-pollCtx.Done():
			return timedOut()
		}

		if interval *= 2; interval > deletionPollMaxInterval {
			interval = deletionPollMaxInterval
		}
	}
}

// decodeKeyed decodes a JSON response body that may or may not be wrapped in a
// single-key envelope, e.g. both {"version": {...}} and {...} decode into the
// same target for key "version".
func decodeKeyed(resp *http.Response, key string, target interface{}) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) == nil {
		if inner, ok := envelope[key]; ok {
			return json.Unmarshal(inner, target)
		}
	}
	return json.Unmarshal(body, target)
}

// listResponse is a generic list response envelope.
type listResponse struct {
	Meta PaginationMeta `json:"meta"`
}

// maxListPages bounds every paginated walk, so a server that never stops
// returning full pages degrades to an error rather than an unbounded request
// stream inside a terraform plan.
const maxListPages = 1000

// morePages reports whether a walk should continue after a page that returned n
// items. n is the page size as served, before any client-side filtering — a page
// of entirely filtered-out records still means there is more to fetch.
//
// The empty-page check comes first deliberately: it is the only guard that
// survives a change to the meta envelope. The last such change renamed every
// field, the old struct decoded to zeros, `count+offset >= total` became
// `0 >= 0`, and all eight list loops truncated at 100 rows while their unit
// tests stayed green. Requiring TotalPages > 0 before trusting the counters
// means an unrecognised meta now over-fetches by one page instead of silently
// dropping data.
func morePages(n int, meta PaginationMeta, page int) (bool, error) {
	if page >= maxListPages {
		return false, fmt.Errorf("pagination exceeded %d pages; the index meta looks inconsistent", maxListPages)
	}
	if meta.TotalPages > 0 {
		// A recognised meta is authoritative, including across an empty middle
		// page: stopping early would return a short list with no error, and wrong
		// data is worse than an extra round trip.
		return meta.CurrentPage < meta.TotalPages, nil
	}
	// Unrecognised envelope — every field decoded to zero. Walk until an empty
	// page rather than trusting counters we clearly cannot read.
	return n > 0, nil
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
		more, err := morePages(len(result.Instances), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
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

	etag := sanitizeETag(resp.Header.Get("ETag"))
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

// DeleteInstance deletes an instance. It reports whether the API accepted the
// deletion asynchronously (202), in which case the host still exists until the
// backend finishes tearing it down — see WaitForInstanceDeletion.
func (c *Client) DeleteInstance(ctx context.Context, id string) (accepted bool, err error) {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/instances/"+id, nil, nil)
	if err != nil {
		return false, err
	}
	// 202 Accepted (async) or 204 No Content
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return false, parseError(resp)
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusAccepted, nil
}

// WaitForInstanceDeletion polls the instance until the API 404s it.
func (c *Client) WaitForInstanceDeletion(ctx context.Context, id string, timeout time.Duration) error {
	return waitForDeletion(ctx, timeout, func(ctx context.Context) (bool, error) {
		_, _, err := c.GetInstance(ctx, id)
		return deletionDone(err)
	})
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
		more, err := morePages(len(result.Tasks), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
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

	etag := sanitizeETag(resp.Header.Get("ETag"))
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

func (c *Client) PauseTask(ctx context.Context, id string) (*Task, error) {
	return c.taskAction(ctx, id, "pause")
}

func (c *Client) ResumeTask(ctx context.Context, id string) (*Task, error) {
	return c.taskAction(ctx, id, "resume")
}

// taskAction posts to a task action endpoint and returns the task the API renders
// back. Resume recomputes expected_ping_at server-side, so the response body is the
// only accurate view of the task afterwards.
func (c *Client) taskAction(ctx context.Context, id string, action string) (*Task, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/tasks/%s/%s", id, action), nil, nil)
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
	// A 200 that decodes to an empty or mismatched task would otherwise be written
	// straight to state, blanking the resource ID.
	if result.Task.ID != id {
		return nil, fmt.Errorf("task %s returned an unexpected task id %q", action, result.Task.ID)
	}
	return &result.Task, nil
}

// --- Workflows ---

// ListWorkflows returns every workflow matching opts. Archived workflows are
// excluded by the API unless opts.Status asks for them.
func (c *Client) ListWorkflows(ctx context.Context, opts WorkflowListOptions) ([]Workflow, error) {
	var all []Workflow
	page := 1
	for {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", "100")
		for key, value := range map[string]string{
			"status":        opts.Status,
			"updated_since": opts.UpdatedSince,
			"order":         opts.Order,
			"direction":     opts.Direction,
			"q":             opts.Q,
		} {
			if value != "" {
				query.Set(key, value)
			}
		}

		resp, err := c.doRequest(ctx, "GET", "/api/v1/workflows?"+query.Encode(), nil, nil)
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
		// Archived workflows are excluded by the API unless opts.Status asks for
		// them, so there is no client-side filter to apply here.
		all = append(all, result.Workflows...)
		more, err := morePages(len(result.Workflows), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

// --- Workflow Runs ---

func (c *Client) ListWorkflowRuns(ctx context.Context, workflowID int64, opts WorkflowRunListOptions) ([]WorkflowRun, error) {
	var all []WorkflowRun
	page := 1
	for {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", "100")
		for key, value := range map[string]string{
			"status":        opts.Status,
			"updated_since": opts.UpdatedSince,
			"order":         opts.Order,
			"direction":     opts.Direction,
		} {
			if value != "" {
				query.Set(key, value)
			}
		}

		path := fmt.Sprintf("/api/v1/workflows/%d/runs?%s", workflowID, query.Encode())
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
		more, err := morePages(len(result.Runs), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

// GetWorkflowRun returns one run with its per-step detail. Run ids are not
// global: a run belonging to another workflow is a 404 here.
func (c *Client) GetWorkflowRun(ctx context.Context, workflowID, runID int64) (*WorkflowRunDetail, error) {
	path := fmt.Sprintf("/api/v1/workflows/%d/runs/%d", workflowID, runID)
	resp, err := c.doRequest(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Run WorkflowRunDetail `json:"run"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Run, nil
}

func (c *Client) GetWorkflow(ctx context.Context, id int64) (*Workflow, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/workflows/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}

	etag := sanitizeETag(resp.Header.Get("ETag"))
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

// UpdateWorkflow patches a workflow. Unlike instances, tasks and monitors, the
// workflow endpoints do not support If-Match, so there is no ETag to pass.
func (c *Client) UpdateWorkflow(ctx context.Context, id int64, input UpdateWorkflowInput) (*Workflow, error) {
	body := map[string]interface{}{"workflow": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/workflows/"+strconv.FormatInt(id, 10), body, nil)
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

// GetWorkflowVersion returns a single version including its full execution
// graph and canvas data, which the workflow and version list responses omit.
func (c *Client) GetWorkflowVersion(ctx context.Context, workflowID, versionID int64) (*WorkflowVersion, error) {
	path := fmt.Sprintf("/api/v1/workflows/%d/versions/%d", workflowID, versionID)
	resp, err := c.doRequest(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var version WorkflowVersion
	if err := decodeKeyed(resp, "version", &version); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &version, nil
}

// --- Workflow Templates ---

// ListWorkflowTemplates returns the catalogue of instantiable templates.
func (c *Client) ListWorkflowTemplates(ctx context.Context) ([]WorkflowTemplate, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/workflows/templates", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var items []json.RawMessage
	if err := decodeKeyed(resp, "templates", &items); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	templates := make([]WorkflowTemplate, 0, len(items))
	for _, item := range items {
		var t WorkflowTemplate
		if err := json.Unmarshal(item, &t); err != nil {
			return nil, fmt.Errorf("decoding template: %w", err)
		}
		t.Raw = item
		templates = append(templates, t)
	}
	return templates, nil
}

// CreateWorkflowFromTemplate instantiates a template as a draft workflow whose
// graph is already published.
func (c *Client) CreateWorkflowFromTemplate(ctx context.Context, slug string) (*Workflow, error) {
	body := map[string]interface{}{"slug": slug}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/workflows/templates", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var workflow Workflow
	if err := decodeKeyed(resp, "workflow", &workflow); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &workflow, nil
}

// --- Node Types ---

// ListNodeTypes returns the node kinds available to execution graphs.
func (c *Client) ListNodeTypes(ctx context.Context) ([]NodeType, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/node_types", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var items []json.RawMessage
	if err := decodeKeyed(resp, "node_types", &items); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	nodeTypes := make([]NodeType, 0, len(items))
	for _, item := range items {
		var n NodeType
		if err := json.Unmarshal(item, &n); err != nil {
			return nil, fmt.Errorf("decoding node type: %w", err)
		}
		n.Raw = item
		nodeTypes = append(nodeTypes, n)
	}
	return nodeTypes, nil
}

// --- Uptime Monitors ---

func (c *Client) ListUptimeMonitors(ctx context.Context, opts *ListUptimeMonitorsOptions) ([]UptimeMonitor, error) {
	query := uptimeMonitorFilters(opts)
	query.Set("per_page", "100")

	var all []UptimeMonitor
	page := 1
	for {
		query.Set("page", strconv.Itoa(page))
		path := "/api/v1/uptime_monitors?" + query.Encode()
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
		more, err := morePages(len(result.UptimeMonitors), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetUptimeMonitor(ctx context.Context, id string) (*UptimeMonitor, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/uptime_monitors/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}

	etag := sanitizeETag(resp.Header.Get("ETag"))
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
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/uptime_monitors/"+url.PathEscape(id), body, headers)
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
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/uptime_monitors/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// PauseUptimeMonitor suspends checks for a monitor. It is idempotent: pausing an
// already paused monitor succeeds and returns it unchanged.
func (c *Client) PauseUptimeMonitor(ctx context.Context, id string) (*UptimeMonitor, error) {
	return c.uptimeMonitorAction(ctx, id, "pause")
}

// ResumeUptimeMonitor restarts checks for a paused monitor. It is idempotent.
func (c *Client) ResumeUptimeMonitor(ctx context.Context, id string) (*UptimeMonitor, error) {
	return c.uptimeMonitorAction(ctx, id, "resume")
}

func (c *Client) uptimeMonitorAction(ctx context.Context, id string, action string) (*UptimeMonitor, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/uptime_monitors/%s/%s", url.PathEscape(id), action), nil, nil)
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
	// A 200 that decodes to an empty or mismatched monitor would otherwise be
	// written straight to state, blanking the resource ID.
	if result.UptimeMonitor.ID != id {
		return nil, fmt.Errorf("uptime monitor %s returned an unexpected monitor id %q", action, result.UptimeMonitor.ID)
	}
	return &result.UptimeMonitor, nil
}

// GetUptimeMonitorStatus fetches the lightweight status payload for a monitor.
func (c *Client) GetUptimeMonitorStatus(ctx context.Context, id string) (*UptimeMonitorStatus, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/uptime_monitors/"+url.PathEscape(id)+"/status", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var status UptimeMonitorStatus
	if err := json.Unmarshal(unwrapEnvelope(body, "uptime_monitor_status", "uptime_monitor"), &status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	// Unknown fields decode silently, so a shape we do not recognise would yield a
	// zero-valued status that reads downstream as a healthy, unpaused monitor.
	// Refuse it instead: a loud error beats a wrong status in Terraform state.
	// Both fields empty means nothing was recognised; a null status alone is a
	// legitimate payload and stays the API's to report.
	if status.ID == "" && status.Status == "" {
		return nil, fmt.Errorf("unrecognized status payload for uptime monitor %s", id)
	}
	if status.ID != "" && status.ID != id {
		return nil, fmt.Errorf("status for uptime monitor %s returned an unexpected monitor id %q", id, status.ID)
	}
	return &status, nil
}

// unwrapEnvelope returns the object nested under the first of keys present in
// body, or body itself when it is already the bare object. The status endpoint
// is newer than the rest of the API and its wrapper key is not pinned by the
// published spec, so accept either shape. A body that matches neither falls
// through to the caller, which rejects it rather than decoding an empty value.
func unwrapEnvelope(body []byte, keys ...string) []byte {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return body
	}
	for _, key := range keys {
		if v, ok := envelope[key]; ok && len(v) > 0 && v[0] == '{' {
			return v
		}
	}
	return body
}

// uptimeMonitorFilters renders the index filter options as query parameters.
func uptimeMonitorFilters(opts *ListUptimeMonitorsOptions) url.Values {
	query := url.Values{}
	if opts == nil {
		return query
	}
	for key, value := range map[string]string{
		"status":        opts.Status,
		"protocol":      opts.Protocol,
		"q":             opts.Query,
		"updated_since": opts.UpdatedSince,
		"order":         opts.Order,
		"direction":     opts.Direction,
	} {
		if value != "" {
			query.Set(key, value)
		}
	}
	return query
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

// ListIntegrations walks the whole index. The endpoint became paginated on
// 2026-09-01 at 25 per page; until it routed through morePages this was a
// single un-paginated GET, so an organisation with more than 25 channels got a
// silently truncated list — the same failure the meta rename caused everywhere
// else, arriving separately because this loop never had a meta to misread.
func (c *Client) ListIntegrations(ctx context.Context, opts IntegrationListOptions) ([]Integration, error) {
	var all []Integration
	page := 1
	for {
		// Enabled is a *bool so that false stays distinct from unset; it is
		// rendered here rather than in the struct so the omit-empty rule below
		// stays the single gate on what reaches the wire.
		enabled := ""
		if opts.Enabled != nil {
			enabled = strconv.FormatBool(*opts.Enabled)
		}
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", "100")
		for key, value := range map[string]string{
			"type":          opts.Type,
			"enabled":       enabled,
			"q":             opts.Q,
			"updated_since": opts.UpdatedSince,
			"order":         opts.Order,
			"direction":     opts.Direction,
		} {
			if value != "" {
				query.Set(key, value)
			}
		}

		resp, err := c.doRequest(ctx, "GET", "/api/v1/integrations?"+query.Encode(), nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}

		var result struct {
			Integrations []Integration  `json:"integrations"`
			Meta         PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.Integrations...)
		more, err := morePages(len(result.Integrations), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

// --- Incidents ---

func (c *Client) ListIncidents(ctx context.Context, opts IncidentListOptions) ([]Incident, error) {
	var all []Incident
	page := 1
	for {
		// WorkflowID is a *int64 so that "not filtering" stays distinct from
		// workflow 0; it is rendered here rather than in the struct so the
		// omit-empty rule below stays the single gate on what reaches the wire.
		workflowID := ""
		if opts.WorkflowID != nil {
			workflowID = strconv.FormatInt(*opts.WorkflowID, 10)
		}
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", "100")
		for key, value := range map[string]string{
			"status":            opts.Status,
			"q":                 opts.Q,
			"host_id":           opts.HostID,
			"task_id":           opts.TaskID,
			"uptime_monitor_id": opts.UptimeMonitorID,
			"workflow_id":       workflowID,
			"from":              opts.From,
			"to":                opts.To,
			"updated_since":     opts.UpdatedSince,
			"order":             opts.Order,
			"direction":         opts.Direction,
		} {
			if value != "" {
				query.Set(key, value)
			}
		}

		resp, err := c.doRequest(ctx, "GET", "/api/v1/incidents?"+query.Encode(), nil, nil)
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
		more, err := morePages(len(result.Incidents), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
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

// --- Network Devices ---

func (c *Client) ListNetworkDevices(ctx context.Context) ([]NetworkDevice, error) {
	var all []NetworkDevice
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/network_devices?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}
		var result struct {
			NetworkDevices []NetworkDevice `json:"network_devices"`
			Meta           PaginationMeta  `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.NetworkDevices...)
		more, err := morePages(len(result.NetworkDevices), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetNetworkDevice(ctx context.Context, id string) (*NetworkDevice, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/network_devices/"+id, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}
	etag := sanitizeETag(resp.Header.Get("ETag"))
	var result struct {
		NetworkDevice NetworkDevice `json:"network_device"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.NetworkDevice, etag, nil
}

func (c *Client) CreateNetworkDevice(ctx context.Context, input CreateNetworkDeviceInput) (*NetworkDevice, error) {
	body := map[string]interface{}{"network_device": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/network_devices", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		NetworkDevice NetworkDevice `json:"network_device"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.NetworkDevice, nil
}

func (c *Client) UpdateNetworkDevice(ctx context.Context, id string, etag string, input UpdateNetworkDeviceInput) (*NetworkDevice, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"network_device": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/network_devices/"+id, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		NetworkDevice NetworkDevice `json:"network_device"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.NetworkDevice, nil
}

// DeleteNetworkDevice deletes a network device. Like instances, the API tears
// devices down asynchronously and answers 202 — see WaitForNetworkDeviceDeletion.
func (c *Client) DeleteNetworkDevice(ctx context.Context, id string) (accepted bool, err error) {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/network_devices/"+id, nil, nil)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return false, parseError(resp)
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusAccepted, nil
}

// WaitForNetworkDeviceDeletion polls the device until the API 404s it.
func (c *Client) WaitForNetworkDeviceDeletion(ctx context.Context, id string, timeout time.Duration) error {
	return waitForDeletion(ctx, timeout, func(ctx context.Context) (bool, error) {
		_, _, err := c.GetNetworkDevice(ctx, id)
		return deletionDone(err)
	})
}

// EnterMaintenanceNetworkDevice puts the device in maintenance mode and returns
// the updated device, so callers don't need a follow-up GET.
func (c *Client) EnterMaintenanceNetworkDevice(ctx context.Context, id string) (*NetworkDevice, error) {
	return c.maintenanceNetworkDevice(ctx, id, "enter_maintenance")
}

// ExitMaintenanceNetworkDevice takes the device out of maintenance mode and
// returns the updated device, so callers don't need a follow-up GET.
func (c *Client) ExitMaintenanceNetworkDevice(ctx context.Context, id string) (*NetworkDevice, error) {
	return c.maintenanceNetworkDevice(ctx, id, "exit_maintenance")
}

func (c *Client) maintenanceNetworkDevice(ctx context.Context, id, action string) (*NetworkDevice, error) {
	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/network_devices/%s/%s", id, action), nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		NetworkDevice NetworkDevice `json:"network_device"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.NetworkDevice, nil
}

// --- Status Pages ---

func (c *Client) ListStatusPages(ctx context.Context) ([]StatusPage, error) {
	var all []StatusPage
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/status_pages?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}
		var result struct {
			StatusPages []StatusPage   `json:"status_pages"`
			Meta        PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.StatusPages...)
		more, err := morePages(len(result.StatusPages), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetStatusPage(ctx context.Context, id int64) (*StatusPage, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/status_pages/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}
	etag := sanitizeETag(resp.Header.Get("ETag"))
	var result struct {
		StatusPage StatusPage `json:"status_page"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.StatusPage, etag, nil
}

func (c *Client) CreateStatusPage(ctx context.Context, input CreateStatusPageInput) (*StatusPage, error) {
	body := map[string]interface{}{"status_page": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/status_pages", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		StatusPage StatusPage `json:"status_page"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.StatusPage, nil
}

func (c *Client) UpdateStatusPage(ctx context.Context, id int64, etag string, input UpdateStatusPageInput) (*StatusPage, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"status_page": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/status_pages/"+strconv.FormatInt(id, 10), body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		StatusPage StatusPage `json:"status_page"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.StatusPage, nil
}

func (c *Client) DeleteStatusPage(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/status_pages/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// CreateIntegration creates a notification channel.
//
// Required fields depend on input.Type:
//
//	webhook    URL (Name and Secret optional)
//	pagerduty  Name + RoutingKey — proved with a live trigger/resolve round-trip
//	pushover   Name + UserKey + AppToken
//	email      Email — returns 202 with an EmailVerification and creates no channel
//
// slack, discord, teams and telegram are interactive OAuth/app installs with no
// headless equivalent; the API rejects them with 422.
func (c *Client) CreateIntegration(ctx context.Context, input CreateIntegrationInput) (*CreateIntegrationResult, error) {
	body := map[string]interface{}{"integration": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/integrations", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, parseError(resp)
	}

	var result struct {
		Integration  *Integration         `json:"integration"`
		Webhook      *WebhookVerification `json:"webhook"`
		Verification *EmailVerification   `json:"verification"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &CreateIntegrationResult{
		Integration:       result.Integration,
		Webhook:           result.Webhook,
		EmailVerification: result.Verification,
	}, nil
}

func (c *Client) DeleteIntegration(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/integrations/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// VerifyWebhookIntegration asks the API to GET the webhook URL and check that
// the response echoes the verification token in the X-Fivenines-Verification
// header. Until it succeeds the webhook stays unverified and workflow
// notification nodes refuse to deliver to it.
func (c *Client) VerifyWebhookIntegration(ctx context.Context, id int64) (*Integration, error) {
	path := fmt.Sprintf("/api/v1/integrations/%d/verify_webhook", id)
	resp, err := c.doRequest(ctx, "POST", path, nil, nil)
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

// RegenerateWebhookToken issues a fresh 24 hour verification token and returns
// it once. The token lives in metadata, which is never serialized on a read.
func (c *Client) RegenerateWebhookToken(ctx context.Context, id int64) (*Integration, *WebhookVerification, error) {
	path := fmt.Sprintf("/api/v1/integrations/%d/regenerate_webhook_token", id)
	resp, err := c.doRequest(ctx, "POST", path, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, parseError(resp)
	}

	var result struct {
		Integration Integration          `json:"integration"`
		Webhook     *WebhookVerification `json:"webhook"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Integration, result.Webhook, nil
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

// --- Status Page Maintenance Windows ---

func maintenanceWindowPath(statusPageID int64) string {
	return fmt.Sprintf("/api/v1/status_pages/%d/maintenance_windows", statusPageID)
}

func (c *Client) GetStatusPageMaintenanceWindow(ctx context.Context, statusPageID, id int64) (*StatusPageMaintenanceWindow, string, error) {
	path := fmt.Sprintf("%s/%d", maintenanceWindowPath(statusPageID), id)
	resp, err := c.doRequest(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}
	etag := sanitizeETag(resp.Header.Get("ETag"))
	var result struct {
		MaintenanceWindow StatusPageMaintenanceWindow `json:"maintenance_window"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.MaintenanceWindow, etag, nil
}

func (c *Client) CreateStatusPageMaintenanceWindow(ctx context.Context, statusPageID int64, input CreateStatusPageMaintenanceWindowInput) (*StatusPageMaintenanceWindow, error) {
	body := map[string]interface{}{"maintenance_window": input}
	resp, err := c.doRequest(ctx, "POST", maintenanceWindowPath(statusPageID), body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		MaintenanceWindow StatusPageMaintenanceWindow `json:"maintenance_window"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.MaintenanceWindow, nil
}

func (c *Client) UpdateStatusPageMaintenanceWindow(ctx context.Context, statusPageID, id int64, etag string, input UpdateStatusPageMaintenanceWindowInput) (*StatusPageMaintenanceWindow, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	path := fmt.Sprintf("%s/%d", maintenanceWindowPath(statusPageID), id)
	body := map[string]interface{}{"maintenance_window": input}
	resp, err := c.doRequest(ctx, "PATCH", path, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		MaintenanceWindow StatusPageMaintenanceWindow `json:"maintenance_window"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.MaintenanceWindow, nil
}

// DeleteStatusPageMaintenanceWindow permanently removes the window, including
// from the status page history. Use CancelStatusPageMaintenanceWindow to keep
// the record.
func (c *Client) DeleteStatusPageMaintenanceWindow(ctx context.Context, statusPageID, id int64) error {
	path := fmt.Sprintf("%s/%d", maintenanceWindowPath(statusPageID), id)
	resp, err := c.doRequest(ctx, "DELETE", path, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// CancelStatusPageMaintenanceWindow marks the window canceled while preserving
// it in the status page history. The endpoint is idempotent.
func (c *Client) CancelStatusPageMaintenanceWindow(ctx context.Context, statusPageID, id int64) error {
	path := fmt.Sprintf("%s/%d/cancel", maintenanceWindowPath(statusPageID), id)
	resp, err := c.doRequest(ctx, "PATCH", path, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Host Groups ---

// hostGroupFilters renders the index filters as query parameters, skipping the
// unset ones. The endpoint 400s on an unknown parameter, so nothing outside the
// documented set may reach it.
func hostGroupFilters(opts *ListHostGroupsOptions) url.Values {
	query := url.Values{}
	if opts == nil {
		return query
	}
	for key, value := range map[string]string{
		"q":             opts.Query,
		"updated_since": opts.UpdatedSince,
		"order":         opts.Order,
		"direction":     opts.Direction,
	} {
		if value != "" {
			query.Set(key, value)
		}
	}
	return query
}

func (c *Client) ListHostGroups(ctx context.Context, opts *ListHostGroupsOptions) ([]HostGroup, error) {
	query := hostGroupFilters(opts)
	query.Set("per_page", "100")

	var all []HostGroup
	page := 1
	for {
		query.Set("page", strconv.Itoa(page))
		path := "/api/v1/host_groups?" + query.Encode()
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}
		var result struct {
			HostGroups []HostGroup    `json:"host_groups"`
			Meta       PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.HostGroups...)
		more, err := morePages(len(result.HostGroups), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetHostGroup(ctx context.Context, id int64) (*HostGroup, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/host_groups/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}
	etag := sanitizeETag(resp.Header.Get("ETag"))
	var result struct {
		HostGroup HostGroup `json:"host_group"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.HostGroup, etag, nil
}

func (c *Client) CreateHostGroup(ctx context.Context, input CreateHostGroupInput) (*HostGroup, error) {
	body := map[string]interface{}{"host_group": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/host_groups", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		HostGroup HostGroup `json:"host_group"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.HostGroup, nil
}

func (c *Client) UpdateHostGroup(ctx context.Context, id int64, etag string, input UpdateHostGroupInput) (*HostGroup, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"host_group": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/host_groups/"+strconv.FormatInt(id, 10), body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		HostGroup HostGroup `json:"host_group"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.HostGroup, nil
}

// DeleteHostGroup removes a host group. The API ungroups its instances; it never
// deletes them.
func (c *Client) DeleteHostGroup(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/host_groups/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- MQTT Brokers ---

func (c *Client) ListMQTTBrokers(ctx context.Context) ([]MQTTBroker, error) {
	var all []MQTTBroker
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/mqtt_brokers?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}
		var result struct {
			MQTTBrokers []MQTTBroker   `json:"mqtt_brokers"`
			Meta        PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.MQTTBrokers...)
		more, err := morePages(len(result.MQTTBrokers), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetMQTTBroker(ctx context.Context, id string) (*MQTTBroker, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/mqtt_brokers/"+id, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}
	etag := sanitizeETag(resp.Header.Get("ETag"))
	var result struct {
		MQTTBroker MQTTBroker `json:"mqtt_broker"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.MQTTBroker, etag, nil
}

func (c *Client) CreateMQTTBroker(ctx context.Context, input CreateMQTTBrokerInput) (*MQTTBroker, error) {
	body := map[string]interface{}{"mqtt_broker": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/mqtt_brokers", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		MQTTBroker MQTTBroker `json:"mqtt_broker"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.MQTTBroker, nil
}

func (c *Client) UpdateMQTTBroker(ctx context.Context, id string, etag string, input UpdateMQTTBrokerInput) (*MQTTBroker, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"mqtt_broker": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/mqtt_brokers/"+id, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		MQTTBroker MQTTBroker `json:"mqtt_broker"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.MQTTBroker, nil
}

// DeleteMQTTBroker deletes a broker and, server-side, every topic monitor under
// it — the freed monitor slots return to the plan limit.
func (c *Client) DeleteMQTTBroker(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/mqtt_brokers/"+id, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- MQTT Topic Monitors ---
//
// Topic monitors are nested under their broker: every path carries the broker
// id, and a monitor id from another broker (or another organisation) is a 404.

func topicMonitorPath(brokerID string) string {
	return fmt.Sprintf("/api/v1/mqtt_brokers/%s/topic_monitors", brokerID)
}

func (c *Client) ListMQTTTopicMonitors(ctx context.Context, brokerID string) ([]MQTTTopicMonitor, error) {
	var all []MQTTTopicMonitor
	page := 1
	for {
		path := fmt.Sprintf("%s?page=%d&per_page=100", topicMonitorPath(brokerID), page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}
		var result struct {
			TopicMonitors []MQTTTopicMonitor `json:"topic_monitors"`
			Meta          PaginationMeta     `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		all = append(all, result.TopicMonitors...)
		more, err := morePages(len(result.TopicMonitors), result.Meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) GetMQTTTopicMonitor(ctx context.Context, brokerID, id string) (*MQTTTopicMonitor, string, error) {
	path := fmt.Sprintf("%s/%s", topicMonitorPath(brokerID), id)
	resp, err := c.doRequest(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}
	etag := sanitizeETag(resp.Header.Get("ETag"))
	var result struct {
		TopicMonitor MQTTTopicMonitor `json:"topic_monitor"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.TopicMonitor, etag, nil
}

// CreateMQTTTopicMonitor adds one monitor to a broker. This is the billable
// unit: each monitor counts 1 toward the organisation's monitor limit, and a
// create at the limit is rejected with 422.
func (c *Client) CreateMQTTTopicMonitor(ctx context.Context, brokerID string, input CreateMQTTTopicMonitorInput) (*MQTTTopicMonitor, error) {
	body := map[string]interface{}{"topic_monitor": input}
	resp, err := c.doRequest(ctx, "POST", topicMonitorPath(brokerID), body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		TopicMonitor MQTTTopicMonitor `json:"topic_monitor"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.TopicMonitor, nil
}

func (c *Client) UpdateMQTTTopicMonitor(ctx context.Context, brokerID, id string, etag string, input UpdateMQTTTopicMonitorInput) (*MQTTTopicMonitor, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	path := fmt.Sprintf("%s/%s", topicMonitorPath(brokerID), id)
	body := map[string]interface{}{"topic_monitor": input}
	resp, err := c.doRequest(ctx, "PATCH", path, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		TopicMonitor MQTTTopicMonitor `json:"topic_monitor"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.TopicMonitor, nil
}

func (c *Client) DeleteMQTTTopicMonitor(ctx context.Context, brokerID, id string) error {
	path := fmt.Sprintf("%s/%s", topicMonitorPath(brokerID), id)
	resp, err := c.doRequest(ctx, "DELETE", path, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- API Tokens ---

// ListAPITokens returns every API token belonging to the calling user in this
// organization, revoked and expired ones included.
func (c *Client) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	var all []APIToken
	err := c.walkAPITokens(ctx, func(page []APIToken) bool {
		all = append(all, page...)
		return true
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// GetAPIToken returns one token by id.
//
// There is no show endpoint for api_tokens — index, create and revoke are the
// whole surface — so this walks the index and synthesises the 404 a caller
// expects when the row is gone. It stops at the page holding the match, which
// matters because a revoke keeps the row: the list only ever grows, and every
// refresh of every managed token walks it.
//
// Narrowing the request is deliberately not attempted. `q` matches on name,
// which is not the key being looked up, and `active` would hide the expired
// tokens this resource keeps in state on purpose — either one turns a miss into
// a 404, which Read reads as "gone" and the next plan turns into minting a
// replacement for a credential that is still live.
func (c *Client) GetAPIToken(ctx context.Context, id int64) (*APIToken, error) {
	var found *APIToken
	err := c.walkAPITokens(ctx, func(page []APIToken) bool {
		for i := range page {
			if page[i].ID == id {
				found = &page[i]
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, &APIError{
			StatusCode: http.StatusNotFound,
			Message:    fmt.Sprintf("no API token with id %d belongs to you in this organization", id),
		}
	}
	return found, nil
}

// walkAPITokens pages through the index, handing each page to fn. fn returns
// false to stop the walk early.
func (c *Client) walkAPITokens(ctx context.Context, fn func([]APIToken) bool) error {
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/api_tokens?page=%d&per_page=100", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return parseError(resp)
		}

		var result struct {
			APITokens []APIToken     `json:"api_tokens"`
			Meta      PaginationMeta `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
		if !fn(result.APITokens) {
			return nil
		}
		more, err := morePages(len(result.APITokens), result.Meta, page)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
		page++
	}
}

// CreateAPIToken mints a token. The returned APIToken is the only place the
// plaintext value ever appears - it is stored as a digest, so a lost value can
// only be replaced, never recovered.
func (c *Client) CreateAPIToken(ctx context.Context, input CreateAPITokenInput) (*APIToken, error) {
	body := map[string]interface{}{"api_token": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/api_tokens", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}

	var result struct {
		APIToken APIToken `json:"api_token"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.APIToken, nil
}

// RevokeAPIToken kills a token. DELETE here is a revocation, not a delete: the
// row survives with a revoked_at stamp so the audit trail outlives the
// credential. The API renders the revoked row back and nothing wants it — the
// resource is dropping the token from state either way.
func (c *Client) RevokeAPIToken(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/api_tokens/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Enrollment Tokens ---

// GetEnrollmentToken returns one enrollment token by id.
//
// The API has no GET /enrollment_tokens/:id — the value is write-once, so the
// route was never opened — which leaves the index as the only way to read a
// token back. Same shape as GetAPIToken, including the synthetic 404 that lets
// the resource treat a deleted token like one on any other resource.
func (c *Client) GetEnrollmentToken(ctx context.Context, id int64) (*EnrollmentToken, error) {
	var found *EnrollmentToken
	err := c.walkEnrollmentTokens(ctx, func(page []EnrollmentToken) bool {
		for i := range page {
			if page[i].ID == id {
				found = &page[i]
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, &APIError{
			StatusCode: http.StatusNotFound,
			Message:    fmt.Sprintf("no enrollment token with id %d belongs to you in this organization", id),
		}
	}
	return found, nil
}

// walkEnrollmentTokens pages through the index, handing each page to fn. fn
// returns false to stop the walk early.
//
// Ordered ascending, unlike the other walks: the server appends `id DESC` as a
// tiebreaker, so with the newest rows sorted LAST a token minted while we are
// paging is appended rather than shifting an unread row onto a page already
// read. The default `created_at desc` has that hazard, and this is the one index
// a fleet bootstrap can be actively growing while Terraform refreshes it.
func (c *Client) walkEnrollmentTokens(ctx context.Context, fn func([]EnrollmentToken) bool) error {
	page := 1
	for {
		path := fmt.Sprintf("/api/v1/enrollment_tokens?page=%d&per_page=100&order=created_at&direction=asc", page)
		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return parseError(resp)
		}

		var result struct {
			EnrollmentTokens []EnrollmentToken `json:"enrollment_tokens"`
			Meta             PaginationMeta    `json:"meta"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
		if !fn(result.EnrollmentTokens) {
			return nil
		}
		more, err := morePages(len(result.EnrollmentTokens), result.Meta, page)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
		page++
	}
}

// CreateEnrollmentToken mints an enrollment token. The returned EnrollmentToken
// is the only place the value ever appears: index and revoke render metadata, so
// a lost value can only be replaced, never recovered.
func (c *Client) CreateEnrollmentToken(ctx context.Context, input CreateEnrollmentTokenInput) (*EnrollmentToken, error) {
	body := map[string]interface{}{"enrollment_token": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/enrollment_tokens", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}

	var result struct {
		EnrollmentToken EnrollmentToken `json:"enrollment_token"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	// A create that decodes without a value has lost it for good. Failing here
	// beats writing a token to state that cannot enroll anything.
	if result.EnrollmentToken.Token == "" {
		return nil, fmt.Errorf("enrollment token %d was created but the response carried no token value; "+
			"the value is returned once and cannot be fetched back", result.EnrollmentToken.ID)
	}
	return &result.EnrollmentToken, nil
}

// RevokeEnrollmentToken stops a token from enrolling any new host, keeping the
// row and the hosts it already registered. Idempotent server-side. The response
// is metadata only — Token is empty on the returned struct.
func (c *Client) RevokeEnrollmentToken(ctx context.Context, id int64) (*EnrollmentToken, error) {
	resp, err := c.doRequest(ctx, "POST", "/api/v1/enrollment_tokens/"+strconv.FormatInt(id, 10)+"/revoke", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		EnrollmentToken EnrollmentToken `json:"enrollment_token"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	// A 200 decoding to an empty or mismatched token would otherwise be reported
	// as a successful revoke of a token we never touched.
	if result.EnrollmentToken.ID != id {
		return nil, fmt.Errorf("revoke returned an unexpected enrollment token id %d, want %d", result.EnrollmentToken.ID, id)
	}
	return &result.EnrollmentToken, nil
}

// DeleteEnrollmentToken permanently deletes a token. Unlike RevokeAPIToken, this
// really is a delete — but only for a token that has never registered a host.
// Once one has, the API refuses with 422 rather than orphan the hosts, and
// IsTokenHasRegisteredHosts identifies that refusal so the caller can revoke
// instead.
func (c *Client) DeleteEnrollmentToken(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/enrollment_tokens/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// IsTokenHasRegisteredHosts reports whether err is the enrollment token DELETE
// refusing because the token has registered hosts.
//
// The public API envelope drops the machine-readable `code` the controller
// passes to render_error, so status is all there is to match on. It is enough
// here: destroy answers 403, 404 or this one 422, and the other two are already
// handled by the time this is asked.
func IsTokenHasRegisteredHosts(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.StatusCode == http.StatusUnprocessableEntity
}

// --- Dashboards ---

// GetDashboard returns the full definition: the dashboard, its sections and
// every panel. There is no list endpoint for sections or panels - this is how
// the nested resources read themselves back.
func (c *Client) GetDashboard(ctx context.Context, id int64) (*Dashboard, string, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/dashboards/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}
	etag := sanitizeETag(resp.Header.Get("ETag"))
	var result struct {
		Dashboard Dashboard `json:"dashboard"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.Dashboard, etag, nil
}

func (c *Client) CreateDashboard(ctx context.Context, input CreateDashboardInput) (*Dashboard, error) {
	body := map[string]interface{}{"dashboard": input}
	resp, err := c.doRequest(ctx, "POST", "/api/v1/dashboards", body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		Dashboard Dashboard `json:"dashboard"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Dashboard, nil
}

func (c *Client) UpdateDashboard(ctx context.Context, id int64, etag string, input UpdateDashboardInput) (*Dashboard, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"dashboard": input}
	resp, err := c.doRequest(ctx, "PATCH", "/api/v1/dashboards/"+strconv.FormatInt(id, 10), body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		Dashboard Dashboard `json:"dashboard"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Dashboard, nil
}

func (c *Client) DeleteDashboard(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/dashboards/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Dashboard sections ---

func (c *Client) CreateDashboardSection(ctx context.Context, dashboardID int64, input DashboardSectionInput) (*DashboardSection, error) {
	body := map[string]interface{}{"section": input}
	path := fmt.Sprintf("/api/v1/dashboards/%d/sections", dashboardID)
	resp, err := c.doRequest(ctx, "POST", path, body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		Section DashboardSection `json:"section"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Section, nil
}

// UpdateDashboardSection patches a section. Sections have no GET of their own -
// the dashboard definition lists them - so there is no ETag to pass back here,
// and the write is last-writer-wins by construction.
func (c *Client) UpdateDashboardSection(ctx context.Context, dashboardID, id int64, input DashboardSectionInput) (*DashboardSection, error) {
	body := map[string]interface{}{"section": input}
	path := fmt.Sprintf("/api/v1/dashboards/%d/sections/%d", dashboardID, id)
	resp, err := c.doRequest(ctx, "PATCH", path, body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		Section DashboardSection `json:"section"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Section, nil
}

func (c *Client) DeleteDashboardSection(ctx context.Context, dashboardID, id int64) error {
	path := fmt.Sprintf("/api/v1/dashboards/%d/sections/%d", dashboardID, id)
	resp, err := c.doRequest(ctx, "DELETE", path, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Dashboard visualizations (panels) ---

func (c *Client) GetVisualization(ctx context.Context, dashboardID, id int64) (*Visualization, string, error) {
	path := fmt.Sprintf("/api/v1/dashboards/%d/visualizations/%d", dashboardID, id)
	resp, err := c.doRequest(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", parseError(resp)
	}
	etag := sanitizeETag(resp.Header.Get("ETag"))
	var result struct {
		Visualization Visualization `json:"visualization"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}
	return &result.Visualization, etag, nil
}

func (c *Client) CreateVisualization(ctx context.Context, dashboardID int64, input VisualizationInput) (*Visualization, error) {
	body := map[string]interface{}{"visualization": input}
	path := fmt.Sprintf("/api/v1/dashboards/%d/visualizations", dashboardID)
	resp, err := c.doRequest(ctx, "POST", path, body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result struct {
		Visualization Visualization `json:"visualization"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Visualization, nil
}

func (c *Client) UpdateVisualization(ctx context.Context, dashboardID, id int64, etag string, input VisualizationInput) (*Visualization, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	body := map[string]interface{}{"visualization": input}
	path := fmt.Sprintf("/api/v1/dashboards/%d/visualizations/%d", dashboardID, id)
	resp, err := c.doRequest(ctx, "PATCH", path, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		Visualization Visualization `json:"visualization"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result.Visualization, nil
}

func (c *Client) DeleteVisualization(ctx context.Context, dashboardID, id int64) error {
	path := fmt.Sprintf("/api/v1/dashboards/%d/visualizations/%d", dashboardID, id)
	resp, err := c.doRequest(ctx, "DELETE", path, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return parseError(resp)
	}
	resp.Body.Close()
	return nil
}

// --- Dashboard templates ---

// ListDashboardTemplates returns the template catalog. It is unpaginated: the
// catalog is a code-defined constant, not a table.
func (c *Client) ListDashboardTemplates(ctx context.Context) ([]DashboardTemplate, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/dashboards/templates", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var result struct {
		Templates []DashboardTemplate `json:"templates"`
	}
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result.Templates, nil
}

// InstantiateDashboardTemplate builds a template. Panels the organization
// cannot feed are dropped rather than created blank, and every drop comes back
// in the result's Skipped list.
func (c *Client) InstantiateDashboardTemplate(ctx context.Context, input InstantiateDashboardTemplateInput) (*DashboardTemplateResult, error) {
	resp, err := c.doRequest(ctx, "POST", "/api/v1/dashboards/templates", input, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}
	var result DashboardTemplateResult
	if err := decodeResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}
