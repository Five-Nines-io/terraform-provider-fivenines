package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// clusterPerPage is the maximum per_page the cluster indexes accept.
const clusterPerPage = 100

// --- Ceph ---

// CephCluster is one organization-owned Ceph cluster.
//
// THE HEALTH IS DERIVED AT READ, from the cluster's fresh reporters, and is
// published with its provenance. Health alone cannot say whether anyone is
// watching, so four fields travel together and a consumer needs all of them:
// Health, Stale, FreshReporterCount and UnreachableReporterCount. Collapsing
// them into a bare status string turns "we have not heard from a single
// reporter" into a confident verdict.
//
// There is deliberately no id: the row's uuid addresses nothing, and the fsid
// is the identifier the dashboard URL, this endpoint's path segment and
// metrics/query's `clusters:` scope all take.
type CephCluster struct {
	// FSID is the identity everywhere. NOT globally unique: two organizations
	// monitoring one shared cluster carry the same fsid.
	FSID string `json:"fsid"`
	// Name is the operator-set name if there is one, else the short fsid.
	Name string `json:"name"`
	// ConfiguredName is the raw column, null when nobody named the cluster.
	ConfiguredName *string `json:"configured_name"`
	// Promoted is whether the cluster is past the anti-phantom gate. A
	// single-reporter cluster is usually a stale ceph.conf on a cloned image.
	Promoted   bool    `json:"promoted"`
	PromotedAt *string `json:"promoted_at"`
	// Health is a WIDER vocabulary than Ceph's own: HEALTH_OK / HEALTH_WARN /
	// HEALTH_ERR come from the cluster, while STALE (nobody has checked in) and
	// UNKNOWN (fresh reporters exist, none could read a status) are absences
	// rather than verdicts. NULL for an unpromoted cluster -- the product
	// refuses to vouch for a phantom's health, so a null here is "ask
	// `promoted`", never "healthy".
	Health *string `json:"health"`
	// Stale means no reporter has checked in inside the organization's offline
	// threshold. Published even while Health is null.
	Stale bool `json:"stale"`
	// LastSeenAt is the most recent scrape from ANY reporter.
	LastSeenAt *string `json:"last_seen_at"`
	// ReporterCount is hosts that have ever reported this cluster;
	// FreshReporterCount is how many the verdict was derived FROM.
	ReporterCount      int64 `json:"reporter_count"`
	FreshReporterCount int64 `json:"fresh_reporter_count"`
	// UnreachableReporterCount is fresh hosts that could not reach the cluster
	// this cycle -- a per-host badge, not a cluster alarm. It becomes the whole
	// story only when it equals FreshReporterCount, which is when Health reads
	// UNKNOWN.
	UnreachableReporterCount int64 `json:"unreachable_reporter_count"`
	// AuthoritativeHostID is the elected reporter, null whenever the cluster is
	// stale (there is nothing to elect).
	AuthoritativeHostID *string `json:"authoritative_host_id"`
	// Reporters is the per-host breakdown. The index OMITS the key rather than
	// sending an empty array; only GET /ceph_clusters/{fsid} populates it.
	Reporters []CephClusterReporter `json:"reporters"`
	CreatedAt *string               `json:"created_at"`
	UpdatedAt *string               `json:"updated_at"`
}

// CephClusterReporter is one host's view of one Ceph cluster.
//
// THIS IS PROVENANCE, NOT A SECOND VERDICT: LastHealth is what that host's
// `ceph status` last returned, and a host that has gone silent keeps its last
// reading forever. Read Fresh first -- only fresh rows contribute to the
// cluster's Health.
type CephClusterReporter struct {
	HostID   string  `json:"host_id"`
	HostName *string `json:"host_name"`
	// Fresh is measured against the organization's offline threshold, the same
	// one instance liveness uses.
	Fresh bool `json:"fresh"`
	// Authoritative marks the elected writer of the per-entity series. Exactly
	// one reporter carries it, and none does while the cluster is stale.
	Authoritative bool `json:"authoritative"`
	Reachable     bool `json:"reachable"`
	// The five *_ok flags report CALL success, not agreement between hosts.
	StatusOK bool `json:"status_ok"`
	DfOK     bool `json:"df_ok"`
	TreeOK   bool `json:"tree_ok"`
	OsdDfOK  bool `json:"osd_df_ok"`
	PerfOK   bool `json:"perf_ok"`
	// CompletenessScore is the precedence key for the authoritative election,
	// published with its denominator so the number reads without the formula.
	CompletenessScore    int64 `json:"completeness_score"`
	MaxCompletenessScore int64 `json:"max_completeness_score"`
	// LastHealth is THIS HOST's last reported string, which is not the
	// cluster's.
	LastHealth   *string `json:"last_health"`
	LastError    *string `json:"last_error"`
	LastSyncedAt *string `json:"last_synced_at"`
	ReceivedAt   *string `json:"received_at"`
}

// CephClusterListOptions narrows GET /api/v1/ceph_clusters.
//
// There is no health filter, deliberately: the verdict is a fold over the
// reporter set, and a second implementation of it in SQL is how a "show me what
// is broken" query starts disagreeing with the health each row publishes. Stale
// is filterable because it has an exact SQL twin.
type CephClusterListOptions struct {
	Query        string
	UpdatedSince string
	Promoted     *bool
	Stale        *bool
	Order        string
	Direction    string
}

func (o CephClusterListOptions) query() url.Values {
	q := url.Values{}
	for key, value := range map[string]string{
		"q":             o.Query,
		"updated_since": o.UpdatedSince,
		"order":         o.Order,
		"direction":     o.Direction,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if o.Promoted != nil {
		q.Set("promoted", strconv.FormatBool(*o.Promoted))
	}
	if o.Stale != nil {
		q.Set("stale", strconv.FormatBool(*o.Stale))
	}
	return q
}

// ListCephClusters returns the organization's Ceph clusters. The rows carry the
// derived health and its provenance; the per-host reporter breakdown is on
// GetCephCluster only.
func (c *Client) ListCephClusters(ctx context.Context, opts CephClusterListOptions) ([]CephCluster, error) {
	return listAllPages[CephCluster](ctx, c, "/api/v1/ceph_clusters", "ceph_clusters", opts.query(), clusterPerPage, 0)
}

// GetCephCluster returns one cluster plus the per-host reporters the index
// omits. The path segment is the fsid, not a row id.
//
// No ETag is read: this row's updated_at only moves when the cluster's own
// columns change, while the health and freshness move constantly without it, so
// a conditional request could answer "unchanged" for a cluster that had gone
// from HEALTH_OK to STALE.
func (c *Client) GetCephCluster(ctx context.Context, fsid string) (*CephCluster, error) {
	var result struct {
		CephCluster CephCluster `json:"ceph_cluster"`
	}
	if err := c.getJSON(ctx, "/api/v1/ceph_clusters/"+url.PathEscape(fsid), &result); err != nil {
		return nil, err
	}
	return &result.CephCluster, nil
}

// --- Proxmox ---

// ProxmoxCluster is one organization-owned Proxmox VE cluster.
//
// Quorate is THREE-VALUED and the null is load-bearing -- see the field. The
// node / guest / storage rollups are counts of the whole inventory and are
// pointers because the API omits them on a response that did not compute them,
// never sending zero: a null rollup is "not counted", and 0 would read as "this
// cluster has no nodes".
type ProxmoxCluster struct {
	// ID is what this endpoint's path takes. NOT what metrics/query takes --
	// that is ClusterKey. Sending the wrong one returns an empty series rather
	// than an error, which is why both are published.
	ID string `json:"id"`
	// ClusterKey is the corosync cluster name, or `standalone:<host_id>`.
	ClusterKey string  `json:"cluster_key"`
	Name       string  `json:"name"`
	Version    *string `json:"version"`
	// Standalone is a single node that never joined a corosync cluster.
	// Quorate is null for every one of these by definition.
	Standalone bool `json:"standalone"`
	// Quorate is THREE-VALUED. true -- a fresh reporter saw quorum (corosync
	// cannot grant it to two partitions at once, so any fresh reporter claiming
	// it settles the question). false -- fresh reporters read cluster status and
	// none has quorum. null -- UNKNOWN, never "lost": the cluster is
	// standalone, or every fresh reporter failed to read cluster status, or the
	// cluster is stale. A consumer that treats null as false fires a
	// lost-quorum alarm on a monitoring outage.
	Quorate *bool `json:"quorate"`
	// Stale means no reporter has checked in inside the organization's offline
	// threshold.
	Stale                    bool    `json:"stale"`
	LastSeenAt               *string `json:"last_seen_at"`
	ReporterCount            int64   `json:"reporter_count"`
	FreshReporterCount       int64   `json:"fresh_reporter_count"`
	UnreachableReporterCount int64   `json:"unreachable_reporter_count"`
	// AuthoritativeHostID is the elected reporter and the SINGLE writer of the
	// child inventory -- one cluster has one set of nodes, guests and storages
	// however many hosts report it. Null whenever the cluster is stale.
	AuthoritativeHostID *string `json:"authoritative_host_id"`
	// The rollups are deliberately NOT freshness filtered; Stale is the field
	// that qualifies them. NodesOnline is the CLUSTER's own view of its nodes.
	NodesTotal    *int64 `json:"nodes_total"`
	NodesOnline   *int64 `json:"nodes_online"`
	GuestsTotal   *int64 `json:"guests_total"`
	GuestsRunning *int64 `json:"guests_running"`
	// StorageTotal counts storage rows, which are keyed PER NODE -- one
	// datacenter-wide pool is counted once per node it is mounted on.
	StorageTotal  *int64 `json:"storage_total"`
	StorageActive *int64 `json:"storage_active"`
	// Reporters is the per-host breakdown, present on the show route only. The
	// index OMITS the key rather than sending an empty array.
	Reporters []ProxmoxClusterReporter `json:"reporters"`
	CreatedAt *string                  `json:"created_at"`
	UpdatedAt *string                  `json:"updated_at"`
}

// ProxmoxClusterReporter is one host's view of one Proxmox cluster.
//
// THIS IS PROVENANCE, NOT A SECOND VERDICT: QuorateSeen is what that host's own
// API call returned, and during a split brain the minority partition's reporter
// really does report false while the cluster is quorate.
type ProxmoxClusterReporter struct {
	HostID   string  `json:"host_id"`
	HostName *string `json:"host_name"`
	// Fresh is measured against the organization's offline threshold.
	Fresh bool `json:"fresh"`
	// Authoritative marks the elected writer of the child inventory and the
	// cluster series.
	Authoritative bool `json:"authoritative"`
	// Reachable is the red "can't reach" state, distinct from "not reporting"
	// (Fresh false).
	Reachable bool `json:"reachable"`
	// The four *_ok flags report CALL success, not agreement between hosts.
	ClusterOK            bool  `json:"cluster_ok"`
	NodesOK              bool  `json:"nodes_ok"`
	GuestsOK             bool  `json:"guests_ok"`
	StorageOK            bool  `json:"storage_ok"`
	CompletenessScore    int64 `json:"completeness_score"`
	MaxCompletenessScore int64 `json:"max_completeness_score"`
	// QuorateSeen is THIS HOST's own reading, null for a standalone node and
	// whenever the host could not read cluster status.
	QuorateSeen     *bool   `json:"quorate_seen"`
	NodesOnlineSeen *int64  `json:"nodes_online_seen"`
	NodesTotalSeen  *int64  `json:"nodes_total_seen"`
	LastError       *string `json:"last_error"`
	LastSyncedAt    *string `json:"last_synced_at"`
	ReceivedAt      *string `json:"received_at"`
}

// ProxmoxClusterListOptions narrows GET /api/v1/proxmox_clusters.
//
// There is no quorate filter, for the reason the Ceph list has no health one:
// the verdict is a fold over the reporter set, and a second implementation in
// SQL is how a filter starts disagreeing with the field.
type ProxmoxClusterListOptions struct {
	Query        string
	UpdatedSince string
	Standalone   *bool
	Stale        *bool
	Order        string
	Direction    string
}

func (o ProxmoxClusterListOptions) query() url.Values {
	q := url.Values{}
	for key, value := range map[string]string{
		"q":             o.Query,
		"updated_since": o.UpdatedSince,
		"order":         o.Order,
		"direction":     o.Direction,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if o.Standalone != nil {
		q.Set("standalone", strconv.FormatBool(*o.Standalone))
	}
	if o.Stale != nil {
		q.Set("stale", strconv.FormatBool(*o.Stale))
	}
	return q
}

// ListProxmoxClusters returns the organization's Proxmox clusters with their
// quorum verdict, provenance and rollups. The per-host reporter breakdown is on
// GetProxmoxCluster only.
func (c *Client) ListProxmoxClusters(ctx context.Context, opts ProxmoxClusterListOptions) ([]ProxmoxCluster, error) {
	return listAllPages[ProxmoxCluster](ctx, c, "/api/v1/proxmox_clusters", "proxmox_clusters", opts.query(), clusterPerPage, 0)
}

// GetProxmoxCluster returns one cluster plus the per-host reporters the index
// omits. The path segment is the cluster's uuid, not its cluster_key.
//
// No ETag is read, for the reason GetCephCluster gives: the derived fields move
// without touching this row's updated_at.
func (c *Client) GetProxmoxCluster(ctx context.Context, id string) (*ProxmoxCluster, error) {
	var result struct {
		ProxmoxCluster ProxmoxCluster `json:"proxmox_cluster"`
	}
	if err := c.getJSON(ctx, "/api/v1/proxmox_clusters/"+url.PathEscape(id), &result); err != nil {
		return nil, err
	}
	return &result.ProxmoxCluster, nil
}

// ListProxmoxClusterRows walks a Proxmox cluster's child inventory --
// GET /api/v1/proxmox_clusters/{id}/{segment} for nodes, guests and storages.
//
// Rows are untyped for the reason ListInventory's are: the three shapes share
// only their envelope, and each data source maps them onto its own field table.
// segment is both the path segment and the response key's suffix, which is why
// the caller passes the response key separately -- the route says `nodes` while
// the envelope says `proxmox_nodes`.
func (c *Client) ListProxmoxClusterRows(ctx context.Context, clusterID, segment, key string, filters map[string]string) ([]map[string]interface{}, error) {
	path := fmt.Sprintf("/api/v1/proxmox_clusters/%s/%s", url.PathEscape(clusterID), url.PathEscape(segment))
	return c.listRowPages(ctx, path, key, filters, clusterPerPage, nil)
}

// ListOrganizationProxmoxGuests walks GET /api/v1/proxmox_guests, the fleet-wide
// guest inventory across every cluster.
//
// Distinct from ListInventory(instanceID, "proxmox_guests", ...) and from
// ListProxmoxClusterRows: those reach one cluster, this one enumerates them all.
func (c *Client) ListOrganizationProxmoxGuests(ctx context.Context, filters map[string]string) ([]map[string]interface{}, error) {
	return c.listRowPages(ctx, "/api/v1/proxmox_guests", "proxmox_guests", filters, clusterPerPage, nil)
}

// getJSON performs a GET and decodes a 200 body into target. Shared by every
// single-object read in this client that has no ETag to return -- the cluster
// routes, capability status and the subscriber index. Those endpoints publish
// none because their payloads are derived at read: an ETag would go on claiming
// "unchanged" while the derived fields moved underneath it.
func (c *Client) getJSON(ctx context.Context, path string, target interface{}) error {
	resp, err := c.doRequest(ctx, "GET", path, nil, nil)
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

// rowID implements rowIdentifier. The fsid is this endpoint's identity, and it is
// what a paginated walk could serve twice.
func (c CephCluster) rowID() string { return c.FSID }

// rowID implements rowIdentifier. ID, not ClusterKey: ID is what this endpoint's
// rows are keyed by.
func (p ProxmoxCluster) rowID() string { return p.ID }
