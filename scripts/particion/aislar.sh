#!/bin/bash
# Uso:
#   sudo ./aislar.sh --interface tailscale0 --grpc-port 5000 --mongo-port 27017
#   sudo ./aislar.sh --restore

IFACE="tailscale0"
GRPC_PORT=5000
MONGO_PORT=27017
CHAIN="PARTITION"
ACCION="aislar"

while [ $# -gt 0 ]; do
  case "$1" in
    --interface)  IFACE="$2"; shift 2 ;;
    --grpc-port)  GRPC_PORT="$2"; shift 2 ;;
    --mongo-port) MONGO_PORT="$2"; shift 2 ;;
    --restore)    ACCION="restaurar"; shift ;;
    *)            shift ;;  # se ignoran las IPs si las pasas
  esac
done

limpiar() {
  iptables -D INPUT  -j $CHAIN 2>/dev/null
  iptables -D OUTPUT -j $CHAIN 2>/dev/null
  iptables -F $CHAIN 2>/dev/null
  iptables -X $CHAIN 2>/dev/null
}

if [ "$ACCION" = "restaurar" ]; then
  limpiar
  echo "Nodo reconectado (reglas $CHAIN eliminadas)."
  exit 0
fi

limpiar   # evita duplicar reglas si lo ejecutas dos veces
iptables -N $CHAIN
iptables -I INPUT 1  -j $CHAIN
iptables -I OUTPUT 1 -j $CHAIN

# Seguro: el SSH nunca se bloquea
iptables -A $CHAIN -p tcp --dport 22 -j ACCEPT
iptables -A $CHAIN -p tcp --sport 22 -j ACCEPT

# Bloqueo por puerto, sin importar la IP
for p in $GRPC_PORT $MONGO_PORT; do
  iptables -A $CHAIN -i $IFACE -p tcp --dport $p -j DROP   # entrada: peers hablan a mi puerto
  iptables -A $CHAIN -i $IFACE -p tcp --sport $p -j DROP   # entrada: respuestas de los puertos de peers
  iptables -A $CHAIN -o $IFACE -p tcp --dport $p -j DROP   # salida: yo hablo al puerto de peers
  iptables -A $CHAIN -o $IFACE -p tcp --sport $p -j DROP   # salida: respuestas desde mis puertos
done

echo "Nodo aislado en puertos $GRPC_PORT y $MONGO_PORT sobre $IFACE."
iptables -L $CHAIN -n -v