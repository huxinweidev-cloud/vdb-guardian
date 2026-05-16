#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose.migration.yml}"
FIXTURE="${VDBG_SMOKE_FIXTURE:-testdata/migration/synthetic-small.json}"
RUN_DIR="${VDBG_SMOKE_RUN_DIR:-$(mktemp -d /tmp/vdb-guardian-copy-smoke.XXXXXX)}"
MILVUS_PORT="${VDB_GUARDIAN_MILVUS_PORT:-19530}"
PG_PORT="${VDB_GUARDIAN_POSTGRES_PORT:-15432}"
export VDB_GUARDIAN_POSTGRES_PORT="${PG_PORT}"
PG_PASSWORD="${VDB_GUARDIAN_POSTGRES_PASSWORD:-vdb_guardian_local_password}"
PG_URL="${VDB_GUARDIAN_LOCAL_PG_URL:-postgres://vdb_guardian:${PG_PASSWORD}@localhost:${PG_PORT}/vdb_guardian?sslmode=disable}"
COLLECTION="${VDBG_SMOKE_COLLECTION:-items}"
TARGET_TABLE="${VDBG_SMOKE_TARGET_TABLE:-items}"
JOB_ID="${VDBG_SMOKE_JOB_ID:-copy-smoke}"
BATCH_SIZE="${VDBG_SMOKE_BATCH_SIZE:-25}"
DIMENSION="${VDBG_SMOKE_DIMENSION:-8}"
EXPECTED_RECORDS="${VDBG_SMOKE_EXPECTED_RECORDS:-100}"

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
  hits="$(grep -RIlE 'postgres://|postgresql://|password|token|credential|Bearer|api[_-]?key' "${RUN_DIR}" || true)"
  if [[ -n "${hits}" ]]; then
    echo "secret_scan=fail" >&2
    echo "secret_scan_matching_files_begin" >&2
    printf '%s\n' "${hits}" >&2
    echo "secret_scan_matching_files_end" >&2
    exit 1
  fi
}

log "run_dir=${RUN_DIR}"
log "pgvector_connection_url=[REDACTED]"
# The smoke stack and generated artifacts are intentionally retained by default
# for local debugging; set VDBG_SMOKE_RUN_DIR to control the artifact location.
log "artifact_retention=retained run_dir=${RUN_DIR}"
mkdir -p "${RUN_DIR}"

if [[ ! "${TARGET_TABLE}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
  echo "invalid_target_table=${TARGET_TABLE}" >&2
  exit 1
fi

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
GENERATED_RECORD_MAPPING="${RUN_DIR}/generated-record-mapping.json"
RECORD_MAPPING="${RUN_DIR}/record-mapping.json"
CHECKPOINT="${RUN_DIR}/migration-checkpoint.json"
MIGRATION_REPORT="${RUN_DIR}/migration-report.json"
SOURCE_ARTIFACT="${RUN_DIR}/source-full-records.json"
TARGET_ARTIFACT="${RUN_DIR}/target-full-records.json"
FULL_RECORD_COMPARE="${RUN_DIR}/full-record-compare.json"
RECONCILE_REPORT="${RUN_DIR}/target-reconciliation.json"

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
  --job-id schema-apply-copy-smoke \
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
  --output "${GENERATED_RECORD_MAPPING}" >/dev/null

# The generated mapping is kept as the schema-gate artifact. The known fixture
# comparison below uses a minimal mapping so the smoke asserts only the expected
# id/vector columns and stays stable if fixture metadata fields are added later.
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
  --pgvector-write-mode copy \
  --dimension "${DIMENSION}" \
  --batch-size "${BATCH_SIZE}" \
  --require-schema-match \
  --schema-plan "${SCHEMA_PLAN}" \
  --live-schema "${LIVE_SCHEMA}" \
  --checkpoint-path "${CHECKPOINT}" \
  --output "${MIGRATION_REPORT}" \
  --job-id "${JOB_ID}" >/dev/null

python3 - "${MIGRATION_REPORT}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    report = json.load(handle)
summary = report.get("summary", {})
requested = summary.get("write_mode_requested")
used = summary.get("write_mode_used")
copy_batches = int(summary.get("copy_batches", 0))
batch_upsert_batches = int(summary.get("batch_upsert_batches", 0))
copy_fallbacks = int(summary.get("copy_fallbacks", 0))
if requested != "copy":
    raise SystemExit(f"write_mode_requested={requested!r}")
if used != "copy":
    raise SystemExit(f"write_mode_used={used!r}")
if copy_batches <= 0:
    raise SystemExit(f"copy_batches={copy_batches}")
if batch_upsert_batches != 0:
    raise SystemExit(f"batch_upsert_batches={batch_upsert_batches}")
if copy_fallbacks != 0:
    raise SystemExit(f"copy_fallbacks={copy_fallbacks}")
print(
    "copy_migration=pass "
    f"requested={requested} used={used} copy_batches={copy_batches} "
    f"batch_upsert_batches={batch_upsert_batches} copy_fallbacks={copy_fallbacks}"
)
PY

row_count="$(docker compose -f "${COMPOSE_FILE}" exec -T postgres-pgvector psql -U vdb_guardian -d vdb_guardian -tAc "SELECT COUNT(*) FROM ${TARGET_TABLE};" | tr -d '[:space:]')"
if [[ "${row_count}" != "${EXPECTED_RECORDS}" ]]; then
  echo "row_count=${row_count}" >&2
  exit 1
fi
log "row_count=${row_count}"

run_vdbg build-milvus-record-artifact \
  --milvus-address "localhost:${MILVUS_PORT}" \
  --record-mapping "${RECORD_MAPPING}" \
  --output "${SOURCE_ARTIFACT}" >/dev/null

run_vdbg build-pgvector-record-artifact \
  --pgvector-connection-url "${PG_URL}" \
  --record-mapping "${RECORD_MAPPING}" \
  --output "${TARGET_ARTIFACT}" >/dev/null

run_vdbg compare-full-records \
  --source "${SOURCE_ARTIFACT}" \
  --target "${TARGET_ARTIFACT}" \
  --output "${FULL_RECORD_COMPARE}" >/dev/null
log "full_record_compare=pass"

run_vdbg reconcile-target \
  --source "${SOURCE_ARTIFACT}" \
  --target "${TARGET_ARTIFACT}" \
  --output "${RECONCILE_REPORT}" >/dev/null

stale_count="$(python3 - "${RECONCILE_REPORT}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    report = json.load(handle)
print(report["summary"]["stale_target_count"])
PY
)"
if [[ "${stale_count}" != "0" ]]; then
  echo "stale_target_count=${stale_count}" >&2
  exit 1
fi
log "target_reconciliation=pass stale_target_count=${stale_count}"

require_0600 "${INSPECTION_PLAN}"
require_0600 "${SCHEMA_PLAN}"
require_0600 "${SCHEMA_COMPARE}"
require_0600 "${LIVE_SCHEMA}"
require_0600 "${APPLIED_COMPARE}"
require_0600 "${GENERATED_RECORD_MAPPING}"
while IFS= read -r artifact; do
  require_0600 "${artifact}"
done < <(find "${SCHEMA_APPLY_DIR}" -type f | sort)
require_0600 "${CHECKPOINT}"
require_0600 "${MIGRATION_REPORT}"
require_0600 "${RECORD_MAPPING}"
require_0600 "${SOURCE_ARTIFACT}"
require_0600 "${TARGET_ARTIFACT}"
require_0600 "${FULL_RECORD_COMPARE}"
require_0600 "${RECONCILE_REPORT}"
log "artifact_permissions=pass"

secret_scan_artifacts
log "secret_scan=pass"
log "smoke_result=pass"
