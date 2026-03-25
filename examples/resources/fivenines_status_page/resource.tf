# Basic public status page
resource "fivenines_status_page" "public" {
  name        = "Service Status"
  description = "Current status of our services"
  public      = true
  uptime      = true

  items {
    item_type = "UptimeMonitor"
    item_id   = fivenines_uptime_monitor.api.id
  }

  items {
    item_type = "UptimeMonitor"
    item_id   = fivenines_uptime_monitor.website.id
  }

  items {
    item_type = "Host"
    item_id   = fivenines_instance.web.id
  }
}

# Status page with custom domain and footer
resource "fivenines_status_page" "branded" {
  name                    = "ACME Status"
  description             = "ACME Corp service status"
  public                  = true
  uptime                  = true
  theme_variant           = "dark"
  custom_domain_enabled   = true
  custom_domain           = "status.acme.com"
  custom_footer_enabled   = true
  custom_footer           = "© 2026 ACME Corp. All rights reserved."
  incidents_history_enabled = true

  items {
    item_type = "UptimeMonitor"
    item_id   = fivenines_uptime_monitor.api.id
  }
}
