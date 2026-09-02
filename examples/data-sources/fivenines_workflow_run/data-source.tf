# The runs index returns headers only: `status = "failed"` there says a run
# broke, not where. Read the run itself for the per-step detail.
data "fivenines_workflow_runs" "failures" {
  workflow_id = fivenines_workflow.alert.id
  status      = "failed"
}

data "fivenines_workflow_run" "latest_failure" {
  workflow_id = fivenines_workflow.alert.id
  run_id      = data.fivenines_workflow_runs.failures.runs[0].id
}

output "failed_steps" {
  value = [
    for step in data.fivenines_workflow_run.latest_failure.steps :
    "${step.node_id}: ${step.error_message}" if step.status == "failed"
  ]
}

# The run-level message, set when a run fails between steps and no step
# explains it.
output "run_error" {
  value = data.fivenines_workflow_run.latest_failure.error
}
