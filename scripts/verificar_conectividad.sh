#!/usr/bin/env bash
# Uso: ./verificar_conectividad.sh usuario ip1 ip2 ...
set -u
USR=$1; shift
OUT=evidencias/E3_conectividad/$(date +%F)_$(hostname).log
mkdir -p "$(dirname "$OUT")"
{ echo "Origen: $(hostname) $(date -Is)"
for ip in "$@"; do
  echo "== ping $ip"; ping -c 3 "$ip"
  echo "== ssh $ip"; ssh -o BatchMode=yes -o ConnectTimeout=5 "$USR@$ip" hostname
done; } 2>&1 | tee "$OUT"
