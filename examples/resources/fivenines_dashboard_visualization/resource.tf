resource "fivenines_dashboard" "fleet" {
  name = "Fleet health"
}

resource "fivenines_dashboard_section" "compute" {
  dashboard_id = fivenines_dashboard.fleet.id
  name         = "Compute"
  position     = 0
}

# A time-series panel over instances. The metric decides which `targets` list
# the panel binds - read `target_kind` back to see which one that is.
resource "fivenines_dashboard_visualization" "cpu" {
  dashboard_id = fivenines_dashboard.fleet.id
  section      = fivenines_dashboard_section.compute.name
  title        = "CPU usage"
  description  = "Per core, 5 minute average"
  metric       = "cpu_usage"
  chart_type   = "line"

  layout = {
    x = 0
    y = 0
    w = 12
    h = 6
  }

  targets = {
    hosts = [fivenines_instance.web.id, fivenines_instance.db.id]
  }

  options = {
    incident_overlay = true
  }
}

# A single-value panel. A stat or gauge needs exactly one dimension, or the
# reducer collapses opposite dimensions into a meaningless number.
resource "fivenines_dashboard_visualization" "memory" {
  dashboard_id = fivenines_dashboard.fleet.id
  section      = fivenines_dashboard_section.compute.name
  title        = "Memory"
  metric       = "memory_usage"
  chart_type   = "gauge"

  layout = {
    x = 12
    y = 0
    w = 6
    h = 6
  }

  targets = {
    hosts = [fivenines_instance.web.id]
  }

  options = {
    reducer   = "avg"
    max       = 100
    sparkline = true
  }
}

# A multi-dimensional metric: network_bytes is recv + sent, and `dimensions`
# picks which of them to chart. Leaving it unset selects them all.
resource "fivenines_dashboard_visualization" "network" {
  dashboard_id = fivenines_dashboard.fleet.id
  section      = fivenines_dashboard_section.compute.name
  title        = "Network throughput"
  metric       = "network_bytes"
  chart_type   = "area"

  targets = {
    hosts = [fivenines_instance.web.id]
  }

  options = {
    dimensions = ["recv", "sent"]
    stacked    = true
  }
}

# Uptime monitors bind a different target kind. Attaching the wrong one is
# rejected at apply time with the model's own message.
resource "fivenines_dashboard_visualization" "api_latency" {
  dashboard_id = fivenines_dashboard.fleet.id
  title        = "API response time"
  metric       = "uptime_response_time_ms"
  chart_type   = "line"

  targets = {
    uptime_monitors = [fivenines_uptime_monitor.api.id]
  }
}

# An org-wide metric takes no entities at all, so it needs no targets block.
resource "fivenines_dashboard_visualization" "incidents" {
  dashboard_id = fivenines_dashboard.fleet.id
  title        = "Open incidents"
  metric       = "incident_count"
  chart_type   = "stat"

  options = {
    reducer = "last"
  }
}

# A panel in the ungrouped grid at the top: no `section` at all.
resource "fivenines_dashboard_visualization" "noisiest_hosts" {
  dashboard_id = fivenines_dashboard.fleet.id
  title        = "Busiest hosts"
  metric       = "load_average"
  chart_type   = "top_n"

  targets = {
    hosts = [fivenines_instance.web.id, fivenines_instance.db.id]
  }

  options = {
    reducer = "max"
    limit   = 5
  }
}
