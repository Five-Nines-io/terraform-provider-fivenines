# List the workflow templates available to your organization
data "fivenines_workflow_templates" "all" {}

output "template_slugs" {
  value = [for t in data.fivenines_workflow_templates.all.templates : t.slug]
}

# Instantiate one by slug
resource "fivenines_workflow" "disk_pressure" {
  name          = "Disk Pressure"
  template_slug = one([for t in data.fivenines_workflow_templates.all.templates : t.slug if t.category == "instances"])
}
