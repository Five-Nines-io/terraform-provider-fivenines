# A broker one of your agent hosts watches from inside your network.
# The broker itself is free — the topic monitors under it are the billable unit.
resource "fivenines_mqtt_broker" "factory" {
  name = "Factory-floor Mosquitto"
  host = "mqtt.internal"
  port = 8883
  tls  = true

  # Write-only: the API never returns either one, so `username_set` and
  # `password_set` are what report that a credential is stored. Dropping either
  # from this configuration leaves the stored value alone — clear one from the
  # dashboard.
  username = var.mqtt_username
  password = var.mqtt_password

  # Until a watcher is assigned the broker is inert. The instance must belong to
  # your organization: it is shipped these credentials decrypted.
  watcher_host_id = fivenines_instance.edge_gateway.id
}

# An anonymous broker, polled from an existing agent host.
resource "fivenines_mqtt_broker" "lab" {
  name            = "Lab broker"
  host            = "10.0.4.20"
  watcher_host_id = fivenines_instance.edge_gateway.id
}
