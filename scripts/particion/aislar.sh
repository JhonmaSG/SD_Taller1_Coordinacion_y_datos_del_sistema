#!/usr/bin/env bash
# Aísla este nodo de los PEERS (entrada y salida). Ejecutar con sudo.
# Uso: sudo ./aislar.sh ip1 ip2 ...
set -eu
for ip in "$@"; do
  iptables -A INPUT -s "$ip" -j DROP
  iptables -A OUTPUT -d "$ip" -j DROP
done
echo "$(date -Is) $(hostname) aislado de: $*"
