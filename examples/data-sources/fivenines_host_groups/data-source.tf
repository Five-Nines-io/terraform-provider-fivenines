# Every host group, in the dashboard's display order
data "fivenines_host_groups" "all" {}

# Server-side filter on a case-insensitive substring of the group name
data "fivenines_host_groups" "production" {
  query = "prod"
}

# Wire a group id into a resource without hardcoding the integer
output "production_group_id" {
  value = one([
    for g in data.fivenines_host_groups.production.host_groups :
    g.id if lower(g.name) == "production"
  ])
}
