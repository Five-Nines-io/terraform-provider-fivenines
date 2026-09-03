data "fivenines_workflow_runs" "alert_runs" {
  workflow_id = fivenines_workflow.alert.id
}

# Failed runs, most recently finished first.
data "fivenines_workflow_runs" "failures" {
  workflow_id = fivenines_workflow.alert.id
  status      = "failed"
  order       = "completed_at"
}

output "latest_run_status" {
  value = length(data.fivenines_workflow_runs.alert_runs.runs) > 0 ? data.fivenines_workflow_runs.alert_runs.runs[0].status : "no runs"
}
