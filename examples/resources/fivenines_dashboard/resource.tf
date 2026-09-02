# An empty dashboard. Sections and panels are their own resources, because the
# API refuses to reconcile them from the dashboard endpoint: a rename must never
# be able to silently delete a section it did not mention.
resource "fivenines_dashboard" "fleet" {
  name        = "Fleet health"
  description = "Everything on one screen"
}

# A dashboard built from the gallery instead. The panels a template creates are
# NOT managed by Terraform: the API drops panels this organization cannot feed
# and reports them as a warning at apply time, so treat this as a starting point
# rather than a declaration. Changing template_slug replaces the dashboard.
# The fivenines_dashboard_templates data source lists the slugs on offer.
resource "fivenines_dashboard" "postgres" {
  name          = "PostgreSQL"
  template_slug = "postgresql"
}

# Audit which dashboards anyone holding a link can read without signing in.
# Sharing is an action, not a managed field.
output "fleet_is_public" {
  value = fivenines_dashboard.fleet.shared
}
