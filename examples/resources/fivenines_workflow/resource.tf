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

# Pin the canvas layout instead of letting the API generate one
resource "fivenines_workflow" "memory_alert" {
  name   = "Memory Alert"
  active = true

  execution_graph_json = file("${path.module}/memory-alert-graph.json")
  canvas_data_json = jsonencode({
    viewport = { x = 0, y = 0, zoom = 1 }
  })
}

# Instantiate a template — the graph comes published, so it can be activated
# straight away. See the fivenines_workflow_templates data source for slugs.
resource "fivenines_workflow" "from_template" {
  name          = "Disk Pressure (from template)"
  template_slug = "disk-pressure"
  active        = true
}
