# Every Ceph cluster in the organization
data "fivenines_ceph_clusters" "all" {}

# The clusters still waiting for an operator to vouch for them. A cluster is
# promoted automatically once two or more fresh hosts confirm the fsid; below
# that it is usually a phantom -- a stale ceph.conf on a cloned image -- so it
# publishes NO health at all.
data "fivenines_ceph_clusters" "unpromoted" {
  promoted = false
}

# Reading `health` alone would be a bug. It is derived at read from the FRESH
# reporters, so a cluster nobody is watching reads STALE rather than going
# quiet, and an unpromoted one reads null rather than green. Branch on all
# three, and note there is deliberately no server-side `health` filter: the
# verdict is a fold over the reporter set, and a second implementation of it in
# SQL is how a "what is broken" query starts disagreeing with the field.
# NOTE the `c.health == null ? false : ...` rather than `c.health != null && ...`:
# HCL's `&&` is EAGER, so both sides are evaluated even when the left is false,
# and `contains()` rejects a null argument outright. Only the `? :` conditional
# short-circuits. An unpromoted cluster has a null `health`, so the guard is not
# hypothetical -- without it one phantom cluster fails the whole plan.
output "needs_attention" {
  value = [
    for c in data.fivenines_ceph_clusters.all.ceph_clusters : c.fsid
    if c.promoted && (c.stale || (c.health == null ? false : contains(["HEALTH_WARN", "HEALTH_ERR", "UNKNOWN", "STALE"], c.health)))
  ]
}

# `unreachable_reporter_count` is a per-host badge, not a cluster alarm -- one
# monitor's network blip is not an outage. It becomes the whole story only when
# it equals fresh_reporter_count, which is when health reads UNKNOWN.
output "fully_unreachable" {
  value = [
    for c in data.fivenines_ceph_clusters.all.ceph_clusters : c.name
    if c.fresh_reporter_count > 0 && c.unreachable_reporter_count == c.fresh_reporter_count
  ]
}
