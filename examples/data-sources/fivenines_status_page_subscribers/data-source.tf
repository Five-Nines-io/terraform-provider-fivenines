# The email subscribers of a status page, newest first.
#
# REQUIRES THE `status_pages: update` PERMISSION even though it only reads:
# subscriber addresses are PII, so a read-only API token gets a 403 here where
# it can read the page itself. The addresses also land in Terraform state in
# plain text -- treat the state file accordingly.
data "fivenines_status_page_subscribers" "all" {
  status_page_id = fivenines_status_page.public.id
}

# A pending address has been sent a confirmation email and has not clicked it,
# so it receives no notifications. The two values partition the list.
data "fivenines_status_page_subscribers" "pending" {
  status_page_id = fivenines_status_page.public.id
  status         = "pending"
}

output "confirmed_count" {
  value = length([
    for s in data.fivenines_status_page_subscribers.all.subscribers :
    s.id if s.status == "confirmed"
  ])
}

# updated_since moves when somebody subscribes and when they confirm, but it
# NEVER tombstones a removal: an unsubscribe, an admin delete and the
# expired-confirmation cleanup all drop the row outright. A reconciler still
# needs a periodic unfiltered read to notice departures.
data "fivenines_status_page_subscribers" "recent" {
  status_page_id = fivenines_status_page.public.id
  updated_since  = "2026-01-01T00:00:00Z"
}
