# List the node types an execution graph can use
data "fivenines_workflow_node_types" "all" {}

output "trigger_nodes" {
  value = [for n in data.fivenines_workflow_node_types.all.node_types : n.type if n.category == "trigger"]
}

# Fail the plan if the graph references a node type the API does not know about
locals {
  known_node_types = toset([for n in data.fivenines_workflow_node_types.all.node_types : n.type])
  graph            = jsondecode(file("${path.module}/graph.json"))
}

resource "fivenines_workflow" "validated" {
  name                 = "Validated Workflow"
  execution_graph_json = jsonencode(local.graph)

  lifecycle {
    precondition {
      condition     = alltrue([for n in local.graph.nodes : contains(local.known_node_types, n.type)])
      error_message = "The execution graph references an unknown node type."
    }
  }
}
