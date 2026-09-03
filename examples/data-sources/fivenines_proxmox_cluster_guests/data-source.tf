data "fivenines_proxmox_clusters" "all" {}

# The VMs and containers on one cluster
data "fivenines_proxmox_cluster_guests" "prod" {
  cluster_id = one(data.fivenines_proxmox_clusters.all.proxmox_clusters[*].id)
}

# Only the containers, filtered server-side
data "fivenines_proxmox_cluster_guests" "containers" {
  cluster_id = one(data.fivenines_proxmox_clusters.all.proxmox_clusters[*].id)
  guest_type = "lxc"
  status     = "running"
}

# `state_changed_at` is what turns "it is stopped" into "it has been stopped
# since 04:10", and it is the field the guest-status trigger reads.
output "stopped_since" {
  value = {
    for g in data.fivenines_proxmox_cluster_guests.prod.proxmox_cluster_guests :
    g.vmid => g.state_changed_at if g.status == "stopped"
  }
}

# A GUEST MIGRATES: vmid is unique cluster-wide and the row is keyed on cluster
# + vmid, so node_name is where the guest lives RIGHT NOW rather than a stable
# attribute of it. Do not persist this as though it were identity.
output "guest_placement" {
  value = {
    for g in data.fivenines_proxmox_cluster_guests.prod.proxmox_cluster_guests :
    g.vmid => g.node_name
  }
}
