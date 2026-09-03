# What the agent last reported it can actually COLLECT on this host, as opposed
# to what the operator switched on (the *_enabled arguments on
# fivenines_instance). It is what tells "Redis monitoring is enabled" from
# "Redis monitoring is enabled and working".
data "fivenines_instance_capability_status" "web" {
  instance_id = fivenines_instance.web.id
}

# THE HONESTY RULE: an empty capabilities map means "the agent has not
# reported", never "nothing is supported". Read `reported` rather than testing
# the map for emptiness -- and note the rule does NOT key off updated_at, which
# the server stamps on every check-in whether or not the agent sent a capability
# block. An older agent checking in every 60s presents as an empty map with a
# timestamp seconds old.
output "capabilities_known" {
  value = data.fivenines_instance_capability_status.web.reported
}

# Safe because the map is always present, never null.
output "docker_collectable" {
  value = lookup(data.fivenines_instance_capability_status.web.capabilities, "docker", false)
}

# `pending` is what the operator enabled that the agent cannot yet collect, and
# `reasons` says why -- "zpool not found in PATH" rather than a silent gap.
output "blocked_capabilities" {
  value = {
    for name in data.fivenines_instance_capability_status.web.pending :
    name => lookup(data.fivenines_instance_capability_status.web.reasons, name, "no reason reported")
  }
}
