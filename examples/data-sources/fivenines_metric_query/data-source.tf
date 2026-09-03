# Average CPU usage over the last hour (the default window), one value per host
data "fivenines_metric_query" "cpu" {
  hosts    = [for i in fivenines_instance.web : i.id]
  resource = "cpu_usage"
}

output "busiest_host" {
  value = try(data.fivenines_metric_query.cpu.items[0].name, "no data")
}

# Peak disk usage per partition over the last week, worst partitions first
data "fivenines_metric_query" "disk" {
  hosts       = [fivenines_instance.db.id]
  resource    = "partition_percent"
  aggregation = "max"
  group_by    = "instance_device"
  limit       = 5
  from        = timeadd(timestamp(), "-168h")

  # Ignore the loopback mounts that would otherwise crowd out the ranking
  exclude = {
    device = ["loop0", "loop1"]
  }
}

# The response-time trend behind a monitor, as points rather than one value
data "fivenines_metric_query" "response_time" {
  monitors = [fivenines_uptime_monitor.api.id]
  resource = "uptime_response_time_ms"
  format   = "time_series"
  from     = timeadd(timestamp(), "-24h")
}

output "response_time_points" {
  value = try(data.fivenines_metric_query.response_time.series[0].points, [])
}

# SNMP interface throughput. Network devices poll at 60s or slower, so
# scrape_interval widens the rate window enough to hold two samples.
data "fivenines_metric_query" "switch_traffic" {
  devices         = [fivenines_network_device.switch.id]
  resource        = "network_if_bytes_in"
  group_by        = "if_name"
  scrape_interval = 60
}
