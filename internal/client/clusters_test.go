package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The list walks must follow the pagination envelope rather than stopping at the
// first page. This is the failure TODOS.md records: when the meta fields were
// renamed the old struct decoded to zeros, every walk truncated at one page, and
// the unit tests stayed green because the fixtures spoke the old names.
func TestListCephClusters_Paginates(t *testing.T) {
	var gotPages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		gotPages = append(gotPages, page)
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("expected per_page=100, got %q", r.URL.Query().Get("per_page"))
		}
		body := map[string]interface{}{
			"ceph_clusters": []interface{}{
				map[string]interface{}{"fsid": "cluster-" + page, "name": "c" + page},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 2, "total_count": 2, "per_page": 100},
		}
		if page == "2" {
			body["meta"] = map[string]int{"current_page": 2, "total_pages": 2, "total_count": 2, "per_page": 100}
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	clusters, err := c.ListCephClusters(context.Background(), CephClusterListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters across 2 pages, got %d", len(clusters))
	}
	if len(gotPages) != 2 || gotPages[0] != "1" || gotPages[1] != "2" {
		t.Errorf("unexpected page sequence: %v", gotPages)
	}
}

func TestListProxmoxClusters_Paginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		total := 2
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_clusters": []interface{}{
				map[string]interface{}{"id": "cluster-" + page, "cluster_key": "pve-" + page},
			},
			"meta": map[string]interface{}{
				"current_page": mustAtoi(t, page), "total_pages": total,
				"total_count": total, "per_page": 100,
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	clusters, err := c.ListProxmoxClusters(context.Background(), ProxmoxClusterListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters across 2 pages, got %d", len(clusters))
	}
}

// An empty list must be [] rather than nil, so the data source layer never has
// to re-establish the distinction.
func TestListClusters_EmptyIsNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "ceph_clusters"
		if r.URL.Path == "/api/v1/proxmox_clusters" {
			key = "proxmox_clusters"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			key:    []interface{}{},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	ceph, err := c.ListCephClusters(context.Background(), CephClusterListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ceph == nil {
		t.Error("expected a non-nil empty slice for ceph clusters")
	}
	prox, err := c.ListProxmoxClusters(context.Background(), ProxmoxClusterListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prox == nil {
		t.Error("expected a non-nil empty slice for proxmox clusters")
	}
}

// The filter renderers must omit an unset option entirely: an unknown or empty
// query parameter is a 400, never a silently unfiltered list.
func TestClusterListOptions_Query(t *testing.T) {
	yes, no := true, false

	ceph := CephClusterListOptions{Query: "prod", Promoted: &no, Stale: &yes, Order: "fsid"}.query()
	for key, want := range map[string]string{"q": "prod", "promoted": "false", "stale": "true", "order": "fsid"} {
		if got := ceph.Get(key); got != want {
			t.Errorf("ceph: expected %s=%q, got %q", key, want, got)
		}
	}
	for _, key := range []string{"updated_since", "direction"} {
		if _, ok := ceph[key]; ok {
			t.Errorf("ceph: expected %s to be omitted", key)
		}
	}

	prox := ProxmoxClusterListOptions{Standalone: &no, Direction: "desc"}.query()
	if got := prox.Get("standalone"); got != "false" {
		t.Errorf("proxmox: expected standalone=false to be sent, got %q", got)
	}
	for _, key := range []string{"q", "updated_since", "stale", "order"} {
		if _, ok := prox[key]; ok {
			t.Errorf("proxmox: expected %s to be omitted", key)
		}
	}

	subs := StatusPageSubscriberListOptions{Status: "pending"}.query()
	if got := subs.Get("status"); got != "pending" {
		t.Errorf("subscribers: expected status=pending, got %q", got)
	}
	for _, key := range []string{"q", "updated_since", "order", "direction"} {
		if _, ok := subs[key]; ok {
			t.Errorf("subscribers: expected %s to be omitted", key)
		}
	}
}

// The child inventory routes share the untyped row walker with ListInventory but
// carry no collector block, so the walk must not depend on one.
func TestListProxmoxClusterRows(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath, not Path: Path is already decoded, so it cannot show
		// whether the id reached the wire escaped.
		gotPath = r.URL.EscapedPath()
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_storages": []interface{}{
				map[string]interface{}{"id": "s-1", "name": "local-zfs", "active": true, "pool": nil},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	rows, err := c.ListProxmoxClusterRows(context.Background(), "cluster uuid", "storages", "proxmox_storages",
		map[string]string{"active": "true", "q": ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The cluster id is path-escaped rather than interpolated raw.
	if gotPath != "/api/v1/proxmox_clusters/cluster%20uuid/storages" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if got := gotQuery.Get("active"); got != "true" {
		t.Errorf("expected active=true, got %q", got)
	}
	// An empty filter value must not reach the API: it is a 400, not a no-op.
	if _, ok := gotQuery["q"]; ok {
		t.Error("expected an empty filter value to be omitted")
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// A JSON null stays a nil entry so the data source can keep null distinct
	// from zero.
	if v, ok := rows[0]["pool"]; !ok || v != nil {
		t.Errorf("expected a nil pool entry, got %v", rows[0]["pool"])
	}
}

func TestListOrganizationProxmoxGuests(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"proxmox_guests": []interface{}{map[string]interface{}{"id": "g-1", "vmid": "1042"}},
			"meta":           map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	rows, err := c.ListOrganizationProxmoxGuests(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/proxmox_guests" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
}

func TestGetInstanceCapabilityStatus(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"capability_status": map[string]interface{}{
				"capabilities": map[string]bool{"docker": true},
				"pending":      []string{"zfs"},
				"reasons":      map[string]string{"zfs": "zpool not found in PATH"},
				"updated_at":   "2026-02-01T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	status, err := c.GetInstanceCapabilityStatus(context.Background(), "host uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/instances/host%20uuid/capability_status" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if !status.Capabilities["docker"] || len(status.Pending) != 1 {
		t.Errorf("unexpected status: %+v", status)
	}
	if status.UpdatedAt == nil || *status.UpdatedAt != "2026-02-01T00:00:00Z" {
		t.Errorf("unexpected updated_at: %v", status.UpdatedAt)
	}
}

func TestListStatusPageSubscribers_Paginates(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		page := r.URL.Query().Get("page")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subscribers": []interface{}{
				map[string]interface{}{"id": mustAtoi(t, page), "email": "a@b.example", "status": "confirmed"},
			},
			"meta": map[string]interface{}{
				"current_page": mustAtoi(t, page), "total_pages": 2, "total_count": 2, "per_page": 100,
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	subs, err := c.ListStatusPageSubscribers(context.Background(), 7, StatusPageSubscriberListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscribers across 2 pages, got %d", len(subs))
	}
	for _, p := range gotPaths {
		if p != "/api/v1/status_pages/7/subscribers" {
			t.Errorf("unexpected path: %s", p)
		}
	}
}

// A 403 has to stay recognisable through the client so the data source can give
// it the permission-specific diagnostic PII access needs.
func TestListStatusPageSubscribers_ForbiddenIsDetectable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "Forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.ListStatusPageSubscribers(context.Background(), 7, StatusPageSubscriberListOptions{})
	if err == nil {
		t.Fatal("expected an error for a 403")
	}
	if !IsForbidden(err) {
		t.Errorf("expected IsForbidden to recognise the error, got %v", err)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		t.Fatalf("bad page %q: %v", s, err)
	}
	return n
}
