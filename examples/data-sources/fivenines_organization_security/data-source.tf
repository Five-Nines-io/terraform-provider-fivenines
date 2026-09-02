data "fivenines_organization_security" "this" {}

# Read the counters, not just the flag: enforcement only bites a member on their
# next request, so require_two_factor with people still pending is a real state
# and not a compliant one.
output "two_factor_coverage" {
  value = "${data.fivenines_organization_security.this.members_with_two_factor}/${data.fivenines_organization_security.this.members_count} enrolled"
}

check "two_factor_enrollment" {
  assert {
    condition = data.fivenines_organization_security.this.members_pending_two_factor == 0
    error_message = format(
      "%d member(s) have not enrolled a second factor.",
      data.fivenines_organization_security.this.members_pending_two_factor,
    )
  }
}
