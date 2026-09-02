# Every argument is a server-side filter — omit them all to list everything.
data "fivenines_incidents" "all" {}

# Open incidents for one host, oldest first.
data "fivenines_incidents" "host_open" {
  host_id   = fivenines_instance.web.id
  status    = "open"
  direction = "asc"
}

# Incidents whose ACTIVE WINDOW overlaps last week. `to` is exclusive, so the
# range is [from, to) — and an incident that opened before `from` and is still
# open matches, because this is the incident's duration, not its creation time.
data "fivenines_incidents" "last_week" {
  from = "2026-08-24T00:00:00Z"
  to   = "2026-08-31T00:00:00Z"
}

output "open_incidents" {
  value = [for inc in data.fivenines_incidents.host_open.incidents : inc.title]
}

# Which incidents are visible on the organization's status pages.
output "published_incidents" {
  value = [for inc in data.fivenines_incidents.all.incidents : inc.title if inc.public]
}
