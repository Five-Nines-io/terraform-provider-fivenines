data "fivenines_docker_containers" "all" {
  instance_id = fivenines_instance.web.id
}

# Restart-looping containers. restarts_last_24h counts genuine crash-restarts
# only, and is a floor for a sustained loop -- treat it as "at least this many".
output "restart_looping" {
  value = [
    for c in data.fivenines_docker_containers.all.docker_containers :
    c.name if c.restarts_last_24h > 3
  ]
}

# health is null when the image defines no HEALTHCHECK -- not a passing verdict.
output "unhealthy" {
  value = [
    for c in data.fivenines_docker_containers.all.docker_containers :
    c.name if c.health == "unhealthy"
  ]
}

# Container rows need agent >= 1.11.2. On an older agent the list is empty.
output "container_rows_unavailable_because" {
  value = data.fivenines_docker_containers.all.collector.unavailable_reason
}
