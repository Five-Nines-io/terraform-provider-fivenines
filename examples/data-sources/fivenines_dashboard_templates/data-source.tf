data "fivenines_dashboard_templates" "all" {}

# A template this organization cannot use is listed with a reason rather than
# hidden, so filter on `available` when you only want the ones that would build
# something today.
output "available_templates" {
  value = [
    for t in data.fivenines_dashboard_templates.all.templates :
    "${t.slug} (${t.category}, ${t.panel_count} panels)" if t.available
  ]
}

output "unavailable_templates" {
  value = {
    for t in data.fivenines_dashboard_templates.all.templates :
    t.slug => t.unavailable_reason if !t.available
  }
}

# Feed a slug to the dashboard resource to build one.
resource "fivenines_dashboard" "postgres" {
  name          = "PostgreSQL"
  template_slug = "postgresql"
}
