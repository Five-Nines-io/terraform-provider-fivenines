resource "fivenines_task" "backup_check" {
  name                 = "Nightly Backup"
  schedule_type        = "cron"
  schedule             = "0 2 * * *"
  time_zone            = "America/New_York"
  grace_period_minutes = 30
}

resource "fivenines_task" "heartbeat" {
  name                 = "App Heartbeat"
  schedule_type        = "interval"
  interval_seconds     = 300
  grace_period_minutes = 10
}

# Pausing suspends alerting without destroying the task or rotating its ping key.
resource "fivenines_task" "seasonal_import" {
  name             = "Seasonal Import"
  schedule_type    = "interval"
  interval_seconds = 3600
  paused           = true
}
