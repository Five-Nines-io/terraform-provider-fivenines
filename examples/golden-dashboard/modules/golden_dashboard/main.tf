terraform {
  required_providers {
    fivenines = {
      source = "Five-Nines-io/fivenines"
    }
  }
}

# The golden dashboard: one layout, instantiated per environment. Everything
# below is declarative, so a panel added here appears in every environment on
# the next apply - which is the point of expressing panels as resources rather
# than instantiating a template.

resource "fivenines_dashboard" "this" {
  name        = "${var.environment} - Fleet health"
  description = var.description
}

resource "fivenines_dashboard_section" "compute" {
  dashboard_id = fivenines_dashboard.this.id
  name         = "Compute"
  position     = 0
}

resource "fivenines_dashboard_section" "availability" {
  dashboard_id = fivenines_dashboard.this.id
  name         = "Availability"
  position     = 1
}

# --- Compute ---

resource "fivenines_dashboard_visualization" "cpu" {
  count = length(var.host_ids) > 0 ? 1 : 0

  dashboard_id = fivenines_dashboard.this.id
  section      = fivenines_dashboard_section.compute.name
  title        = "CPU usage"
  metric       = "cpu_usage"
  chart_type   = "line"

  layout = {
    x = 0
    y = 0
    w = 12
    h = 6
  }

  targets = {
    hosts = var.host_ids
  }

  options = {
    incident_overlay = true
  }
}

resource "fivenines_dashboard_visualization" "memory" {
  count = length(var.host_ids) > 0 ? 1 : 0

  dashboard_id = fivenines_dashboard.this.id
  section      = fivenines_dashboard_section.compute.name
  title        = "Memory usage"
  metric       = "memory_usage"
  chart_type   = "line"

  layout = {
    x = 12
    y = 0
    w = 12
    h = 6
  }

  targets = {
    hosts = var.host_ids
  }
}

resource "fivenines_dashboard_visualization" "load" {
  count = length(var.host_ids) > 0 ? 1 : 0

  dashboard_id = fivenines_dashboard.this.id
  section      = fivenines_dashboard_section.compute.name
  title        = "Busiest hosts"
  metric       = "load_average"
  chart_type   = "top_n"

  layout = {
    x = 0
    y = 6
    w = 24
    h = 6
  }

  targets = {
    hosts = var.host_ids
  }

  options = {
    reducer = "max"
    limit   = 5
  }
}

# --- Availability ---

resource "fivenines_dashboard_visualization" "uptime" {
  count = length(var.uptime_monitor_ids) > 0 ? 1 : 0

  dashboard_id = fivenines_dashboard.this.id
  section      = fivenines_dashboard_section.availability.name
  title        = "Uptime"
  metric       = "monitor_uptime"
  chart_type   = "stat"

  layout = {
    x = 0
    y = 0
    w = 6
    h = 6
  }

  targets = {
    uptime_monitors = var.uptime_monitor_ids
  }

  # The availability metrics reduce ACROSS entities, so they accept only avg,
  # min and max.
  options = {
    reducer = "avg"
  }
}

resource "fivenines_dashboard_visualization" "response_time" {
  count = length(var.uptime_monitor_ids) > 0 ? 1 : 0

  dashboard_id = fivenines_dashboard.this.id
  section      = fivenines_dashboard_section.availability.name
  title        = "Response time"
  metric       = "uptime_response_time_ms"
  chart_type   = "line"

  layout = {
    x = 6
    y = 0
    w = 18
    h = 6
  }

  targets = {
    uptime_monitors = var.uptime_monitor_ids
  }
}

resource "fivenines_dashboard_visualization" "ssl_expiry" {
  count = length(var.uptime_monitor_ids) > 0 ? 1 : 0

  dashboard_id = fivenines_dashboard.this.id
  section      = fivenines_dashboard_section.availability.name
  title        = "Certificate expiry"
  metric       = "monitor_ssl_expiry"
  chart_type   = "table"

  layout = {
    x = 0
    y = 6
    w = 24
    h = 6
  }

  targets = {
    uptime_monitors = var.uptime_monitor_ids
  }
}

# --- Organization-wide ---
#
# Org metrics take no entities at all, so this panel is identical in every
# environment. It sits in the ungrouped grid at the top of the dashboard.

resource "fivenines_dashboard_visualization" "incidents" {
  dashboard_id = fivenines_dashboard.this.id
  title        = "Open incidents"
  metric       = "incident_count"
  chart_type   = "stat"

  layout = {
    x = 0
    y = 0
    w = 6
    h = 4
  }

  options = {
    reducer = "last"
  }
}
