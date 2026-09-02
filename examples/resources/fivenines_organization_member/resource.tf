# Onboarding is two resources, and they belong to two different applies.
#
# The invitation goes first. It sends the email and holds a seat, and that is the
# whole of the first apply — a member resource for the same address cannot be in
# this configuration yet, because nothing in Terraform can wait for a human to
# click a link. Declaring both at once guarantees a failed first apply, after the
# invitation has already gone out.
resource "fivenines_organization_invitation" "new_hire" {
  email = "newhire@acme.com"
  role  = "member"
}

# Once "newhire@acme.com" has accepted, add the member resource in a SECOND
# apply. It adopts the membership they now hold and brings its role under
# Terraform; it does not create them. Applying it early fails telling you so.
#
# resource "fivenines_organization_member" "new_hire" {
#   email = "newhire@acme.com"
#   role  = "member"
# }

# Promote someone to admin. The API refuses changing your own role, so this
# cannot be the identity behind the provider's API key.
resource "fivenines_organization_member" "lead" {
  email = "lead@acme.com"
  role  = "admin"
}

# Offboarding: delete this block (or run `terraform apply -destroy -target`) and
# the next apply removes the membership AND deletes the user account, which also
# destroys every API token that person owned.
#
# Two things to check before you do:
#
#   1. The provider's own API key must not belong to the person being removed —
#      the token dies with the account, and the API refuses self-removal anyway.
#   2. Reassign what they covered in the same apply, so the organization is not
#      left without a second admin.
#
# Terraform pre-validates the removal with a dry-run request during `plan`, so a
# refusal (yourself, the owner, a read-only key) surfaces before any of it runs.
resource "fivenines_organization_member" "departing_admin" {
  email = "departing@acme.com"
  role  = "admin"
}

# The replacement, promoted in the same apply that removes the admin above.
resource "fivenines_organization_member" "replacement_admin" {
  email = "replacement@acme.com"
  role  = "admin"
}
