data "fivenines_proxmox_storages" "all" {
  instance_id = fivenines_instance.pve_reporter.id
}

# One logical pool appears once per node, and active is per node: a shared NFS
# mount really can be up on one node and down on another. Grouping by name
# alone collapses exactly the failure worth seeing.
output "inactive_storage_by_node" {
  value = [
    for s in data.fivenines_proxmox_storages.all.proxmox_storages :
    "${coalesce(s.node_name, "unknown")}/${s.name}" if s.active == false
  ]
}

# zpool_root correlates a ZFS-backed storage to a pool in fivenines_zfs_pools.
output "zfs_backed_storages" {
  value = {
    for s in data.fivenines_proxmox_storages.all.proxmox_storages :
    "${coalesce(s.node_name, "unknown")}/${s.name}" => s.zpool_root if s.zfs_backed
  }
}
