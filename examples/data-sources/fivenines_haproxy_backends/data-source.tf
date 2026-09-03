data "fivenines_haproxy_backends" "all" {
  instance_id = fivenines_instance.lb.id
}

# Compare against status_word, never against status: HAProxy writes the member
# tally and transition source inline ("UP 1/3", "MAINT (via web/srv1)").
output "backends_down" {
  value = [
    for b in data.fivenines_haproxy_backends.all.haproxy_backends :
    b.name if b.status_word == "DOWN"
  ]
}

# The three booleans do not partition: NOLB is in none of them, so an
# exhaustive if/else over up/down/maint reports it as something it is not.
output "backend_states" {
  value = {
    for b in data.fivenines_haproxy_backends.all.haproxy_backends :
    b.name => b.status
  }
}
