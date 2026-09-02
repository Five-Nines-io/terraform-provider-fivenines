# These rows are the CLUSTER's, not this instance's: every instance reporting
# the cluster returns the same rows with the same ids. Deduplicate on id if you
# read more than one instance in a cluster.
data "fivenines_proxmox_guests" "all" {
  instance_id = fivenines_instance.pve_reporter.id
}

output "stopped_guests" {
  value = [
    for g in data.fivenines_proxmox_guests.all.proxmox_guests :
    "${g.guest_type}/${g.vmid}" if g.status == "stopped"
  ]
}

# A guest migrates, so node_name is where it lives right now rather than a
# stable attribute of it. Stale rows are listed and flagged, never hidden.
output "guest_placement" {
  value = {
    for g in data.fivenines_proxmox_guests.all.proxmox_guests :
    g.vmid => ({ node = g.node_name, stale = g.stale })
  }
}
