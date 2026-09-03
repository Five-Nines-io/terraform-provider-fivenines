# Days until each monitored certificate expires, soonest first
data "fivenines_ssl_status" "certs" {
  monitors = [for m in fivenines_uptime_monitor.public : m.id]
}

output "expiring_certificates" {
  # Everything renewing in under three weeks, for a report or a notification
  value = [
    for cert in data.fivenines_ssl_status.certs.items : {
      monitor = cert.name
      expires = cert.formatted
    } if cert.value < 21
  ]
}

# Fail the run while a certificate is close to expiry. Monitors with no
# certificate data (tcp, icmp, dns, or never probed) are omitted rather than
# reported as 0 days, so `soonest_expiry_days` is null when nothing is known —
# and the check below then fails instead of passing on an absent value.
check "certificates_valid" {
  assert {
    condition     = coalesce(data.fivenines_ssl_status.certs.soonest_expiry_days, -1) > 14
    error_message = "A TLS certificate expires within 14 days: ${try(data.fivenines_ssl_status.certs.items[0].name, "unknown monitor")}."
  }
}
