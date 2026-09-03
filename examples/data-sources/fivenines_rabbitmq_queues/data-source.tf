data "fivenines_rabbitmq_queues" "all" {
  instance_id = fivenines_instance.broker.id
}

# Use the published predicates rather than rebuilding the rule from the raw
# columns: a null consumers reads as ZERO here, so a queue with no reported
# consumer count and a positive depth really is starved.
output "starved_queues" {
  value = [
    for q in data.fivenines_rabbitmq_queues.all.rabbitmq_queues :
    "${q.vhost}/${q.name}" if q.starved
  ]
}

output "backlogged_queues" {
  value = {
    for q in data.fivenines_rabbitmq_queues.all.rabbitmq_queues :
    "${q.vhost}/${q.name}" => q.backlog_detail if q.backlogged
  }
}
