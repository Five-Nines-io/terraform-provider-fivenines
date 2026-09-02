# The organization's whole container image inventory.
data "fivenines_docker_images" "all" {}

locals {
  # Read `state` before the counts. A pending, unsupported or unscannable image
  # carries a null count, never 0, so filtering on state first is what keeps
  # "nobody looked" out of the same bucket as "nothing found".
  scanned_images = [
    for img in data.fivenines_docker_images.all.images : img if img.state == "scanned"
  ]

  images_with_criticals = [
    for img in local.scanned_images : img if img.critical_vulnerability_count > 0
  ]
}

# Fail the plan when a scanned image carries Critical findings, or when part of
# the inventory has not been scanned at all - an unscanned image is not a clean
# one, and posture is the deliberately unfiltered answer to "is this list the
# whole picture".
resource "terraform_data" "image_gate" {
  input = length(local.images_with_criticals)

  lifecycle {
    precondition {
      condition = length(local.images_with_criticals) == 0
      error_message = format("Images with Critical CVEs: %s.", join(", ", [
        for img in local.images_with_criticals :
        "${img.display_name} (${img.critical_vulnerability_count}${img.finding_count_is_floor ? "+" : ""})"
      ]))
    }

    precondition {
      condition     = data.fivenines_docker_images.all.posture.pending == 0
      error_message = "${data.fivenines_docker_images.all.posture.pending} images have not been scanned yet, so the check above does not cover them."
    }
  }
}

# Look an image up by tag or digest.
data "fivenines_docker_images" "nginx" {
  q = "nginx"
}

# The images that could not be scanned, with the reason and the blast radius.
output "unscannable_images" {
  value = {
    for img in data.fivenines_docker_images.all.images :
    img.display_name => {
      reason = img.state_reason
      # Only api_error is transient; the rest are permanent for an immutable
      # digest, so retrying them is wasted work.
      error_type  = img.state_error_type
      retry_worth = img.state_error_type == "api_error"
      hosts       = img.running_host_count
    }
    if img.state == "unscannable"
  }
}

# The agent capped these package lists, so their counts are a floor.
output "images_with_partial_counts" {
  value = [
    for img in local.scanned_images :
    "${img.display_name}: ${img.vulnerability_count}+ findings"
    if img.finding_count_is_floor
  ]
}
