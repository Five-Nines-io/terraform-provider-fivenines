resource "fivenines_uptime_monitor" "website" {
  name     = "Production Website"
  protocol = "https"
  url      = "https://example.com"
}

# Lightweight status read, cheap enough to refresh on every plan.
data "fivenines_uptime_monitor_status" "website" {
  id = fivenines_uptime_monitor.website.id
}

output "website_status" {
  # One of: unknown, up, down, paused, recovering.
  value = data.fivenines_uptime_monitor_status.website.status
}

output "website_certificate_expiry" {
  value = data.fivenines_uptime_monitor_status.website.ssl_expires_at
}
