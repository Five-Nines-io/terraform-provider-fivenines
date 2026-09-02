# Every host group
data "fivenines_host_groups" "all" {}

# Server-side filter on a case-insensitive substring of the group name
data "fivenines_host_groups" "production" {
  q = "prod"
}

output "production_group_id" {
  value = one([
    for g in data.fivenines_host_groups.production.host_groups :
    g.id if lower(g.name) == "production"
  ])
}
