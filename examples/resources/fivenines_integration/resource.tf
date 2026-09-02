# Webhook — the only type whose credentials Terraform mints for you.
resource "fivenines_integration" "ops_webhook" {
  type = "webhook"
  name = "Ops hook"
  url  = "https://example.com/hooks/fivenines"

  # Optional: bring your own HMAC-SHA256 signing key. Omit it and the API
  # generates one, which either way is exported as webhook_signing_secret.
  secret = var.webhook_signing_secret
}

# The signing secret and the verification token are returned once, at create,
# and are never readable again — they only exist in Terraform state from here on.
output "webhook_signing_secret" {
  value     = fivenines_integration.ops_webhook.webhook_signing_secret
  sensitive = true
}

# Your endpoint must answer a GET with 200 and echo this token in the
# X-Fivenines-Verification header. The token expires 24 hours after create.
output "webhook_verification_token" {
  value     = fivenines_integration.ops_webhook.webhook_verification_token
  sensitive = true
}

# Verify as part of the apply, once the endpoint is already serving the header.
# The apply fails with the API's reason if verification does not succeed —
# workflow notification nodes refuse to deliver to an unverified webhook.
resource "fivenines_integration" "verified_webhook" {
  type           = "webhook"
  url            = "https://example.com/hooks/fivenines-verified"
  verify_webhook = true
}

# PagerDuty — the routing key is proved with a live trigger/resolve round-trip
# at create, so applying this sends a real (immediately resolved) test alert.
resource "fivenines_integration" "pagerduty" {
  type        = "pagerduty"
  name        = "Platform on-call"
  routing_key = var.pagerduty_routing_key
}

# Pushover — bring your own application token from https://pushover.net/apps/build.
resource "fivenines_integration" "pushover" {
  type      = "pushover"
  name      = "On-call phones"
  user_key  = var.pushover_user_key
  app_token = var.pushover_app_token
}

# Slack, Discord, Teams, Telegram and email cannot be created over the API.
# Connect them from Settings > Integrations, then look them up by name.
data "fivenines_integrations" "all" {}

locals {
  slack_id = one([
    for i in data.fivenines_integrations.all.integrations :
    i.id if i.provider == "slack"
  ])
}
