data "fivenines_proxmox_clusters" "all" {}

data "fivenines_proxmox_cluster_storages" "prod" {
  cluster_id = one(data.fivenines_proxmox_clusters.all.proxmox_clusters[*].id)
}

# ONE LOGICAL POOL APPEARS ONCE PER NODE, and that is the useful shape rather
# than duplication: `active` is per node, so a shared NFS mount really can be up
# on one node and down on another. Grouping by `name` alone collapses exactly
# the failure worth seeing.
# `node_name` is nullable and a null inside a "${...}" template is a hard plan
# error, so it gets a floor. `s.active == false` needs no guard: a null compares
# unequal rather than erroring, and a null `active` means "never reported",
# which is `stale`'s question, not this one.
output "inactive_mounts" {
  value = [
    for s in data.fivenines_proxmox_cluster_storages.prod.proxmox_cluster_storages :
    "${coalesce(s.node_name, "unknown-node")}:${s.name}" if s.active == false
  ]
}

# `pool` IS NOT `name`. `name` is the PVE storage id an operator types; `pool`
# is the backing dataset ("rpool/data"), and `zpool_root` is its first segment
# -- the actual zpool, and the key that correlates this row to a ZFS pool on the
# node's own host.
# `if s.zfs_backed` would be a hard error on a null (a for-expression's `if`
# clause must not be null), so compare explicitly.
output "zfs_backed_storages" {
  value = {
    for s in data.fivenines_proxmox_cluster_storages.prod.proxmox_cluster_storages :
    "${coalesce(s.node_name, "unknown-node")}:${s.name}" => s.zpool_root if s.zfs_backed == true
  }
}
