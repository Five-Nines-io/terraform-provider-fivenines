# A read-only token, for a dashboard or an exporter that never writes.
# `scopes` defaults to ["read"], so this is the least-privileged token there is.
resource "fivenines_api_token" "reporting" {
  name = "Grafana reporting"
}

# A write-scoped token. "read" is the floor every token carries and the API folds
# it in, so ["write"] and ["read", "write"] describe the same token.
resource "fivenines_api_token" "ci" {
  name   = "CI deploy key"
  scopes = ["write"]
}

# The value is returned once, by the create call, and is stored as a digest
# server-side — there is no endpoint that can hand it back. Capture it now or
# mint a new token.
output "ci_token" {
  value     = fivenines_api_token.ci.token
  sensitive = true
}

# --- Rotation -----------------------------------------------------------------
#
# Rotate in this order: create the replacement, deploy it, then revoke the old
# one. Doing it the other way round locks you out of the API, because a token is
# allowed to revoke itself and the dashboard is the only way back in.
#
# `create_before_destroy` is that order expressed to Terraform. Without it, a
# change to `name`, `scopes` or `expires_at` — none of which the API can edit in
# place — destroys the live token before its replacement exists, and everything
# holding the old value fails in the gap.

resource "time_rotating" "api_token" {
  rotation_days = 90
}

resource "fivenines_api_token" "deploy" {
  name   = "deploy-${time_rotating.api_token.id}"
  scopes = ["write"]

  # Each rotation moves the expiry forward, which changes the token and so
  # replaces it. The successor is allowed to outlive its predecessor: that is
  # what makes an overlap possible.
  expires_at = timeadd(time_rotating.api_token.rotation_rfc3339, "168h")

  lifecycle {
    create_before_destroy = true
  }
}

# Feed the new value wherever the credential is consumed. The old token keeps
# working until Terraform revokes it at the end of the same apply, so a deploy
# that reads this secret is never left holding a dead key.
output "deploy_token" {
  value     = fivenines_api_token.deploy.token
  sensitive = true
}

# Managing the token the provider itself authenticates with is the one case
# where destroy is refused: revoking it would cut Terraform off mid-apply, with
# the dashboard as the only way back in.
resource "fivenines_api_token" "bootstrap" {
  name   = "terraform-bootstrap"
  scopes = ["write"]

  # Uncomment to allow revoking this very credential — for a leak, not for
  # convenience. It is read from state at destroy time, so it takes an apply of
  # its own before the apply that destroys the token, and it stays true for
  # every destroy afterwards.
  # allow_self_revoke = true
}
