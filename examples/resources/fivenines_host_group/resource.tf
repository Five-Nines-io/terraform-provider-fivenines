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

# Hosts are put INTO a group from the instance side: the group's id is what
# fivenines_instance.host_group_id expects, and referencing it here is also what
# orders the two on destroy.
resource "fivenines_instance" "web" {
  display_name  = "web-1"
  host_group_id = fivenines_host_group.production.id
}

# Deleting a group only ungroups its hosts — the hosts themselves are never
# removed. The API also drops a group on its own once its LAST host leaves, so a
# group that has held hosts can disappear outside Terraform; Read treats that as
# a removal and the next apply recreates it. A group that is never populated
# never makes that transition, so it stays put.
#
# The reference above also orders a full destroy: Terraform tears down the
# instance first, so if it was the group's last host the API has already dropped
# the group by the time its own delete runs — a 404 there is treated as done.
