resource "fivenines_workflow" "cpu_alert" {
  name             = "High CPU Alert"
  description      = "Alert when CPU exceeds 90% for 5 minutes"
  interval_seconds = 60
}
