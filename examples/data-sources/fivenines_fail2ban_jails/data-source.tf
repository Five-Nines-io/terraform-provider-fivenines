data "fivenines_fail2ban_jails" "all" {
  instance_id = fivenines_instance.web.id
}

# currently_banned is a live gauge; total_banned is cumulative since fail2ban
# started and resets on a restart. They answer different questions.
output "jail_ban_counts" {
  value = {
    for j in data.fivenines_fail2ban_jails.all.fail2ban_jails :
    j.name => j.currently_banned
  }
}

# banned_ips is best-effort: the agent can read the count without the list, so
# an empty array on a jail with a non-zero currently_banned is possible.
output "banned_addresses" {
  value = flatten([
    for j in data.fivenines_fail2ban_jails.all.fail2ban_jails : j.banned_ips
  ])
}
