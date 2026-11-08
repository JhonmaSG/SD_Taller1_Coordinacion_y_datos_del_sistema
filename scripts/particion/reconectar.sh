#!/bin/bash
# Uso: sudo ./reconectar.sh

CHAIN="PARTITION"

# Quitar los saltos desde INPUT y OUTPUT (repetido por si quedaron duplicados)
while iptables -D INPUT  -j $CHAIN 2>/dev/null; do :; done
while iptables -D OUTPUT -j $CHAIN 2>/dev/null; do :; done

# Vaciar y borrar la cadena
iptables -F $CHAIN 2>/dev/null
iptables -X $CHAIN 2>/dev/null

echo "Nodo reconectado: reglas $CHAIN eliminadas."

# Verificación: no debe aparecer ninguna regla con DROP en 5000 ni 27017
iptables -L -n | grep -E "dpt:(5000|27017)|spt:(5000|27017)" \
  && echo "AVISO: aún quedan reglas sobre esos puertos." \
  || echo "OK: sin reglas residuales sobre 5000/27017."