# Basic public status page.
# `items` is a list attribute, so it takes `= [...]`, and the order of the list
# is the order things appear on the page.
resource "fivenines_status_page" "public" {
  name        = "Service Status"
  description = "Current status of our services"
  public      = true
  uptime      = true

  # items is a list attribute, not a block: assign it with `=`.
  items = [
    {
      item_type = "UptimeMonitor"
      item_id   = fivenines_uptime_monitor.api.id
    },
    {
      item_type = "UptimeMonitor"
      item_id   = fivenines_uptime_monitor.website.id
    },
    {
      item_type = "Host"
      item_id   = fivenines_instance.web.id
    },
  ]
}

# Status page with custom domain, footer and branding
resource "fivenines_status_page" "branded" {
  name                      = "ACME Status"
  description               = "ACME Corp service status"
  public                    = true
  uptime                    = true
  theme_variant             = "dark"
  custom_domain_enabled     = true
  custom_domain             = "status.acme.com"
  custom_footer_enabled     = true
  custom_footer             = "© 2026 ACME Corp. All rights reserved."
  incidents_history_enabled = true
  contact_url               = "https://acme.com/support"

  # Hides the Subscribe button without dropping existing subscribers.
  subscriptions_enabled = false

  # Keep the page out of search results, badges included.
  search_indexing_enabled = false

  # Days shorter than two minutes of downtime still count as green.
  uptime_green_tolerance_seconds = 120
  uptime_window_days             = 90

  # Base64-encoded PNG, at most 1 MB decoded. Requires a white-label plan.
  # The API never returns it; the stored image is exposed as `logo_url`.
  logo = filebase64("${path.module}/logo.png")

  # Sections have to be declared before an item can reference one.
  sections = ["Core services", "Edge"]

  items = [
    {
      item_type     = "UptimeMonitor"
      item_id       = fivenines_uptime_monitor.api.id
      display_label = "Public API"
      description   = "REST and GraphQL endpoints"
      section       = "Core services"
    },
    {
      item_type = "Host"
      item_id   = fivenines_instance.web.id
      section   = "Edge"
    },
  ]
}

output "acme_logo_url" {
  value = fivenines_status_page.branded.logo_url
}

# Emptying a page: an explicit `[]` removes every item and section. Dropping the
# attributes instead would leave whatever the page already has in place.
resource "fivenines_status_page" "empty" {
  name     = "Placeholder"
  sections = []
  items    = []
}
