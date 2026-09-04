# Every task in the organization.
data "fivenines_tasks" "all" {}

# Filters are optional and combine.
data "fivenines_tasks" "paused_cron" {
  status        = "paused"
  schedule_type = "cron"
}

# Case-insensitive substring search on the task name.
data "fivenines_tasks" "backups" {
  query     = "backup"
  order     = "name"
  direction = "asc"
}

# Poll incrementally: feed the newest updated_at you saw back as the cursor.
# The boundary is inclusive, so a task updated in that same instant comes back
# again rather than being skipped.
#
# It tracks the row's own updated_at and nothing else. A task going late is
# derived at read time and writes no column, and a deleted task leaves no
# tombstone -- so do not make this the only source for health or for removals.
data "fivenines_tasks" "recently_changed" {
  updated_since = "2026-01-01T00:00:00Z"
}

# updated_since and limit cannot be combined -- the provider rejects it at plan
# time. The cursor is inclusive, so a capped poll can stop advancing and re-read
# the same tasks forever. Poll unbounded, or take a bounded snapshot; not both.

# Health is derived, so read it unfiltered: ok | late | waiting | paused.
# "waiting" is a task that has never been pinged at all, which is a different
# problem from one that stopped.
data "fivenines_tasks" "all_active" {
  status = "active"
}

output "late_task_names" {
  value = [
    for t in data.fivenines_tasks.all_active.tasks : t.name
    if t.monitoring_status == "late"
  ]
}

output "never_pinged_task_names" {
  value = [
    for t in data.fivenines_tasks.all_active.tasks : t.name
    if t.monitoring_status == "waiting"
  ]
}

# Bound the read. Unset walks the whole index at one request per 100 tasks, and
# the API throttles per IP -- so on a large organization, cap it. Pair limit with
# order and direction, or the API's default sort decides which tasks you keep.
data "fivenines_tasks" "ten_newest" {
  order     = "created_at"
  direction = "desc"
  limit     = 10
}

# Catch a task somebody paused in the dashboard and forgot to resume.
output "paused_task_names" {
  value = data.fivenines_tasks.paused_cron.tasks[*].name
}

# ping_key and ping_url are secrets, so an output that carries them has to say so.
output "backup_ping_url" {
  value = one([
    for t in data.fivenines_tasks.backups.tasks : t.ping_url if t.name == "nightly backup"
  ])
  sensitive = true
}
