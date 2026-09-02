data "fivenines_smart_devices" "all" {
  instance_id = fivenines_instance.db.id
}

output "failing_drives" {
  value = [for d in data.fivenines_smart_devices.all.smart_devices : d.device if d.failed]
}

# Read verdict_known before failed: a null overall_health means smartctl gave
# NO verdict (RAID controller, USB bridge), never PASSED. Without this,
# "failed = false" quietly means "healthy, or we could not tell".
output "drives_with_no_verdict" {
  value = [
    for d in data.fivenines_smart_devices.all.smart_devices :
    d.device if !d.verdict_known
  ]
}
