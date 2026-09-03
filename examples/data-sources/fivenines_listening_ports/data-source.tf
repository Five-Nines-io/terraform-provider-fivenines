# Everything listening, loopback sockets included and flagged
data "fivenines_listening_ports" "all" {
  instance_id = fivenines_instance.web.id
}

# Only the sockets reachable off the host -- the set a security review wants
data "fivenines_listening_ports" "exposed" {
  instance_id = fivenines_instance.web.id
  loopback    = false
}

# protocol, address and stack are null on an agent that did not classify them,
# so keep them as attributes rather than interpolating them into a string.
output "exposed_ports" {
  value = [
    for p in data.fivenines_listening_ports.exposed.listening_ports :
    ({ port = p.port, protocol = p.protocol, address = p.address })
  ]
}

# These are snapshot entries with no timestamps, so freshness comes from the
# collector block rather than from a per-row column.
output "ports_last_reported_at" {
  value = data.fivenines_listening_ports.exposed.collector.last_reported_at
}
