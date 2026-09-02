# Minimal window: title and a time range, announced to subscribers on create
resource "fivenines_status_page_maintenance_window" "quick_restart" {
  status_page_id = fivenines_status_page.public.id
  title          = "Rolling restart"
  starts_at      = "2026-09-15T02:00:00Z"
  ends_at        = "2026-09-15T02:30:00Z"
}

# Window scoped to specific items, written in the status page timezone.
# Timestamps without a UTC offset are read in that timezone.
resource "fivenines_status_page_maintenance_window" "database_upgrade" {
  status_page_id = fivenines_status_page.public.id
  title          = "Database upgrade"
  body           = <<-EOT
    We are upgrading the primary database cluster.

    The API stays available in **read-only** mode for the duration of the window.
  EOT

  starts_at = "2026-09-20T22:00:00"
  ends_at   = "2026-09-21T02:00:00"

  # Every pair must already be listed on the status page, and must reference the
  # underlying resource — not the status page item ID.
  affected_items = [
    {
      item_type = "UptimeMonitor"
      item_id   = fivenines_uptime_monitor.api.id
    },
    {
      item_type = "Host"
      item_id   = fivenines_instance.db.id
    },
  ]
}

# Keep the window in the status page history when Terraform destroys it:
# `terraform destroy` cancels it instead of deleting it.
resource "fivenines_status_page_maintenance_window" "network_migration" {
  status_page_id    = fivenines_status_page.public.id
  title             = "Network migration"
  starts_at         = "2026-10-01T01:00:00+02:00"
  ends_at           = "2026-10-01T05:00:00+02:00"
  cancel_on_destroy = true
}
