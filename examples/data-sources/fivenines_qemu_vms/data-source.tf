# Tombstones are included by default -- this is the only collector that can
# report a deletion, and hiding them is how a sync misses the removal it most
# needed to see.
data "fivenines_qemu_vms" "all" {
  instance_id = fivenines_instance.kvm_host.id
}

output "vanished_vms" {
  value = [
    for v in data.fivenines_qemu_vms.all.qemu_vms :
    ({ uuid = v.vm_uuid, name = v.vm_name, vanished_at = v.vanished_at }) if v.vanished
  ]
}

# Set vanished = false for parity with the dashboard.
data "fivenines_qemu_vms" "present" {
  instance_id = fivenines_instance.kvm_host.id
  vanished    = false
}

# The metric values are freshness-gated: cpu_percent comes back null once
# metrics_fresh is false, rather than as a stale "42%" forever.
output "vm_cpu" {
  value = {
    for v in data.fivenines_qemu_vms.present.qemu_vms :
    v.vm_uuid => v.cpu_percent if v.metrics_fresh
  }
}
