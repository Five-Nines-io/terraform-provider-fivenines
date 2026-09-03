# Every Proxmox VE cluster in the organization
data "fivenines_proxmox_clusters" "all" {}

# `quorate` IS THREE-VALUED and the null is load-bearing: null means UNKNOWN,
# never "lost". The cluster is standalone, or every fresh reporter failed to
# read cluster status, or the cluster is stale. Treating null as false fires a
# lost-quorum alarm on a monitoring outage -- the exact false page the
# derivation exists to prevent.
output "lost_quorum" {
  value = [
    for c in data.fivenines_proxmox_clusters.all.proxmox_clusters : c.name
    if c.quorate == false
  ]
}

# The three ways `quorate` can be null, kept apart.
output "quorum_unknown" {
  value = [
    for c in data.fivenines_proxmox_clusters.all.proxmox_clusters :
    {
      name   = c.name
      reason = c.standalone ? "standalone" : (c.stale ? "nobody reporting" : "API unreadable")
    } if c.quorate == null
  ]
}

# A node the CLUSTER cannot see, which is a different signal from "the agent
# stopped reporting" (stale) and from "we cannot reach the API"
# (unreachable_reporter_count).
# BOTH rollups are independently nullable (null means "not counted", never
# zero), and HCL's `&&` is eager -- `c.nodes_total != null && c.nodes_online <
# c.nodes_total` still evaluates the comparison and fails on a null. coalesce
# gives each side a floor, so an uncounted cluster reads 0 < 0 and drops out.
output "clusters_with_offline_nodes" {
  value = [
    for c in data.fivenines_proxmox_clusters.all.proxmox_clusters : c.name
    if coalesce(c.nodes_online, 0) < coalesce(c.nodes_total, 0)
  ]
}

# TWO IDENTIFIERS, NOT INTERCHANGEABLE: `id` is what the per-cluster data
# sources take, `cluster_key` is what a metric query's scope takes. Sending the
# wrong one returns an empty series rather than an error.
output "cluster_keys" {
  value = { for c in data.fivenines_proxmox_clusters.all.proxmox_clusters : c.name => c.cluster_key }
}
