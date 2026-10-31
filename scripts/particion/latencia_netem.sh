#!/usr/bin/env bash
# Opcional: latencia 100ms ± 20ms. Uso: sudo ./latencia_netem.sh tailscale0 [on|off]
IF=${1:-tailscale0}; ACT=${2:-on}
if [ "$ACT" = on ]; then tc qdisc add dev "$IF" root netem delay 100ms 20ms; else tc qdisc del dev "$IF" root; fi
