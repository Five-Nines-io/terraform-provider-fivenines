# Identity is (backend, name): a server name is unique only within its backend.
data "fivenines_haproxy_servers" "web_pool" {
  instance_id = fivenines_instance.lb.id
  backend     = "web"
}

output "down_members" {
  value = [
    for s in data.fivenines_haproxy_servers.web_pool.haproxy_servers :
    "${s.backend}/${s.name}" if s.status_word == "DOWN"
  ]
}

# check_status says WHY (L7OK, L4CON, L7STS/503). HAProxy runs no checks on a
# member under maintenance, so an empty value is "not checked", never "passed".
output "check_verdicts" {
  value = {
    for s in data.fivenines_haproxy_servers.web_pool.haproxy_servers :
    "${s.backend}/${s.name}" => s.check_status
  }
}
