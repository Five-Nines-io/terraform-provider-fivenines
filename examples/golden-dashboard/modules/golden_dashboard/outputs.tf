output "dashboard_id" {
  description = "ID of the dashboard this module built."
  value       = fivenines_dashboard.this.id
}

output "shared" {
  description = "Whether a public share link exists. Sharing is an action, not a managed field - reconcile this across environments to audit what is readable without signing in."
  value       = fivenines_dashboard.this.shared
}

output "visualization_count" {
  description = "Panels the dashboard holds. Higher than the panels this module declares means someone added one by hand."
  value       = fivenines_dashboard.this.visualization_count
}
