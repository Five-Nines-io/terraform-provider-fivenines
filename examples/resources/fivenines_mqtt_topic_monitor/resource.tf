# Freshness: fire when nothing live has arrived on a matched topic for 5 minutes.
# Retained messages never count as fresh, so a dead sensor still goes stale.
resource "fivenines_mqtt_topic_monitor" "temperature" {
  mqtt_broker_id      = fivenines_mqtt_broker.factory.id
  topic_filter        = "sensors/+/temperature"
  stale_after_seconds = 300
}

# Payload expectation: alert while the last will and testament reads "offline".
# An exact topic (no wildcard) is the only kind that can alert on a device that
# was already silent when the monitor was created.
resource "fivenines_mqtt_topic_monitor" "pump_status" {
  mqtt_broker_id = fivenines_mqtt_broker.factory.id
  topic_filter   = "devices/pump-1/status"
  match_kind     = "exact"
  expected_value = "online"
}

# Both checks, reading a key out of a JSON payload. A dotted path digs nested
# objects; a bare key reads the top level.
resource "fivenines_mqtt_topic_monitor" "pump_battery" {
  mqtt_broker_id      = fivenines_mqtt_broker.factory.id
  topic_filter        = "devices/pump-1/telemetry"
  stale_after_seconds = 900
  match_kind          = "json_key"
  json_key            = "battery.level"
  expected_value      = "ok"
}

# Topics are generated in fleets, not curated one at a time.
resource "fivenines_mqtt_topic_monitor" "cell" {
  for_each = toset(["cell-a", "cell-b", "cell-c"])

  mqtt_broker_id      = fivenines_mqtt_broker.factory.id
  topic_filter        = "plant/${each.key}/heartbeat"
  stale_after_seconds = 60
  capture_payload     = false
}
