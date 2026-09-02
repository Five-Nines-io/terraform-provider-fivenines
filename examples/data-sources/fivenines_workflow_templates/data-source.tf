data "fivenines_workflow_templates" "all" {}

output "template_slugs" {
  value = [for t in data.fivenines_workflow_templates.all.templates : t.slug]
}
