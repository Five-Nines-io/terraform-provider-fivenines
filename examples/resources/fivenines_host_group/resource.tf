# Group your hosts by environment. Positions are 1-based and shared across the
# whole organization: slotting a group in renumbers the ones below it.
resource "fivenines_host_group" "production" {
  name     = "Production"
  position = 1
}

resource "fivenines_host_group" "staging" {
  name     = "Staging"
  position = 2
}

# Leave position out and the group lands on top of the list.
resource "fivenines_host_group" "lab" {
  name = "Lab"
}

# Hosts join a group from the instance side, through the host_group_id attribute
# that ships with the full fivenines_instance configuration surface:
#
# resource "fivenines_instance" "web" {
#   display_name  = "web-1"
#   host_group_id = fivenines_host_group.production.id
# }
#
# Deleting a group only ungroups its hosts — the hosts themselves are never
# removed. The API also drops a group on its own once its last host leaves, so
# Terraform may find it gone and recreate it on the next apply.
