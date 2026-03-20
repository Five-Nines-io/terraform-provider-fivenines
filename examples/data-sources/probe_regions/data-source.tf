data "fivenines_probe_regions" "all" {}

output "available_regions" {
  value = data.fivenines_probe_regions.all.regions
}
