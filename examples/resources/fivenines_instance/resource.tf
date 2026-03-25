resource "fivenines_instance" "web_server" {
  display_name     = "Web Server (Production)"
  enabled          = true
  maintenance_mode = false
}
