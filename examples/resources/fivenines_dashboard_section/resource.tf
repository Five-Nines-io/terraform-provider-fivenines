resource "fivenines_dashboard" "fleet" {
  name = "Fleet health"
}

# Sections render in `position` order, zero-based. Leave position unset to
# append at the bottom.
resource "fivenines_dashboard_section" "compute" {
  dashboard_id = fivenines_dashboard.fleet.id
  name         = "Compute"
  position     = 0
}

resource "fivenines_dashboard_section" "storage" {
  dashboard_id = fivenines_dashboard.fleet.id
  name         = "Storage"
  position     = 1
  collapsed    = true
}

# Panels reference their section by NAME, which is what makes a dashboard
# definition portable between organizations - and referencing the attribute is
# also what tells Terraform to create the section first.
resource "fivenines_dashboard_visualization" "cpu" {
  dashboard_id = fivenines_dashboard.fleet.id
  section      = fivenines_dashboard_section.compute.name
  title        = "CPU usage"
  metric       = "cpu_usage"
  chart_type   = "line"

  targets = {
    hosts = [fivenines_instance.web.id]
  }
}
