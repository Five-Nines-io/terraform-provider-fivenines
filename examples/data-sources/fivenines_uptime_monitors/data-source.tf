# Every monitor in the organization.
data "fivenines_uptime_monitors" "all" {}

# Filters are optional and combine.
data "fivenines_uptime_monitors" "failing_https" {
  status   = "down"
  protocol = "https"
}

# Substring match on the monitor name. It does not search the URL or hostname.
data "fivenines_uptime_monitors" "api" {
  query     = "api"
  order     = "name"
  direction = "asc"
}

# Only monitors touched since a given point in time.
data "fivenines_uptime_monitors" "recently_changed" {
  updated_since = "2026-01-01T00:00:00Z"
}

output "down_urls" {
  value = data.fivenines_uptime_monitors.failing_https.monitors[*].url
}
