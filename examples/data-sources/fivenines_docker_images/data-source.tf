data "fivenines_docker_images" "all" {
  instance_id = fivenines_instance.web.id
}

# vulnerability_count is null unless state is "scanned". A null is
# "not scanned", never "clean", so split the two sets before comparing --
# Terraform errors on a null operand rather than treating it as 0.
locals {
  scanned_images = [
    for i in data.fivenines_docker_images.all.docker_images : i if i.countable
  ]
  unscanned_images = [
    for i in data.fivenines_docker_images.all.docker_images : i if !i.countable
  ]
}

output "images_with_critical_cves" {
  value = [
    for i in local.scanned_images :
    i.display_name if i.critical_vulnerability_count > 0
  ]
}

# The images nobody has ever scanned -- the ones a naive "0 CVEs" read hides.
output "unscanned_images" {
  value = [for i in local.unscanned_images : i.display_name]
}

# A capped package list makes the counts a floor: render "12+", not "12".
output "images_with_floor_counts" {
  value = [
    for i in data.fivenines_docker_images.all.docker_images :
    i.display_name if i.finding_count_is_floor
  ]
}
