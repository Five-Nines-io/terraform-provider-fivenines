data "fivenines_integrations" "all" {}

# `type` is the backing class name the API returns, not the short key used to
# create a channel: SlackIntegration, not slack.
data "fivenines_integrations" "slack" {
  type    = "SlackIntegration"
  enabled = true
}

# A workflow notification node refuses to deliver to an unverified channel even
# when it is enabled, so filter on `enabled` and check `verified` on the rows.
output "deliverable_slack_channels" {
  value = [
    for i in data.fivenines_integrations.slack.integrations :
    i.name if i.verified
  ]
}
