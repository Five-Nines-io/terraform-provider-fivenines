# Every argument is a server-side filter. Omit them all for the whole org.
data "fivenines_vulnerabilities" "critical" {
  severity  = ["Critical"]
  patchable = true
}

# Fail the plan when the fleet carries more than 5 patchable Critical CVEs.
#
# The instances_never_checked half is not optional. An instance that has never
# sent a package list contributes zero findings while being entirely
# unexamined, so a gate that only counts rows passes on an unscanned fleet.
resource "terraform_data" "cve_gate" {
  input = length(data.fivenines_vulnerabilities.critical.vulnerabilities)

  lifecycle {
    precondition {
      condition     = length(data.fivenines_vulnerabilities.critical.vulnerabilities) <= 5
      error_message = "${length(data.fivenines_vulnerabilities.critical.vulnerabilities)} patchable Critical CVEs exceed the limit of 5."
    }

    precondition {
      condition = data.fivenines_vulnerabilities.critical.scan.instances_never_checked == 0
      error_message = format(
        "%d instances have never been scanned, so the count above is not the whole fleet. Oldest check: %s.",
        data.fivenines_vulnerabilities.critical.scan.instances_never_checked,
        coalesce(data.fivenines_vulnerabilities.critical.scan.oldest_checked_at, "never"),
      )
    }
  }
}

# One host's findings.
data "fivenines_vulnerabilities" "web" {
  instance_id = fivenines_instance.web.id
  severity    = ["Critical", "High"]
}

resource "terraform_data" "web_gate" {
  input = data.fivenines_vulnerabilities.web.scan.last_checked_at

  lifecycle {
    # "Did we look?" comes before "what did we find?". A never-scanned instance
    # returns a null `vulnerabilities` rather than an empty list, so the count
    # below fails the plan on it too -- this precondition fails first, with a
    # sentence that says why.
    precondition {
      condition     = !data.fivenines_vulnerabilities.web.scan.never_checked
      error_message = "This instance has never been scanned. No package list has reached FiveNines, so an empty result would not be an all-clear."
    }

    precondition {
      condition     = length(data.fivenines_vulnerabilities.web.vulnerabilities) == 0
      error_message = "Findings on this host: ${join(", ", [for v in data.fivenines_vulnerabilities.web.vulnerabilities : "${v.package_name} ${v.vulnerability_id} (${v.severity})"])}."
    }
  }
}

# One container image's findings. Read docker_image.state before the list: a
# pending, unsupported or unscannable image answers null, never an empty list.
data "fivenines_organization_docker_images" "nginx" {
  q     = "nginx"
  state = "scanned"
}

data "fivenines_vulnerabilities" "base_image" {
  docker_image_id = data.fivenines_organization_docker_images.nginx.images[0].id
}

output "base_image_criticals" {
  value = [
    for v in data.fivenines_vulnerabilities.base_image.vulnerabilities :
    "${v.package_name} ${v.installed_version} -> ${coalesce(v.fix_version, "no fix")}"
    if v.severity == "Critical"
  ]
}

# "Where else in my fleet is this advisory?"
data "fivenines_vulnerabilities" "advisory" {
  vulnerability_id = "UBUNTU-CVE-2024-2511"
}

output "affected_hosts" {
  value = toset([for v in data.fivenines_vulnerabilities.advisory.vulnerabilities : v.host_name])
}
