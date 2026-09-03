# Current vulnerability posture across the whole organization
data "fivenines_cve_stats" "actionable" {
  metric = "cve_actionable_count"
}

output "patchable_vulnerabilities" {
  value = coalesce(data.fivenines_cve_stats.actionable.value, 0)
}

# The same count broken down by severity, worst first
data "fivenines_cve_stats" "by_severity" {
  metric   = "cve_actionable_count"
  group_by = "severity"
}

output "vulnerabilities_by_severity" {
  value = { for bucket in data.fivenines_cve_stats.by_severity.items : bucket.name => bucket.value }
}

# Scoped to the instances this configuration owns. A filter matching none of
# your instances returns zero, never the organization-wide total.
data "fivenines_cve_stats" "web_tier" {
  metric = "cve_count"
  hosts  = [for i in fivenines_instance.web : i.id]
}

# Refuse to grow the web tier while it carries critical findings
check "no_critical_vulnerabilities" {
  assert {
    condition = length([
      for bucket in data.fivenines_cve_stats.by_severity.items : bucket if bucket.name == "Critical"
    ]) == 0
    error_message = "The fleet has critical vulnerabilities with an available fix; patch before scaling out."
  }
}
