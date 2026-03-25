# Basic workflow (metadata only, configure graph in UI)
resource "fivenines_workflow" "cpu_alert" {
  name             = "High CPU Alert"
  description      = "Alert when CPU exceeds 90% for 5 minutes"
  interval_seconds = 60
}

# Workflow with execution graph and auto-activation
resource "fivenines_workflow" "disk_alert" {
  name             = "Disk Space Alert"
  description      = "Alert when disk usage exceeds 85%"
  interval_seconds = 300
  active           = true

  # Provide the execution graph as JSON — use file() or jsonencode()
  execution_graph_json = file("${path.module}/disk-alert-graph.json")
}
