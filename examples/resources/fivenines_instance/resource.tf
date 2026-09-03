# A minimal instance: register the host, then install the agent (see
# fivenines_enrollment_token for scripted installs).
resource "fivenines_instance" "web_server" {
  display_name = "Web Server (Production)"
  description  = "Primary web tier"

  host_group_id = fivenines_host_group.production.id
}

# The full monitoring configuration is writable too. Collector settings not
# listed here keep their server-side values — including choices made in the
# dashboard — so manage the ones you care about and leave the rest alone.
resource "fivenines_instance" "db_server" {
  display_name = "PostgreSQL Primary"
  cluster_name = "eu-west-1"

  smart_storage_health_enabled = true
  systemd_enabled              = true

  postgresql_enabled  = true
  postgresql_host     = "127.0.0.1"
  postgresql_port     = 5432
  postgresql_user     = "monitoring"
  postgresql_database = "app"

  # Write-only: the API never returns a credential, so `postgresql_password_set`
  # is what reports that one is stored. Dropping this attribute later leaves the
  # stored value alone — clear one from the dashboard.
  postgresql_password = var.postgresql_monitoring_password
}
