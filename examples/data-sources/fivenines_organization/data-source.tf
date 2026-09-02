data "fivenines_organization" "current" {}

output "plan" {
  value = data.fivenines_organization.current.plan
}

# Pre-flight for automated onboarding: an unaccepted invitation holds a seat, so
# check for room before inviting rather than parsing the resulting error.
#
# seats_total is null on an unmetered plan, so it is coalesced rather than
# interpolated directly — a null in a string template is an error on exactly the
# plans that have no cap.
output "seats" {
  value = format(
    "%d/%s used, %d free",
    data.fivenines_organization.current.seats_used,
    coalesce(tostring(data.fivenines_organization.current.seats_total), "unmetered"),
    data.fivenines_organization.current.seats_remaining,
  )
}
