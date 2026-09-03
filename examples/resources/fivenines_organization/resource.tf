# The organization an API key belongs to. There is only ever one, so this
# resource has no create and no delete: applying it renames the organization,
# destroying it just drops the resource from state.
resource "fivenines_organization" "this" {
  name = "Acme Corp"
}

# The read side carries the plan and the seat accounting, so a pipeline can
# check for room before it tries to invite somebody.
output "seats_remaining" {
  value = fivenines_organization.this.seats_remaining
}
