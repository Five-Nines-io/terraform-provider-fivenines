# Availability for the last 30 days (the default window)
data "fivenines_uptime" "api" {
  monitors = [fivenines_uptime_monitor.api.id]
}

output "api_sla" {
  # "99.98%", ready to paste into status page copy
  value = try(data.fivenines_uptime.api.items[0].formatted, "no data")
}

# A relative window: metrics are re-read on every plan, so pinning absolute
# dates freezes a report that was meant to roll forward.
data "fivenines_uptime" "fleet" {
  hosts = [for i in fivenines_instance.web : i.id]
  from  = timeadd(timestamp(), "-720h") # 30 days
}

# Gate an apply on the SLA: refuse to touch production while the fleet is
# below target. `availability` is the worst host, and null when nothing
# reported — so a missing measurement blocks rather than reading as 0%.
resource "fivenines_status_page" "public" {
  name   = "Service Status"
  public = true

  items {
    item_type = "UptimeMonitor"
    item_id   = fivenines_uptime_monitor.api.id
  }

  lifecycle {
    precondition {
      condition     = coalesce(data.fivenines_uptime.fleet.availability, 0) >= 99.9
      error_message = "Fleet availability is below the 99.9% target; stabilize before publishing."
    }
  }
}

# Collapse the fleet to a single number instead of one row per host
data "fivenines_uptime" "worst_host" {
  hosts          = [for i in fivenines_instance.web : i.id]
  collapse_scope = true
  aggregation    = "min"
}
