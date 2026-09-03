data "fivenines_organization_members" "all" {}

# Per-person two-factor state lives here; the org-wide totals are on the
# fivenines_organization_security data source.
output "members_without_two_factor" {
  value = [
    for m in data.fivenines_organization_members.all.members : m.email
    if !m.two_factor_enabled
  ]
}

output "admins" {
  value = [
    for m in data.fivenines_organization_members.all.members : m.email
    if contains(["owner", "admin"], m.role)
  ]
}
