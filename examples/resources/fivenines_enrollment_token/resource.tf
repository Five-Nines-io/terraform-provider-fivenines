# The value is returned by the API only when the token is created, so it lives in
# Terraform state and nowhere else. Give each fleet its own, so one leak revokes
# one fleet.
resource "fivenines_enrollment_token" "web_fleet" {
  name = "web fleet"
}

# --- Bootstrapping a host with cloud-init ---------------------------------
#
# The agent installs and enrolls itself on first boot, so the machine appears in
# FiveNines without anyone touching it. `install_command` is the one-liner the
# dashboard hands out, with this token already baked in.
#
# The compute resource below is illustrative — it needs the AWS provider, and the
# same wiring works for any cloud that takes a user-data blob.
variable "ubuntu_ami" {
  description = "AMI to boot. Whatever your image pipeline produces."
  type        = string
}

resource "aws_instance" "web" {
  ami           = var.ubuntu_ami
  instance_type = "t3.small"

  # user_data is readable by anything holding ec2:DescribeInstances, and by every
  # process on the instance itself. That is the usual exposure for a bootstrap
  # credential; if it is not acceptable here, put the token in your secrets
  # manager and have cloud-init fetch it at boot instead.
  user_data = <<-CLOUDINIT
    #cloud-config
    runcmd:
      - ${fivenines_enrollment_token.web_fleet.install_command}
  CLOUDINIT

  # Replacing the token replaces the instances that bootstrap from it. Drop this
  # to rotate the token without recycling the fleet — hosts that already enrolled
  # keep reporting either way.
  user_data_replace_on_change = true
}

# Pass the bare token instead when something other than the setup script does the
# install: an image that already ships the agent, an Ansible play, a Windows host.
resource "aws_instance" "worker" {
  ami           = var.ubuntu_ami
  instance_type = "t3.small"

  user_data = <<-CLOUDINIT
    #cloud-config
    write_files:
      - path: /etc/fivenines/enrollment-token
        permissions: "0600"
        content: ${fivenines_enrollment_token.web_fleet.token}
  CLOUDINIT
}

# --- Retiring a token -----------------------------------------------------
#
# Destroying a token that never enrolled a host deletes it outright. Destroying
# one that has enrolled hosts cannot delete it — that would orphan them — so the
# provider revokes it instead and says so. Either way the token stops enrolling,
# and the hosts it already onboarded keep reporting.
#
# Revoking in the dashboard has the same effect on the next plan: a revoked token
# is dropped from state and replaced, because it enrolls nothing and cannot be
# reactivated. That is the rule fivenines_api_token follows too.

output "web_fleet_enrollment_token" {
  value     = fivenines_enrollment_token.web_fleet.token
  sensitive = true
}
