data "fivenines_proxmox_clusters" "all" {
  query = "pve-prod"
}

# One cluster by uuid -- NOT its cluster_key.
data "fivenines_proxmox_cluster" "prod" {
  id = one(data.fivenines_proxmox_clusters.all.proxmox_clusters[*].id)
}

# The per-host breakdown the index omits. Each row is PROVENANCE, not a second
# verdict: during a split brain the minority partition's reporter really does
# report quorate_seen = false while the cluster itself is quorate. The cluster's
# own `quorate` is the only value that answers the question.
output "reporters_seeing_no_quorum" {
  value = [
    for r in data.fivenines_proxmox_cluster.prod.reporters :
    coalesce(r.host_name, r.host_id) if r.fresh && r.quorate_seen == false
  ]
}

# `reachable` false is the red "can't reach the Proxmox API" state, distinct
# from "not reporting" (fresh false).
output "unreachable_reporters" {
  value = [
    for r in data.fivenines_proxmox_cluster.prod.reporters :
    coalesce(r.host_name, r.host_id) if r.fresh && !r.reachable
  ]
}

# The metric series take the cluster KEY, not this id.
data "fivenines_metric_query" "cluster_cpu" {
  metrics          = ["proxmox_cluster_cpu_usage"]
  proxmox_clusters = [data.fivenines_proxmox_cluster.prod.cluster_key]
  from             = "2026-01-01T00:00:00Z"
  to               = "2026-01-02T00:00:00Z"
}
