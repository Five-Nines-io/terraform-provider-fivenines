# Every Proxmox guest in the organization, across every cluster -- the
# fleet-wide counterpart of fivenines_proxmox_cluster_guests. No deduplication
# is needed: guests are cluster-scoped and only the authoritative reporter
# writes them, so each appears exactly once however many hosts report a cluster.
data "fivenines_organization_proxmox_guests" "all" {}

# Every stopped VM in the fleet, filtered server-side
data "fivenines_organization_proxmox_guests" "stopped_vms" {
  status     = "stopped"
  guest_type = "vm"
}

# Group the fleet back into clusters. proxmox_cluster_id is the uuid
# fivenines_proxmox_clusters publishes as `id` -- NOT the cluster_key a metric
# query takes.
output "guests_per_cluster" {
  value = {
    for id in distinct(data.fivenines_organization_proxmox_guests.all.organization_proxmox_guests[*].proxmox_cluster_id) :
    id => length([
      for g in data.fivenines_organization_proxmox_guests.all.organization_proxmox_guests :
      g.vmid if g.proxmox_cluster_id == id
    ])
  }
}

# Or narrow to one cluster server-side instead of filtering in HCL.
#
# An unknown cluster uuid is a 400 naming it, never an empty list -- the
# organization's clusters are a closed vocabulary, and an empty list would be an
# all-clear for a cluster nobody can see. So interpolate a real id rather than
# pasting a literal.
data "fivenines_organization_proxmox_guests" "one_cluster" {
  proxmox_cluster_id = one([
    for c in data.fivenines_proxmox_clusters.all.proxmox_clusters :
    c.id if c.name == "pve-prod"
  ])
}
