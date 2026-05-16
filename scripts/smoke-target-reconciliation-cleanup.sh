#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose.migration.yml}"
FIXTURE="${VDBG_SMOKE_FIXTURE:-testdata/migration/synthetic-small.json}"
RUN_DIR="${VDBG_SMOKE_RUN_DIR:-$(mktemp -d /tmp/vdb-guardian-stale-cleanup-smoke.XXXXXX)}"
MILVUS_PORT="${VDB_GUARDIAN_MILVUS_PORT:-19530}"
PG_PORT="${VDB_GUARDIAN_POSTGRES_PORT:-15432}"
export VDB_GUARDIAN_POSTGRES_PORT="${PG_PORT}"
PG_URL="${VDB_GUARDIAN_LOCAL_PG_URL:-postgres://vdb_guardian:vdb_guardian_local_password@localhost:${PG_PORT}/vdb_guardian?sslmode=disable}"
COLLECTION="${VDBG_SMOKE_COLLECTION:-items}"
TARGET_TABLE="${VDBG_SMOKE_TARGET_TABLE:-items}"
DIMENSION="${VDBG_SMOKE_DIMENSION:-8}"
STALE_ID="${VDBG_SMOKE_STALE_ID:-stale_only_target}"

log() {
  printf '%s\n' "$*"
}

run_vdbg() {
  go run ./cmd/vdbg "$@"
}

require_0600() {
  local path="$1"
  local mode
  mode="$(stat -c '%a' "${path}")"
  if [[ "${mode}" != "600" ]]; then
    echo "artifact_permissions=fail path=${path} mode=${mode}" >&2
    exit 1
  fi
}

secret_scan_artifacts() {
  local hits
  hits="$(grep -RInE 'postgres://|postgresql://|password|token|credential|Bearer|api[_-]?key' "${RUN_DIR}" || true)"
  if [[ -n "${hits}" ]]; then
    echo "secret_scan=fail" >&2
    printf '%s\n' "${hits}" >&2
    exit 1
  fi
}

log "run_dir=${RUN_DIR}"
log "pgvector_connection_url=[REDACTED]"
mkdir -p "${RUN_DIR}"

make migration-stack-config >/dev/null
make migration-stack-up >/dev/null
make migration-stack-check >/dev/null
log "stack_ready=pass"

RECORD_MAPPING="${RUN_DIR}/record-mapping.json"
SOURCE_ARTIFACT="${RUN_DIR}/source-full-records.json"
TARGET_BEFORE="${RUN_DIR}/target-full-records-before.json"
TARGET_AFTER="${RUN_DIR}/target-full-records-after.json"
RECONCILE_BEFORE="${RUN_DIR}/target-reconciliation-before.json"
RECONCILE_AFTER="${RUN_DIR}/target-reconciliation-after.json"
CLEANUP_RESULT="${RUN_DIR}/target-stale-cleanup.json"

cat >"${RECORD_MAPPING}" <<JSON
{
  "schema_version": "record_mapping/v1",
  "status": "pass",
  "summary": {
    "collection_count": 1,
    "mapped_scalar_count": 0,
    "ignored_field_count": 0,
    "blocker_count": 0,
    "warning_count": 0
  },
  "mappings": [
    {
      "source_collection": "${COLLECTION}",
      "target_schema": "public",
      "target_table": "${TARGET_TABLE}",
      "primary_key": {"kind": "primary_key", "source_field": "id", "target_column": "id", "target_type": "text"},
      "vector": {"kind": "vector", "source_field": "embedding", "target_column": "embedding", "target_type": "vector(${DIMENSION})"},
      "scalars": [],
      "ignored_fields": [],
      "blockers": [],
      "warnings": []
    }
  ]
}
JSON
chmod 600 "${RECORD_MAPPING}"

run_vdbg seed-milvus \
  --fixture "${FIXTURE}" \
  --address "localhost:${MILVUS_PORT}" \
  --collection "${COLLECTION}" \
  --id-field id \
  --vector-field embedding \
  --metric cosine >/dev/null
log "milvus_seed=pass"

run_vdbg seed-pgvector \
  --fixture "${FIXTURE}" \
  --connection-url "${PG_URL}" \
  --table "${TARGET_TABLE}" \
  --id-column id \
  --vector-column embedding >/dev/null
log "pgvector_seed=pass"

# Insert one target-only row using the local disposable database. The connection
# URL is never printed or written to generated artifacts.
docker compose -f "${COMPOSE_FILE}" exec -T postgres-pgvector psql -U vdb_guardian -d vdb_guardian -v ON_ERROR_STOP=1 >/dev/null <<SQL
INSERT INTO ${TARGET_TABLE} (id, embedding)
VALUES ('${STALE_ID}', '[0.11,0.12,0.13,0.14,0.15,0.16,0.17,0.18]'::vector)
ON CONFLICT (id) DO UPDATE SET embedding = EXCLUDED.embedding;
SQL
log "stale_seed=pass"

run_vdbg build-milvus-record-artifact \
  --milvus-address "localhost:${MILVUS_PORT}" \
  --record-mapping "${RECORD_MAPPING}" \
  --output "${SOURCE_ARTIFACT}" >/dev/null

run_vdbg build-pgvector-record-artifact \
  --pgvector-connection-url "${PG_URL}" \
  --record-mapping "${RECORD_MAPPING}" \
  --output "${TARGET_BEFORE}" >/dev/null

run_vdbg reconcile-target \
  --source "${SOURCE_ARTIFACT}" \
  --target "${TARGET_BEFORE}" \
  --output "${RECONCILE_BEFORE}" >/dev/null || true
require_0600 "${RECONCILE_BEFORE}"

stale_before="$(python3 - "${RECONCILE_BEFORE}" "${STALE_ID}" <<'PY'
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    report = json.load(handle)
if sys.argv[2] not in report["stale_target_ids"]:
    raise SystemExit(f"missing stale id {sys.argv[2]}")
print(report["summary"]["stale_target_count"])
PY
)"
if [[ "${stale_before}" != "1" ]]; then
  echo "stale_before=${stale_before}" >&2
  exit 1
fi
log "stale_before=${stale_before}"

run_vdbg cleanup-target-stale \
  --reconcile-report "${RECONCILE_BEFORE}" \
  --pgvector-connection-url "${PG_URL}" \
  --target-table "${TARGET_TABLE}" \
  --target-id-column id \
  --output "${CLEANUP_RESULT}" \
  --confirm-delete-stale >/dev/null
require_0600 "${CLEANUP_RESULT}"
log "cleanup_result=pass"

row_count="$(docker compose -f "${COMPOSE_FILE}" exec -T postgres-pgvector psql -U vdb_guardian -d vdb_guardian -tAc "SELECT COUNT(*) FROM ${TARGET_TABLE};" | tr -d '[:space:]')"
if [[ "${row_count}" != "100" ]]; then
  echo "row_count=${row_count}" >&2
  exit 1
fi
log "row_count_after_cleanup=${row_count}"

run_vdbg build-pgvector-record-artifact \
  --pgvector-connection-url "${PG_URL}" \
  --record-mapping "${RECORD_MAPPING}" \
  --output "${TARGET_AFTER}" >/dev/null

run_vdbg reconcile-target \
  --source "${SOURCE_ARTIFACT}" \
  --target "${TARGET_AFTER}" \
  --output "${RECONCILE_AFTER}" >/dev/null
require_0600 "${RECONCILE_AFTER}"

stale_after="$(python3 - "${RECONCILE_AFTER}" <<'PY'
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    report = json.load(handle)
print(report["summary"]["stale_target_count"])
PY
)"
if [[ "${stale_after}" != "0" ]]; then
  echo "stale_after=${stale_after}" >&2
  exit 1
fi
log "stale_after=${stale_after}"

require_0600 "${RECORD_MAPPING}"
require_0600 "${SOURCE_ARTIFACT}"
require_0600 "${TARGET_BEFORE}"
require_0600 "${TARGET_AFTER}"
secret_scan_artifacts
log "secret_scan=pass"
log "smoke_result=pass"
