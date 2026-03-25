data "fivenines_workflow_runs" "alert_runs" {
  workflow_id = fivenines_workflow.alert.id
}

output "latest_run_status" {
  value = length(data.fivenines_workflow_runs.alert_runs.runs) > 0 ? data.fivenines_workflow_runs.alert_runs.runs[0].status : "no runs"
}
