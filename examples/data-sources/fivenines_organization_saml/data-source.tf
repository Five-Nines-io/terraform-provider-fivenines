data "fivenines_organization_saml" "this" {}

# An organization that has never configured SAML answers with the same keys, all
# null or false — so a sweep never has to branch on key presence.
output "sso_enforced" {
  value = data.fivenines_organization_saml.this.enforce_sso
}

# When the IdP signing certificate lapses, every member is locked out at once,
# with no warning from the IdP — and this date is surfaced nowhere else.
#
# The date is null when SAML is unconfigured or the stored value will not parse,
# and HCL does not reliably short-circuit function arguments, so the comparison
# is wrapped in try() rather than guarded by a preceding null check.
check "saml_certificate_expiry" {
  assert {
    condition = try(
      timecmp(
        timestamp(),
        timeadd(data.fivenines_organization_saml.this.idp_certificate_expires_at, "-720h"),
      ) < 0,
      true, # no parseable expiry to check
    )
    error_message = format(
      "The IdP signing certificate expires at %s — under 30 days away. Every member is locked out when it lapses.",
      data.fivenines_organization_saml.this.idp_certificate_expires_at,
    )
  }
}
