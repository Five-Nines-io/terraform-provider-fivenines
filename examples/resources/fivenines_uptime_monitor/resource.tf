data "fivenines_probe_regions" "all" {}

resource "fivenines_uptime_monitor" "website" {
  name                = "Production Website"
  protocol            = "https"
  url                 = "https://example.com"
  http_method         = "GET"
  interval_seconds    = 60
  timeout_seconds     = 10
  confirmation_count  = 2
  follow_redirects    = true
  expected_status_codes = [200]
  probe_region_ids    = data.fivenines_probe_regions.all.regions[*].id
}

resource "fivenines_uptime_monitor" "api" {
  name               = "API Health Check"
  protocol           = "https"
  url                = "https://api.example.com/health"
  http_method        = "GET"
  interval_seconds   = 30
  timeout_seconds    = 5
  confirmation_count = 3
  keyword            = "healthy"
}

resource "fivenines_uptime_monitor" "database" {
  name     = "Database TCP Check"
  protocol = "tcp"
  hostname = "db.example.com"
  port     = 5432
}

resource "fivenines_uptime_monitor" "dns" {
  name            = "DNS A Record"
  protocol        = "dns"
  hostname        = "example.com"
  dns_record_type = "A"

  # Up to 50 records, 2048 characters each. Set to [] to stop pinning an
  # expectation without deleting the monitor.
  dns_expected_records = ["93.184.216.34"]
}

# The protocol can be changed in place; set whatever the new protocol requires
# in the same plan (https needs url, tcp needs hostname + port, icmp needs
# hostname, dns needs dns_record_type).
resource "fivenines_uptime_monitor" "staging" {
  name     = "Staging Website"
  protocol = "https"
  url      = "https://staging.example.com"

  # Suspends checks without deleting the monitor or its history.
  paused = true
}
