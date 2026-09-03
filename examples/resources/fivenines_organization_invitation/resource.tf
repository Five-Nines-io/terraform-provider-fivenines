# Sends the invitation email and holds a seat until it is accepted or revoked.
resource "fivenines_organization_invitation" "new_hire" {
  email = "newhire@acme.com"
  role  = "member"
}

resource "fivenines_organization_invitation" "new_admin" {
  email = "lead@acme.com"
  role  = "admin"
}

# Invitations last 7 days. `status` reads "expired" after that, and "accepted"
# once the person joins — at which point the invitation has done its job and
# fivenines_organization_member takes over managing them.
output "new_hire_status" {
  value = fivenines_organization_invitation.new_hire.status
}

# Re-sending is an action rather than a state, so it is not an attribute here.
# To issue a fresh invitation and a fresh acceptance link:
#
#   terraform apply -replace=fivenines_organization_invitation.new_hire
#
# Onboard a whole batch from one list. Check seats first: an unaccepted invite
# holds a seat, so the seat limit is reached at invite time, not on acceptance.
locals {
  new_hires = {
    "alice@acme.com" = "member"
    "bob@acme.com"   = "admin"
  }
}

resource "fivenines_organization_invitation" "batch" {
  for_each = local.new_hires

  email = each.key
  role  = each.value
}
