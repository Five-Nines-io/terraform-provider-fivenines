data "fivenines_raid_arrays" "all" {
  instance_id = fivenines_instance.db.id
}

# Read status, not the three booleans: mdstat reports a SET of words, so
# healthy and degraded are both true for a degrading array that still serves.
# status applies the precedence failed > degraded > healthy.
output "arrays_needing_attention" {
  value = [
    for a in data.fivenines_raid_arrays.all.raid_arrays :
    a.device if contains(["failed", "degraded"], a.status)
  ]
}

# The progress percentages are null when no such operation is running -- a 0
# would read as a rebuild stuck at the start.
output "rebuilding" {
  value = {
    for a in data.fivenines_raid_arrays.all.raid_arrays :
    a.device => a.rebuild_status_percent if a.rebuild_status_percent != null
  }
}

# There is no stale field on this collector, so compare last_synced_at against
# the instance's own last check-in yourself.
output "raid_last_reported_at" {
  value = data.fivenines_raid_arrays.all.collector.last_reported_at
}
