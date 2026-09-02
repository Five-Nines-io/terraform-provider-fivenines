data "fivenines_wireguard_peers" "all" {
  instance_id = fivenines_instance.vpn.id
}

# Key your own records on key_digest. The raw public key is never published,
# and display_name is operator-editable -- a label, not an identifier.
output "peer_names" {
  value = {
    for p in data.fivenines_wireguard_peers.all.wireguard_peers :
    p.key_digest => p.display_name
  }
}

# Handshake age only means something next to keepalive: WireGuard is silent by
# design, so an idle peer without a persistent keepalive can have an
# arbitrarily old handshake and be perfectly healthy.
output "quiet_keepalive_peers" {
  value = [
    for p in data.fivenines_wireguard_peers.all.wireguard_peers :
    p.display_name if p.keepalive && !p.handshake_active
  ]
}
