# Mean time to resolve over the last 30 days
data "fivenines_incident_stats" "mttr" {
  metric = "incident_mttr"
  from   = timeadd(timestamp(), "-720h")
}

output "mttr" {
  # "2h 15m" for a report; `value` holds the same figure in seconds
  value = try(data.fivenines_incident_stats.mttr.items[0].formatted, "no incidents resolved")
}

# The instances, monitors and tasks that opened the most incidents this quarter
data "fivenines_incident_stats" "noisiest" {
  metric   = "incident_count"
  group_by = "entity"
  limit    = 10
  from     = timeadd(timestamp(), "-2160h")
}

output "noisiest_entities" {
  value = [for e in data.fivenines_incident_stats.noisiest.items : "${e.name}: ${e.formatted}"]
}

# The incident-count trend, for a chart or an export
data "fivenines_incident_stats" "trend" {
  metric = "incident_count"
  format = "time_series"
  from   = timeadd(timestamp(), "-720h")
}
