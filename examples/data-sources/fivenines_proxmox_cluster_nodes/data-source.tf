data "fivenines_proxmox_clusters" "all" {}

# The nodes of one cluster, reached by cluster id rather than through a host you
# already know. Exactly one row per physical node however many hosts report the
# cluster: only the cluster's authoritative reporter writes them.
data "fivenines_proxmox_cluster_nodes" "prod" {
  cluster_id = one(data.fivenines_proxmox_clusters.all.proxmox_clusters[*].id)
}

# status = "offline" IS THE CLUSTER'S VIEW of a node -- the surviving nodes
# cannot see it. That is the signal worth alerting on, and it is distinct from
# both "the agent stopped reporting" (stale) and "we cannot reach the Proxmox
# API at all" (the cluster's unreachable_reporter_count).
output "offline_nodes" {
  value = [
    for n in data.fivenines_proxmox_cluster_nodes.prod.proxmox_cluster_nodes :
    n.name if n.status == "offline"
  ]
}

# `stale` here is a FIXED 10-minute window -- not the organization threshold the
# guests data source uses, so the same age can read stale there and fresh here.
data "fivenines_proxmox_cluster_nodes" "current" {
  cluster_id = one(data.fivenines_proxmox_clusters.all.proxmox_clusters[*].id)
  stale      = false
}
