data "fivenines_integrations" "all" {}

output "slack_integrations" {
  value = [
    for i in data.fivenines_integrations.all.integrations :
    i if i.provider == "slack"
  ]
}
