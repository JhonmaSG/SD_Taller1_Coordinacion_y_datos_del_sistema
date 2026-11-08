#!/usr/bin/env bash
#
# deploy_nodos.sh
# Compila el binario del nodo y lo copia a cada máquina remota vía scp (SSH).
#
# Uso:
#   NODES="nodo1@100.x.x.1 nodo2@100.x.x.2 nodo3@100.x.x.3" bash scripts/deploy_nodos.sh
#
# Variables de entorno:
#   NODES        (obligatoria) lista de nodos "usuario@host" separados por espacios.
#   DEST_SUBDIR  (opcional) subcarpeta destino dentro del HOME. Por defecto: test
#                Destino final por nodo: /home/<usuario>/<DEST_SUBDIR>
#   SSH_KEY      (opcional) ruta a la llave privada. Por defecto: ~/.ssh/id_ed25519_vm
#   BIN_NAME     (opcional) nombre del binario en destino. Por defecto: nodo
#
# Ejemplos:
#   NODES="nodo1@100.0.0.1 nodo2@100.0.0.2" bash scripts/deploy_nodos.sh
#   NODES="nodo1@100.0.0.1" DEST_SUBDIR=bully bash scripts/deploy_nodos.sh
#
# Requisitos:
#   - Acceso SSH por llave a cada nodo (password deshabilitado).
#
# No toca los .env de cada nodo: solo copia el binario.

set -euo pipefail

# ------------------------------------------------------------------
# CONFIGURACIÓN — ajusta estos valores a tu entorno
# ------------------------------------------------------------------

# Ruta a la llave privada SSH.
SSH_KEY="${SSH_KEY:-$HOME/.ssh/id_ed25519_vm}"

DEST_SUBDIR="${DEST_SUBDIR:-test}"

BIN_NAME="${BIN_NAME:-nodo}"

if [[ -z "${NODES:-}" ]]; then
  echo "ERROR: define la variable de entorno NODES con los nodos a desplegar." >&2
  echo '       Ejemplo: NODES="nodo1@100.x.x.1 nodo2@100.x.x.2" bash scripts/deploy_nodos.sh' >&2
  exit 1
fi
# shellcheck disable=SC2206
NODES_ARR=(${NODES})

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD_OUT="${PROJECT_DIR}/dist/${BIN_NAME}"

echo "==> Proyecto: ${PROJECT_DIR}"
echo "==> Compilando binario estático (linux/amd64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o "${BUILD_OUT}" "${PROJECT_DIR}/cmd"
echo "    binario listo: ${BUILD_OUT}"

if [[ ! -f "${SSH_KEY}" ]]; then
  echo "ERROR: no se encontró la llave SSH en '${SSH_KEY}'." >&2
  echo "       Ajusta la variable SSH_KEY al inicio del script." >&2
  exit 1
fi

SSH_OPTS=(-i "${SSH_KEY}" -o StrictHostKeyChecking=accept-new)

for node in "${NODES_ARR[@]}"; do
  # Deriva el usuario (antes del @) para construir el home destino.
  user="${node%@*}"
  dest_dir="/home/${user}/${DEST_SUBDIR}"

  echo "==> Desplegando en ${node} -> ${dest_dir} ..."

  # Asegura que la carpeta destino exista.
  if ! ssh "${SSH_OPTS[@]}" "${node}" "mkdir -p '${dest_dir}'"; then
    echo "    AVISO: no se pudo crear ${dest_dir} en ${node}. Se omite." >&2
    continue
  fi

  # Copia el binario.
  if scp "${SSH_OPTS[@]}" "${BUILD_OUT}" "${node}:${dest_dir}/${BIN_NAME}"; then
    ssh "${SSH_OPTS[@]}" "${node}" "chmod +x '${dest_dir}/${BIN_NAME}'"
    echo "    OK -> ${node}:${dest_dir}/${BIN_NAME}"
  else
    echo "    ERROR copiando a ${node}." >&2
  fi
done

echo "==> Despliegue finalizado."
echo "    En cada nodo, ejecuta desde el directorio que contiene su .env:"
echo "      cd /home/<usuario>/${DEST_SUBDIR} && ./${BIN_NAME}"
