# API tokens are identified by a numeric id, not a UUID.
#
# Import brings the metadata under management, never the value: the plaintext
# existed for exactly one response and the server keeps only its digest, so
# `token` stays null on an imported token. Mint a new one if you need a usable
# secret.
terraform import fivenines_api_token.ci 42
