# Import an MQTT broker by its UUID. Its stored credentials do not come with it:
# the API never returns either one, so `username` and `password` read back as null
# while `username_set` / `password_set` report that a credential exists. Setting
# one in the configuration rotates it; leaving it out leaves the stored value alone.
terraform import fivenines_mqtt_broker.factory 3f6c1d10-9a5e-4d3d-8e0a-7b2f9c1a4e55
