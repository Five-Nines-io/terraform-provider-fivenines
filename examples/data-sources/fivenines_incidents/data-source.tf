# List all incidents
data "fivenines_incidents" "all" {}

# Output open incidents
output "open_incidents" {
  value = [for inc in data.fivenines_incidents.all.incidents : inc if inc.status != "resolved"]
}
