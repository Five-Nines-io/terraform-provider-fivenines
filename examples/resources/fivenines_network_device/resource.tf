# SNMPv2c device
resource "fivenines_network_device" "switch" {
  name            = "Core Switch"
  ip_address      = "192.168.1.1"
  device_type     = "switch"
  polling_interval = 60
  snmp_version    = "v2c"
  snmp_community  = "public"
}

# SNMPv3 device with auth+priv
resource "fivenines_network_device" "router" {
  name              = "Edge Router"
  ip_address        = "10.0.0.1"
  device_type       = "router"
  polling_interval  = 30
  snmp_version      = "v3"
  snmp_username     = "monitoring"
  snmp_security_level = "auth_priv"
  snmp_auth_protocol  = "sha"
  snmp_auth_password  = var.snmp_auth_password
  snmp_priv_protocol  = "aes"
  snmp_priv_password  = var.snmp_priv_password

  # Poll from a specific instance
  polling_host_id = fivenines_instance.poller.id
}

# SNMP credentials are not verified at create/update time — the API stores them
# without a connectivity test. Reachability only becomes known after the device
# has been polled: re-run `terraform plan` (or `terraform refresh`) until
# last_polled_at advances, then read these back.
output "switch_reachability" {
  value = {
    status               = fivenines_network_device.switch.status
    last_polled_at       = fivenines_network_device.switch.last_polled_at
    consecutive_failures = fivenines_network_device.switch.consecutive_failures
    last_error_type      = fivenines_network_device.switch.last_error_type
    last_error_message   = fivenines_network_device.switch.last_error_message
  }
}
