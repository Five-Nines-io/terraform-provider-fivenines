# One cluster by fsid -- the identity everywhere. There is no row id: the fsid
# is what this data source, the dashboard URL and the metrics query all take.
data "fivenines_ceph_cluster" "prod" {
  fsid = "8e4a7d5c-1b2f-4c3d-9e8a-0f1b2c3d4e5f"
}

# The per-host breakdown the fivenines_ceph_clusters index omits. Each row is
# PROVENANCE, not a second verdict: last_health is what THAT host's `ceph
# status` last returned, and a host that has gone silent keeps its last reading
# forever. Read `fresh` before anything else on the row.
output "silent_reporters" {
  value = [
    for r in data.fivenines_ceph_cluster.prod.reporters :
    coalesce(r.host_name, r.host_id) if !r.fresh
  ]
}

# The elected writer of the per-entity series -- the most complete fresh
# reporter. Null-safe: no reporter is authoritative while the cluster is stale.
output "authoritative_host" {
  value = one([
    for r in data.fivenines_ceph_cluster.prod.reporters : r.host_id if r.authoritative
  ])
}

# The metric series live on fivenines_metric_query, scoped by the same fsid.
data "fivenines_metric_query" "ceph_capacity" {
  metrics  = ["ceph_cluster_used_bytes"]
  clusters = [data.fivenines_ceph_cluster.prod.fsid]
  from     = "2026-01-01T00:00:00Z"
  to       = "2026-01-02T00:00:00Z"
}
