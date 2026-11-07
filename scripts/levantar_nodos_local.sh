#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
MODULE_DIR="${REPO_ROOT}/src/bully-algorithm"
RUNTIME_DIR="${REPO_ROOT}/.runtime/local-nodes"
LOG_DIR="${RUNTIME_DIR}/logs"

# Permite reutilizar DB_URI, DB_NAME y DB_COLLECTION desde el .env del módulo.
# Se parsea manualmente para soportar archivos creados con saltos de línea CRLF
# en Windows y para no ejecutar el contenido del archivo como shell script.
ENV_FILE="${MODULE_DIR}/.env"
if [[ -f "${ENV_FILE}" ]]; then
  while IFS='=' read -r key value || [[ -n "${key:-}" ]]; do
    key="${key//$'\r'/}"
    value="${value//$'\r'/}"
    if [[ -z "${key}" || "${key}" == \#* ]]; then
      continue
    fi
    export "${key}=${value}"
  done < "${ENV_FILE}"
fi

DB_URI="${DB_URI:-}"
DB_NAME="${DB_NAME:-inventory}"
DB_COLLECTION="${DB_COLLECTION:-resources}"
TIMEOUT_MS="${TIMEOUT_MS:-3000}"

if [[ -z "${DB_URI}" ]]; then
  echo "ERROR: DB_URI no está definido." >&2
  echo "Configúralo en ${ENV_FILE} o exporta DB_URI antes de ejecutar el script." >&2
  exit 1
fi

mkdir -p "${LOG_DIR}"

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    BINARY="${RUNTIME_DIR}/bully-node.exe"
    ;;
  *)
    BINARY="${RUNTIME_DIR}/bully-node"
    ;;
esac

pushd "${MODULE_DIR}" >/dev/null

echo "Compilando el nodo..."
go build -o "${BINARY}" ./cmd

popd >/dev/null

PIDS=()

cleanup() {
  local pid
  echo
  echo "Deteniendo nodos locales..."
  for pid in "${PIDS[@]:-}"; do
    kill "${pid}" 2>/dev/null || true
  done
  for pid in "${PIDS[@]:-}"; do
    wait "${pid}" 2>/dev/null || true
  done
}

trap cleanup INT TERM EXIT

start_node() {
  local node_id="$1"
  local port="$2"
  local peers="$3"
  local log_file="${LOG_DIR}/node${node_id}.log"

  echo "Iniciando nodo ${node_id} en 127.0.0.1:${port}"
  echo "  Peers: ${peers:-ninguno}"
  echo "  Log: ${log_file}"

  env \
    NODE_ID="${node_id}" \
    NODE_HOST="127.0.0.1" \
    NODE_PORT="${port}" \
    PEERS="${peers}" \
    TIMEOUT_MS="${TIMEOUT_MS}" \
    DB_URI="${DB_URI}" \
    DB_NAME="${DB_NAME}" \
    DB_COLLECTION="${DB_COLLECTION}" \
    "${BINARY}" >"${log_file}" 2>&1 &

  PIDS+=("$!")
}

start_node 1 5001 "2@127.0.0.1:5002,3@127.0.0.1:5003"
start_node 2 5002 "1@127.0.0.1:5001,3@127.0.0.1:5003"
start_node 3 5003 "1@127.0.0.1:5001,2@127.0.0.1:5002"

echo
echo "Tres nodos iniciados. El nodo 3 tiene la prioridad más alta."
echo "Logs disponibles en: ${LOG_DIR}"
echo "Presiona Ctrl+C para detenerlos."

while true; do
  for pid in "${PIDS[@]}"; do
    if ! kill -0 "${pid}" 2>/dev/null; then
      wait "${pid}" 2>/dev/null || true
      echo "Un nodo terminó. Revisa los logs en ${LOG_DIR}." >&2
      exit 1
    fi
  done
  sleep 1
done
