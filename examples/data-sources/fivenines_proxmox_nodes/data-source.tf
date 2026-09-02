data "fivenines_proxmox_nodes" "all" {
  instance_id = fivenines_instance.pve_reporter.id
}

# status = "offline" is the CLUSTER's view of a node -- the surviving nodes
# cannot see it. That is distinct from "the agent stopped reporting" (stale)
# and from "we cannot reach the Proxmox API at all" (the collector block).
output "offline_nodes" {
  value = [
    for n in data.fivenines_proxmox_nodes.all.proxmox_nodes :
    n.name if n.status == "offline"
  ]
}

# An instance whose Proxmox reporter has aged out returns an empty list with
# collector.enabled still true -- freshness, not configuration.
output "proxmox_last_reported_at" {
  value = data.fivenines_proxmox_nodes.all.collector.last_reported_at
}
