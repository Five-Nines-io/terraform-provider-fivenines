# Enrollment tokens are imported by numeric ID.
#
# The API returns a token's value only when it is created, so `token` and
# `install_command` are null on an imported resource and stay null. Import to
# manage a token's lifecycle — not to recover a secret you have lost.
terraform import fivenines_enrollment_token.web_fleet 42
