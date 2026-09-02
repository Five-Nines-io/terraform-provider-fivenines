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
	var all []map[string]interface{}
	var status *CollectorStatus

	for page := 1; ; page++ {
		query := url.Values{}
		for k, v := range filters {
			query.Set(k, v)
		}
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", strconv.Itoa(inventoryPerPage))
		path := fmt.Sprintf("/api/v1/instances/%s/%s?%s", url.PathEscape(instanceID), collector, query.Encode())

		resp, err := c.doRequest(ctx, "GET", path, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, nil, parseError(resp)
		}

		var envelope map[string]json.RawMessage
		if err := decodeResponse(resp, &envelope); err != nil {
			return nil, nil, fmt.Errorf("decoding response: %w", err)
		}

		// The collector block is identical on every page; keep the first.
		if raw, ok := envelope["collector"]; ok && status == nil {
			var cs CollectorStatus
			if err := json.Unmarshal(raw, &cs); err != nil {
				return nil, nil, fmt.Errorf("decoding collector block: %w", err)
			}
			status = &cs
		}

		// The association name doubles as the response key and the route
		// segment, so the collector argument addresses both.
		var rows []map[string]interface{}
		if raw, ok := envelope[collector]; ok {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&rows); err != nil {
				return nil, nil, fmt.Errorf("decoding %s rows: %w", collector, err)
			}
		}
		all = append(all, rows...)

		var meta PaginationMeta
		if raw, ok := envelope["meta"]; ok {
			if err := json.Unmarshal(raw, &meta); err != nil {
				return nil, nil, fmt.Errorf("decoding pagination meta: %w", err)
			}
		}
		// Same walk-termination rule as every other index in this client: an
		// unrecognised envelope over-fetches by one page rather than truncating
		// silently, and maxListPages bounds a server that ignores `page`.
		more, err := morePages(len(rows), meta, page)
		if err != nil {
			return nil, nil, err
		}
		if !more {
			break
		}
	}

	return all, status, nil
}
