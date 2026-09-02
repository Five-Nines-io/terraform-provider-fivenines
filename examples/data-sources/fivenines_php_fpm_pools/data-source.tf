data "fivenines_php_fpm_pools" "all" {
  instance_id = fivenines_instance.web.id
}

# Read max_children_reached_at, not max_children_reached: the counter is
# cumulative since php-fpm started and resets on a reload, so a non-zero value
# means "at some point", not "right now".
output "pools_that_ran_out_of_workers" {
  value = [
    for p in data.fivenines_php_fpm_pools.all.php_fpm_pools :
    p.name if p.max_children_reached_at != null
  ]
}

# children_exhausted applies the product's own 5-minute window to that stamp.
output "pools_exhausted_now" {
  value = [
    for p in data.fivenines_php_fpm_pools.all.php_fpm_pools :
    p.saturation_detail if p.children_exhausted
  ]
}
