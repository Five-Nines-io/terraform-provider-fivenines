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
