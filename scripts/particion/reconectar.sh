#!/usr/bin/env bash
# Uso: sudo ./reconectar.sh ip1 ip2 ...
set -eu
for ip in "$@"; do
  iptables -D INPUT -s "$ip" -j DROP || true
  iptables -D OUTPUT -d "$ip" -j DROP || true
done
echo "$(date -Is) $(hostname) reconectado"
