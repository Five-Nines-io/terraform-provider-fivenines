data "fivenines_node_types" "all" {}

# The trigger nodes this organization is actually allowed to use
output "available_triggers" {
  value = [
    for nt in data.fivenines_node_types.all.node_types :
    nt.type if nt.category == "trigger" && nt.available
  ]
}
