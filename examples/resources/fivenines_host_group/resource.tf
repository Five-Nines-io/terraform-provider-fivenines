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

resource "fivenines_host_group" "lab" {
  name     = "Lab"
  position = 3
}

# Either pin every group, as above, or pin none of them:
#
#   resource "fivenines_host_group" "lab" {
#     name = "Lab"
#   }
#
# An unpinned group lands on top and renumbers the rest, so mixing the two in one
# configuration means the unpinned group pushes the pinned ones out of the slots
# they ask for, and every later plan shows a diff for them.

# Hosts are put INTO a group from the instance side. The provider does not expose
# that attribute yet: fivenines_instance has no host_group_id, so for now assign
# hosts to a group in the FiveNines dashboard or through the API. Once the
# instance resource exposes its full configuration surface the pairing reads:
#
# resource "fivenines_instance" "web" {
#   display_name  = "web-1"
#   host_group_id = fivenines_host_group.production.id
# }
#
# Deleting a group only ungroups its hosts — the hosts themselves are never
# removed. The API also drops a group on its own once its LAST host leaves, so a
# group that has held hosts can disappear outside Terraform; Read treats that as
# a removal and the next apply recreates it. A group that is never populated
# never makes that transition, so it stays put.
