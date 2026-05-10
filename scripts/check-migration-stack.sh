#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose.migration.yml}"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres-pgvector}"
MILVUS_HOST="${VDB_GUARDIAN_MILVUS_HOST:-127.0.0.1}"
MILVUS_PORT="${VDB_GUARDIAN_MILVUS_PORT:-19530}"

usage() {
  cat <<USAGE
Usage: $0 <command>

Commands:
  config       Validate the Docker Compose file without starting containers.
  status       Show migration stack container status.
  postgres     Verify PostgreSQL is reachable and pgvector is installed.
  milvus-port  Verify the Milvus gRPC port is reachable.
USAGE
}

compose() {
  docker compose -f "${PROJECT_ROOT}/${COMPOSE_FILE}" "$@"
}

check_config() {
  compose config >/dev/null
  echo "compose config ok: ${COMPOSE_FILE}"
}

check_status() {
  compose ps
}

check_postgres() {
  compose exec -T "${POSTGRES_SERVICE}" psql \
    -U "${VDB_GUARDIAN_POSTGRES_USER:-vdb_guardian}" \
    -d "${VDB_GUARDIAN_POSTGRES_DB:-vdb_guardian}" \
    -c "SELECT 1 AS postgres_ok; SELECT extname FROM pg_extension WHERE extname = 'vector';"
}

check_milvus_port() {
  python3 - "${MILVUS_HOST}" "${MILVUS_PORT}" <<'PY'
import socket
import sys

host = sys.argv[1]
port = int(sys.argv[2])
with socket.create_connection((host, port), timeout=5):
    print(f"milvus port reachable: {host}:{port}")
PY
}

main() {
  command="${1:-}"
  case "${command}" in
    config)
      check_config
      ;;
    status)
      check_status
      ;;
    postgres)
      check_postgres
      ;;
    milvus-port)
      check_milvus_port
      ;;
    -h|--help|help|"")
      usage
      ;;
    *)
      echo "unknown command: ${command}" >&2
      usage >&2
      exit 2
      ;;
  esac
}

main "$@"
