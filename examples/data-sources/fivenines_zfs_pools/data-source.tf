data "fivenines_zfs_pools" "all" {
  instance_id = fivenines_instance.storage.id
}

output "degraded_pools" {
  value = [for p in data.fivenines_zfs_pools.all.zfs_pools : p.name if p.problem]
}

# scrub_errors is null when nobody has ever checked and 0 when a scrub
# completed clean. Reading the null as 0 turns "unverified" into "verified".
output "pools_never_scrubbed" {
  value = [
    for p in data.fivenines_zfs_pools.all.zfs_pools :
    p.name if p.scrub_errors == null
  ]
}

output "tank_vdev_tree" {
  value = {
    for p in data.fivenines_zfs_pools.all.zfs_pools :
    p.name => jsondecode(p.vdev_tree) if p.vdev_tree != null
  }
}
