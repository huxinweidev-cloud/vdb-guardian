#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose.migration.yml}"
FIXTURE="${VDBG_SMOKE_FIXTURE:-testdata/migration/synthetic-small.json}"
RUN_DIR="${VDBG_SMOKE_RUN_DIR:-$(mktemp -d /tmp/vdb-guardian-checkpoint-smoke.XXXXXX)}"
MILVUS_PORT="${VDB_GUARDIAN_MILVUS_PORT:-19530}"
PG_PORT="${VDB_GUARDIAN_POSTGRES_PORT:-15432}"
export VDB_GUARDIAN_POSTGRES_PORT="${PG_PORT}"
PG_URL="${VDB_GUARDIAN_LOCAL_PG_URL:-postgres://vdb_guardian:vdb_guardian_local_password@localhost:${PG_PORT}/vdb_guardian?sslmode=disable}"
COLLECTION="${VDBG_SMOKE_COLLECTION:-items}"
TARGET_TABLE="${VDBG_SMOKE_TARGET_TABLE:-items}"
JOB_ID="${VDBG_SMOKE_JOB_ID:-checkpoint-smoke}"
BATCH_SIZE="${VDBG_SMOKE_BATCH_SIZE:-25}"
DIMENSION="${VDBG_SMOKE_DIMENSION:-8}"

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

run_vdbg seed-milvus \
  --fixture "${FIXTURE}" \
  --address "localhost:${MILVUS_PORT}" \
  --collection "${COLLECTION}" \
  --id-field id \
  --vector-field embedding \
  --metric cosine >/dev/null

run_vdbg search-milvus \
  --fixture "${FIXTURE}" \
  --address "localhost:${MILVUS_PORT}" \
  --collection "${COLLECTION}" \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine >/dev/null
log "milvus_seed=pass"

INSPECTION_PLAN="${RUN_DIR}/milvus-plan.json"
SCHEMA_PLAN="${RUN_DIR}/pgvector-schema-plan.json"
SCHEMA_COMPARE="${RUN_DIR}/schema-compare-report.json"
SCHEMA_APPLY_DIR="${RUN_DIR}/schema-apply"
LIVE_SCHEMA="${RUN_DIR}/live-pgvector-schema.json"
APPLIED_COMPARE="${RUN_DIR}/applied-schema-compare-report.json"
RECORD_MAPPING="${RUN_DIR}/record-mapping.json"
CHECKPOINT="${RUN_DIR}/migration-checkpoint.json"
MIGRATION_REPORT="${RUN_DIR}/migration-report.json"
ARTIFACT_DIR="${RUN_DIR}/verify"

run_vdbg inspect-milvus \
  --milvus-address "localhost:${MILVUS_PORT}" \
  --collection "${COLLECTION}" \
  --output "${INSPECTION_PLAN}" >/dev/null

run_vdbg plan-pgvector-schema \
  --inspection-plan "${INSPECTION_PLAN}" \
  --output "${SCHEMA_PLAN}" >/dev/null

run_vdbg compare-schema-plans \
  --inspection-plan "${INSPECTION_PLAN}" \
  --schema-plan "${SCHEMA_PLAN}" \
  --output "${SCHEMA_COMPARE}" >/dev/null

run_vdbg apply-pgvector-schema \
  --schema-plan "${SCHEMA_PLAN}" \
  --artifact-dir "${SCHEMA_APPLY_DIR}" \
  --job-id schema-apply-smoke \
  --pgvector-connection-url "${PG_URL}" \
  --execute >/dev/null

run_vdbg inspect-pgvector-schema \
  --pgvector-connection-url "${PG_URL}" \
  --target-schema public \
  --output "${LIVE_SCHEMA}" >/dev/null

run_vdbg compare-applied-schema \
  --schema-plan "${SCHEMA_PLAN}" \
  --live-schema "${LIVE_SCHEMA}" \
  --output "${APPLIED_COMPARE}" >/dev/null

run_vdbg map-migration-records \
  --schema-plan "${SCHEMA_PLAN}" \
  --output "${RECORD_MAPPING}" >/dev/null
log "schema_gate=pass"

run_vdbg migrate \
  --milvus-address "localhost:${MILVUS_PORT}" \
  --source-collection "${COLLECTION}" \
  --milvus-id-field id \
  --milvus-vector-field embedding \
  --pgvector-connection-url "${PG_URL}" \
  --target-table "${TARGET_TABLE}" \
  --pgvector-id-column id \
  --pgvector-vector-column embedding \
  --dimension "${DIMENSION}" \
  --batch-size "${BATCH_SIZE}" \
  --require-schema-match \
  --schema-plan "${SCHEMA_PLAN}" \
  --live-schema "${LIVE_SCHEMA}" \
  --checkpoint-path "${CHECKPOINT}" \
  --output "${MIGRATION_REPORT}" \
  --job-id "${JOB_ID}-initial" >/dev/null

require_0600 "${CHECKPOINT}"
require_0600 "${MIGRATION_REPORT}"
python3 - "${CHECKPOINT}" <<'PY'
import json
import os
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as handle:
    checkpoint = json.load(handle)
# The smoke first proves a complete checkpointed migrate, then reopens the
# checkpoint as a resumable all-batches-complete state so migrate-and-verify
# exercises the real --resume-from validation/skip path without forcing an
# external crash into the test harness. migrate-and-verify does not currently
# expose the standalone migrate schema-preflight flags, so clear those resume
# fingerprints before the resume-only verification pass.
checkpoint["status"] = "running"
checkpoint.setdefault("resume", {})["schema_plan_path"] = ""
checkpoint.setdefault("resume", {})["schema_plan_fingerprint"] = ""
with open(path, "w", encoding="utf-8") as handle:
    json.dump(checkpoint, handle, indent=2)
    handle.write("\n")
os.chmod(path, 0o600)
PY
log "checkpoint_written=pass"

run_vdbg migrate-and-verify \
  --fixture "${FIXTURE}" \
  --milvus-address "localhost:${MILVUS_PORT}" \
  --source-collection "${COLLECTION}" \
  --milvus-id-field id \
  --milvus-vector-field embedding \
  --pgvector-connection-url "${PG_URL}" \
  --target-table "${TARGET_TABLE}" \
  --pgvector-id-column id \
  --pgvector-vector-column embedding \
  --dimension "${DIMENSION}" \
  --batch-size "${BATCH_SIZE}" \
  --checkpoint-path "${CHECKPOINT}" \
  --resume-from "${CHECKPOINT}" \
  --artifact-dir "${ARTIFACT_DIR}" \
  --job-id "${JOB_ID}" \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine \
  --min-consistency-score 1.0 \
  --max-fingerprint-distance 0 >/dev/null
log "resume_verify=pass"

row_count="$(docker compose -f "${COMPOSE_FILE}" exec -T postgres-pgvector psql -U vdb_guardian -d vdb_guardian -tAc "SELECT COUNT(*) FROM ${TARGET_TABLE};" | tr -d '[:space:]')"
if [[ "${row_count}" != "100" ]]; then
  echo "row_count=${row_count}" >&2
  exit 1
fi
log "row_count=${row_count}"

require_0600 "${ARTIFACT_DIR}/${JOB_ID}-diagnostic-report.json"
if [[ -f "${ARTIFACT_DIR}/${JOB_ID}-source-full-records.json" ]]; then
  require_0600 "${ARTIFACT_DIR}/${JOB_ID}-source-full-records.json"
fi
if [[ -f "${ARTIFACT_DIR}/${JOB_ID}-target-full-records.json" ]]; then
  require_0600 "${ARTIFACT_DIR}/${JOB_ID}-target-full-records.json"
fi
log "artifact_permissions=pass"

secret_scan_artifacts
log "secret_scan=pass"
log "smoke_result=pass"
