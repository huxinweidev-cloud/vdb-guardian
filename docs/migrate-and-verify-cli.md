# migrate-and-verify CLI

`vdbg migrate-and-verify` runs the first one-shot local Milvus-to-pgvector migration consistency loop.

It composes existing tested commands and boundaries:

```text
migrate Milvus -> pgvector
build Milvus source fingerprint artifact
build pgvector target fingerprint artifact
compare artifacts through the Python engine
optionally build source/target full-record artifacts and compare full-record equality
render Markdown and diagnostic JSON reports
```

The command assumes both databases are already running and reachable. It does not start Docker or provision services.

## Command

```bash
go run ./cmd/vdbg migrate-and-verify \
  --fixture testdata/migration/synthetic-small.json \
  --milvus-address localhost:19530 \
  --source-collection items \
  --milvus-id-field id \
  --milvus-vector-field embedding \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --pgvector-id-column id \
  --pgvector-vector-column embedding \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --artifact-dir /tmp/vdb-guardian-run \
  --job-id migrate-and-verify-smoke \
  --dimension 8 \
  --batch-size 100 \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine \
  --reset-target \
  --strict-count \
  --full-record-compare \
  --checkpoint-path /tmp/vdb-guardian-run/migrate-and-verify-smoke-checkpoint.json \
  --min-consistency-score 0.999 \
  --max-fingerprint-distance 0.001
```

## Output

A successful run prints migration and verification summaries:

```text
migrate-and-verify completed
source_collection: items
target_table: items
dimension: 8
records_read: 100
records_written: 100
consistency_score: 1.000000
fingerprint_distance: 0.000000
matched_queries: 10
source_fingerprint: /tmp/vdb-guardian-run/migrate-and-verify-smoke-source-fingerprint.json
target_fingerprint: /tmp/vdb-guardian-run/migrate-and-verify-smoke-target-fingerprint.json
result: /tmp/vdb-guardian-run/migrate-and-verify-smoke-result.json
report: /tmp/vdb-guardian-run/migrate-and-verify-smoke-report.md
diagnostic_report: /tmp/vdb-guardian-run/migrate-and-verify-smoke-diagnostic-report.json
source_full_records: /tmp/vdb-guardian-run/migrate-and-verify-smoke-source-full-records.json
target_full_records: /tmp/vdb-guardian-run/migrate-and-verify-smoke-target-full-records.json
full_record_compare: /tmp/vdb-guardian-run/migrate-and-verify-smoke-full-record-compare.json
```

When checkpointing is enabled, the Markdown report includes a `Checkpoint / resume` section and the diagnostic JSON includes:

```json
"checkpoint": {
  "path": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-checkpoint.json",
  "resume_from": ""
}
```

## Required flags

- `--fixture`: synthetic fixture containing verification query vectors.
- `--milvus-address`: Milvus gRPC endpoint.
- `--pgvector-connection-url`: PostgreSQL connection URL. Redact this in logs and docs.
- `--artifact-dir`: directory for source, target, and result artifacts.
- `--dimension`: expected vector dimension.
- `--record-mapping`: optional `vdbg map-migration-records` JSON path. When supplied, the migration step uses the mapping artifact for full-record execution before fingerprint verification. Required when `--full-record-compare` is enabled.

## Defaults

- `--source-collection`: `items`
- `--target-table`: `items`
- `--milvus-id-field`: `id`
- `--milvus-vector-field`: `embedding`
- `--pgvector-id-column`: `id`
- `--pgvector-vector-column`: `embedding`
- `--job-id`: `migrate-and-verify`
- `--batch-size`: `100`
- `--top-k`: `3`
- `--expand-k`: `5`
- `--stable-k`: `2`
- `--boundary-k`: `1`
- `--metric`: `cosine`
- `--reset-target`: `false`. When enabled, the command truncates the pgvector target table before migration.
- `--strict-count`: `false`. When enabled, the command fails if the pgvector target row count does not equal records written after migration.
- `--full-record-compare`: `false`. When enabled, the command builds live Milvus and pgvector full-record artifacts from `--record-mapping`, runs `compare-full-records`, includes full-record equality paths in reports, and exits non-zero on equality failure after preserving diagnostics.
- `--checkpoint-path`: empty. When set, the migration step writes a `0600` batch-level checkpoint after each successful pgvector write batch and after a failed write batch before returning an error.
- `--resume-from`: empty. When set, the migration step loads and validates the checkpoint before creating live database runners. If `--checkpoint-path` is omitted, updated progress is written back to the same checkpoint file.
- `--min-consistency-score`: `0`. The command fails after report generation when `consistency_score` is below this threshold.
- `--max-fingerprint-distance`: `1`. The command fails after report generation when `fingerprint_distance` is above this threshold.

## Optional checkpoint / resume

`migrate-and-verify` passes checkpoint and resume options through to the internal migration step without changing fingerprint or full-record comparison semantics:

```bash
go run ./cmd/vdbg migrate-and-verify \
  --fixture testdata/migration/synthetic-small.json \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --artifact-dir /tmp/vdb-guardian-run \
  --dimension 1536 \
  --checkpoint-path /secure/artifacts/migrate-and-verify-checkpoint.json
```

To resume, pass the checkpoint artifact back with `--resume-from`:

```bash
go run ./cmd/vdbg migrate-and-verify \
  --fixture testdata/migration/synthetic-small.json \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --artifact-dir /tmp/vdb-guardian-run \
  --dimension 1536 \
  --resume-from /secure/artifacts/migrate-and-verify-checkpoint.json
```

Resume validation fails closed when the checkpoint source collection, target table, dimension, batch size, schema-plan fingerprint, record-mapping fingerprint, or status is unsafe. Completed checkpoints are not accepted for resume. `--reset-target` cannot be combined with `--resume-from`; resumed migrations must not truncate the target table before continuing.

Checkpoint files are written with `0600` permissions and contain only non-secret migration identity, record counts, completed/failed batch ranges, and resume offsets. They do not contain PostgreSQL connection URLs, credentials, tokens, raw vectors, or row payloads.

MVP limitation: the source Milvus reader still loads the source result set before pgvector batch writes. Checkpointing currently protects target write batch progress and resume offset; source cursor/page-level streaming remains future work.

## Scope

Implemented:

- Real source-to-target migration.
- Source fingerprint artifact generation from Milvus.
- Target fingerprint artifact generation from pgvector.
- Artifact comparison through the Python engine.
- Markdown report rendering at `<artifact-dir>/<job-id>-report.md`.
- Machine-readable diagnostic JSON rendering at `<artifact-dir>/<job-id>-diagnostic-report.json`.
- Summary output with record counts and primary consistency metrics.
- Optional `--reset-target` cleanup to truncate the pgvector target table before migration.
- Optional `--strict-count` validation to fail on target row count mismatches after migration.
- Optional `--min-consistency-score` and `--max-fingerprint-distance` quality gates that fail the command after writing the Markdown report.
- Optional `--full-record-compare` gate that builds source/target full-record artifacts and runs local full-record equality comparison from the same passing record mapping artifact.
- Optional `--checkpoint-path` and `--resume-from` pass-through for batch-level migration checkpoint/resume.
- Injected-step unit tests for orchestration and failure short-circuiting.

## Verified local smoke

A local migration stack smoke run against `testdata/migration/synthetic-small.json` produced:

```text
records_read: 100
records_written: 100
consistency_score: 1.000000
fingerprint_distance: 0.000000
matched_queries: 10
missing_source_queries: 0
missing_target_queries: 0
```

The generated result artifact is shaped like:

```json
{
  "job_id": "migrate-and-verify-smoke",
  "state": "SUCCEEDED",
  "consistency_score": 1,
  "metrics": {
    "FingerprintDistance": 0,
    "StableNeighborDistance": 0,
    "BoundaryCandidateDistance": 0,
    "BoundaryFlipRate": 0,
    "MatchedQueryCount": 10,
    "MissingSourceQueryCount": 0,
    "MissingTargetQueryCount": 0
  }
}
```

The generated diagnostic report artifact is shaped like:

```json
{
  "schema_version": "v1",
  "job_id": "migrate-and-verify-smoke",
  "state": "SUCCEEDED",
  "migration": {
    "source_collection": "items",
    "target_table": "items",
    "dimension": 8,
    "records_read": 100,
    "records_written": 100
  },
  "verification": {
    "consistency_score": 1,
    "metrics": {
      "fingerprint_distance": 0,
      "matched_query_count": 10,
      "missing_source_query_count": 0,
      "missing_target_query_count": 0
    }
  },
  "artifacts": {
    "source_fingerprint": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-source-fingerprint.json",
    "target_fingerprint": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-target-fingerprint.json",
    "result_json": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-result.json"
  },
  "safety": {
    "reset_target": true,
    "strict_count": true
  },
  "full_record_equality": {
    "enabled": true,
    "source_artifact": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-source-full-records.json",
    "target_artifact": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-target-full-records.json",
    "compare_report": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-full-record-compare.json"
  },
  "checkpoint": {
    "path": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-checkpoint.json",
    "resume_from": ""
  },
  "quality_gates": {
    "min_consistency_score": 0.999,
    "max_fingerprint_distance": 0.001,
    "passed": true
  }
}
```

Not implemented yet:

- Source cursor/page-level streaming for resume without re-reading the source result set.
- Production bulk import / COPY path.
- Automatic stale target row cleanup / reconciliation.

## Safety notes

Run this first against the local migration stack or disposable test databases.

Full-record compare artifacts can contain record IDs, scalar fields, dynamic metadata, partition values, and vector hashes. Write `--artifact-dir` only to an approved secured location, keep the generated `0600` artifacts out of chat/log output, and clean them up when diagnostics are no longer needed.

By default, the migration step uses pgvector upsert semantics and does not delete stale target records. Pass `--reset-target` for disposable smoke runs where the target table should be truncated before migration. Do not enable it against production tables unless destructive cleanup is explicitly intended.

Do not combine `--reset-target` with `--resume-from`; the command rejects this combination before migration starts.

Pass `--strict-count` when the run should fail unless the post-migration pgvector target row count exactly matches `records_written`. This is most useful together with `--reset-target` for clean smoke checks; without cleanup, stale target rows can intentionally make the strict count fail.

Pass `--min-consistency-score` and/or `--max-fingerprint-distance` when this command is used as an automated quality gate. These thresholds are evaluated after the Markdown report is written, so failed runs still leave a human-readable diagnostic artifact.

For strict production equivalence, future increments should add source-side streaming cursor semantics, bulk import strategy, and stale-row reconciliation.

## Test command

```bash
go test ./cmd/vdbg -run 'TestParseMigrateAndVerify|TestRunMigrateAndVerify' -v
go test ./internal/reporting -v
```

Full gate before commit:

```bash
make fmt
make lint
make test
make coverage-check
git diff --check
```

For migration-critical changes, also run the opt-in local Docker smoke:

```bash
make smoke-migration-checkpoint
```

The smoke starts/checks the disposable migration stack, seeds the committed small Milvus fixture, runs schema/mapping gates, performs a checkpointed migration, resumes via `migrate-and-verify`, verifies 100 target rows, checks `0600` report/checkpoint permissions, and scans generated artifacts for obvious secret markers. It is intentionally outside `make test` because it requires Docker and local ports; never point it at production databases.
