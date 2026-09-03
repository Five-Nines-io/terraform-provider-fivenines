data "fivenines_rabbitmq_nodes" "all" {
  instance_id = fivenines_instance.broker.id
}

# Either alarm blocks publishes broker-wide. `alarm` collapses a null column to
# false; the raw booleans stay three-valued (null = not reported, not false).
output "nodes_in_alarm" {
  value = [for n in data.fivenines_rabbitmq_nodes.all.rabbitmq_nodes : n.name if n.alarm]
}

# fd_percent is null when the limit is unknown, never 0 -- reading a null as
# zero would report a node at its file-descriptor ceiling as idle.
output "fd_pressure" {
  value = {
    for n in data.fivenines_rabbitmq_nodes.all.rabbitmq_nodes :
    n.name => n.fd_percent if n.fd_percent != null
  }
}
