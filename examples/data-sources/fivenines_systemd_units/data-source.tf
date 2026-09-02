# Every systemd unit on a host
data "fivenines_systemd_units" "all" {
  instance_id = fivenines_instance.web.id
}

# Ask the API for the failed ones instead of filtering in HCL
data "fivenines_systemd_units" "failed" {
  instance_id  = fivenines_instance.web.id
  active_state = "failed"
  stale        = false # only rows the agent refreshed inside the sync window
}

# An empty list is not an all-clear on its own: the collector may be switched
# off (which deletes the rows) or the agent may be too old to report them.
output "units_are_healthy" {
  value = (
    data.fivenines_systemd_units.failed.collector.enabled &&
    data.fivenines_systemd_units.failed.collector.supported &&
    length(data.fivenines_systemd_units.failed.systemd_units) == 0
  )
}

output "failed_unit_names" {
  value = [for u in data.fivenines_systemd_units.failed.systemd_units : u.name]
}
