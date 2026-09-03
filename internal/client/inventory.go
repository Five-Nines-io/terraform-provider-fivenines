package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// CollectorStatus is the `collector` block every per-instance inventory
// response carries.
//
// It exists because an empty row list is ambiguous three ways -- "this host
// genuinely runs none", "the operator never switched the collector on", or
// "the agent is too old to report these rows at all" -- and disabling a
// collector DELETES its rows, so the wrong reading is a confident all-clear.
// Any automation keying on "no failed units" has to be able to tell that from
// "we are not looking", which is why every inventory data source exposes it.
//
// What it does NOT prove: these fields describe configuration and capability,
// not the outcome of the most recent collection. An enabled, supported
// collector can still fail its tick. Where rows exist, each row's
// last_synced_at / stale is the per-row answer.
type CollectorStatus struct {
	Name              string  `json:"name"`
	Enabled           bool    `json:"enabled"`
	Supported         bool    `json:"supported"`
	Pending           bool    `json:"pending"`
	UnavailableReason *string `json:"unavailable_reason"`
	BlockedReason     *string `json:"blocked_reason"`
	LastReportedAt    *string `json:"last_reported_at"`
}

// inventoryPerPage is the maximum per_page the inventory endpoints accept.
const inventoryPerPage = 100

// ListInventory fetches every page of GET /api/v1/instances/{id}/{collector}
// and returns the rows alongside the response's collector block.
//
// Rows come back as decoded JSON objects rather than typed structs: the twenty
// collectors share only their envelope, and the data source layer maps each
// one onto its own field table. Numbers are decoded as json.Number so an int64
// column survives the round trip intact, and a JSON null stays a nil entry so
// callers can keep null distinct from zero.
func (c *Client) ListInventory(ctx context.Context, instanceID, collector string, filters map[string]string) ([]map[string]interface{}, *CollectorStatus, error) {
	var status *CollectorStatus

	path := fmt.Sprintf("/api/v1/instances/%s/%s", url.PathEscape(instanceID), url.PathEscape(collector))
	// The association name doubles as the response key and the route segment,
	// so the collector argument addresses both.
	rows, err := c.listRowPages(ctx, path, collector, filters, inventoryPerPage, func(envelope map[string]json.RawMessage) error {
		// The collector block is identical on every page; keep the first one
		// that carries it. The `status == nil` guard is what makes this the
		// FIRST rather than the LAST, and reading it on every page rather than
		// only on page 1 is deliberate: a page that omits the block leaves the
		// data source unable to tell an empty row list from a switched-off
		// collector, so tolerating a late block is strictly safer than
		// hard-failing on an early omission.
		raw, ok := envelope["collector"]
		// A JSON null counts as ABSENT, not as a block. encoding/json unmarshals
		// null into a struct as a silent no-op -- no error, every field left at
		// its zero value -- so accepting it would publish `enabled: false`,
		// whose own documentation reads "switching it off deletes the rows, so
		// false fully explains an empty list". That is the confident all-clear
		// this whole block exists to prevent, handed out for a host the API
		// declined to answer about. Absent instead makes the data source raise
		// its "Missing collector block" contract error.
		if !ok || string(raw) == "null" || status != nil {
			return nil
		}
		var cs CollectorStatus
		if err := json.Unmarshal(raw, &cs); err != nil {
			return fmt.Errorf("decoding collector block: %w", err)
		}
		status = &cs
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return rows, status, nil
}

// listRowPages walks every page of a paginated index whose rows live under
// `key`, returning them as decoded JSON objects.
//
// Untyped rows are the point: the collector inventories and the Proxmox
// cluster children share only their envelope, and each data source maps the
// rows onto its own field table. Numbers decode as json.Number so an int64
// column survives the round trip, and a JSON null stays a nil map entry so a
// caller can keep null distinct from zero.
//
// onPage, when non-nil, is handed EVERY page's envelope so a caller can pull a
// sibling block out of it -- ListInventory's `collector`, which qualifies the
// whole row list and is repeated identically on every page. Every page rather
// than just the first: the caller owns the keep-the-first decision, and a
// first-page-only read would turn a single page that omits the block into a
// hard failure for the whole walk.
func (c *Client) listRowPages(
	ctx context.Context,
	basePath, key string,
	filters map[string]string,
	perPage int,
	onPage func(map[string]json.RawMessage) error,
) ([]map[string]interface{}, error) {
	// Non-nil for the reason listAllPages gives: the data source layer maps a
	// nil slice to a NULL Terraform list, and `length(null)` fails a plan, so
	// "the index matched nothing" must not be spelled the same way as "the API
	// refused to answer".
	all := []map[string]interface{}{}

	// Built once: only `page` changes between iterations. An empty value is
	// left out entirely rather than sent -- these endpoints answer 400 to an
	// empty query parameter instead of ignoring it.
	query := url.Values{}
	for k, v := range filters {
		if v != "" {
			query.Set(k, v)
		}
	}
	query.Set("per_page", strconv.Itoa(perPage))

	for page := 1; ; page++ {
		query.Set("page", strconv.Itoa(page))

		resp, err := c.doRequest(ctx, "GET", basePath+"?"+query.Encode(), nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, parseError(resp)
		}

		var envelope map[string]json.RawMessage
		if err := decodeResponse(resp, &envelope); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}

		if onPage != nil {
			if err := onPage(envelope); err != nil {
				return nil, err
			}
		}

		var rows []map[string]interface{}
		if raw, ok := envelope[key]; ok {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&rows); err != nil {
				return nil, fmt.Errorf("decoding %s rows: %w", key, err)
			}
		}
		all = append(all, rows...)

		var meta PaginationMeta
		if raw, ok := envelope["meta"]; ok {
			if err := json.Unmarshal(raw, &meta); err != nil {
				return nil, fmt.Errorf("decoding pagination meta: %w", err)
			}
		}
		// Same walk-termination rule as every other index in this client: an
		// unrecognised envelope over-fetches by one page rather than truncating
		// silently, and maxListPages bounds a server that ignores `page`.
		more, err := morePages(len(rows), meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			return all, nil
		}
	}
}

// listAllPages walks every page of a paginated index whose rows decode into a
// concrete type T, returning them as one slice.
//
// The typed sibling of listRowPages: same envelope, same walk-termination rule,
// same "an empty value stays out of the query" contract. It is a free function
// rather than a method because Go does not allow type parameters on methods.
//
// The returned slice is always non-nil, so an index that matches nothing gives
// callers an empty slice rather than a nil one -- the data source layer maps nil
// to a NULL Terraform list, which fails `length()` in a plan, and "no matches"
// must not read as "the API refused".
func listAllPages[T any](ctx context.Context, c *Client, basePath, key string, filters url.Values, perPage int) ([]T, error) {
	all := []T{}

	// Copied, not written through: url.Values is a map, so setting page and
	// per_page on the caller's value would leave a stale `page` behind for any
	// caller that reuses or hoists its options -- and would panic outright on a
	// nil map.
	query := url.Values{}
	for k, vs := range filters {
		query[k] = append([]string(nil), vs...)
	}
	query.Set("per_page", strconv.Itoa(perPage))

	for page := 1; ; page++ {
		query.Set("page", strconv.Itoa(page))

		var envelope map[string]json.RawMessage
		if err := c.getJSON(ctx, basePath+"?"+query.Encode(), &envelope); err != nil {
			return nil, err
		}

		var rows []T
		if raw, ok := envelope[key]; ok {
			if err := json.Unmarshal(raw, &rows); err != nil {
				return nil, fmt.Errorf("decoding %s rows: %w", key, err)
			}
		}
		all = append(all, rows...)

		var meta PaginationMeta
		if raw, ok := envelope["meta"]; ok {
			if err := json.Unmarshal(raw, &meta); err != nil {
				return nil, fmt.Errorf("decoding pagination meta: %w", err)
			}
		}
		more, err := morePages(len(rows), meta, page)
		if err != nil {
			return nil, err
		}
		if !more {
			return all, nil
		}
	}
}
