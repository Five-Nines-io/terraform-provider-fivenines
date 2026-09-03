# The sensor inventory, not the readings -- join to the `temperature` metric
# series on sensor_id for the actual values.
data "fivenines_temperature_sensors" "cpu" {
  instance_id = fivenines_instance.web.id
  category    = "cpu"
}

output "cpu_sensor_labels" {
  value = {
    for s in data.fivenines_temperature_sensors.cpu.temperature_sensors :
    s.sensor_id => coalesce(s.label, s.sensor_id)
  }
}

# A null threshold means the chip declares none -- not zero, and not "no
# limit". An alert rule that treats null as 0 fires on every one of them.
output "sensors_with_a_critical_point" {
  value = {
    for s in data.fivenines_temperature_sensors.cpu.temperature_sensors :
    s.sensor_id => s.critical_threshold if s.critical_threshold != null
  }
}
