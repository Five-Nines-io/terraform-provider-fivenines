# By membership ID (the id on the fivenines_organization_members data source —
# not the user ID)
terraform import fivenines_organization_member.lead 7

# Or by email address, which the provider resolves for you
terraform import fivenines_organization_member.lead lead@acme.com
