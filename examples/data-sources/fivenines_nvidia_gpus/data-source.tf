data "fivenines_nvidia_gpus" "all" {
  instance_id = fivenines_instance.gpu_box.id
}

# name lives only on this row: the metric series is labelled by index and
# cannot tell you which card is which.
output "gpu_inventory" {
  value = {
    for g in data.fivenines_nvidia_gpus.all.nvidia_gpus : g.index => g.name
  }
}

# Null is "nvidia-smi did not return it", never zero -- a consumer card
# reports no power_limit, a passthrough card reports no fan_speed.
output "gpus_reporting_power_limits" {
  value = [
    for g in data.fivenines_nvidia_gpus.all.nvidia_gpus :
    g.index if g.power_limit != null
  ]
}
