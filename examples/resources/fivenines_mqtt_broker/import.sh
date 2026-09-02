# Import an MQTT broker by its UUID. Stored credentials stay as they are —
# Terraform cannot read them back, so add `username` / `password` to the
# configuration only if you want Terraform to manage (and be able to rotate) them.
terraform import fivenines_mqtt_broker.factory 3f6c1d10-9a5e-4d3d-8e0a-7b2f9c1a4e55
