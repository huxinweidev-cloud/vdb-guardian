# migrate CLI

`vdbg migrate` runs the first real Milvus-to-pgvector record transfer path.

It can run in the original `id + vector` mode, or consume a `vdbg map-migration-records` JSON artifact through `--record-mapping` to migrate mapped scalar fields, dynamic metadata, and partition metadata alongside the primary key and vector.

It reads normalized records from a Milvus source collection through the Milvus SDK query path and writes them into a pgvector target table through the pgx-backed writer.

The command does not start Docker, create services, build fingerprint artifacts, or compare retrieval behavior. It assumes the local migration stack or equivalent disposable test databases are already running.

## Command

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --source-collection items \
  --milvus-id-field id \
  --milvus-vector-field embedding \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --pgvector-id-column id \
  --pgvector-vector-column embedding \
  --dimension 8 \
  --batch-size 100 \
  --require-schema-match \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --checkpoint-path /tmp/vdb-guardian-migration-checkpoint.json \
  --output /tmp/vdb-guardian-migration-report.json \
  --job-id migration-smoke
```

## Output

A successful run prints a compact summary:

```text
migration completed
source_collection: items
target_table: items
dimension: 8
records_read: 100
records_written: 100
```

When `--output` is provided, the command also writes a machine-readable JSON report with `0600` permissions. The report records the job id, source collection, target table, schema preflight status, optional record-mapping summary metadata, optional checkpoint summary, dimension, records read, and records written. It never includes the PostgreSQL connection URL.

For pre-migration full-record mapping validation, run `vdbg map-migration-records` against the same schema plan before execution. That command is local-artifact only and does not connect to Milvus or PostgreSQL.

## Optional full-record mapping

Use `--record-mapping` to make `migrate` consume the machine-readable output from `vdbg map-migration-records`:

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --dimension 8 \
  --record-mapping /tmp/vdb-guardian-record-mapping.json
```

When supplied, the mapping artifact is the source of truth for source collection, target table, primary key, vector field, scalar columns, dynamic metadata, and partition metadata. The command rejects the run before creating a runner if the mapping status is not `pass`, if it contains anything other than one collection mapping, or if the primary key/vector mapping is missing. Loading the mapping artifact is local-only and does not connect to Milvus or PostgreSQL.

## Optional schema preflight

Use `--require-schema-match` to make standalone migration depend on the planned-vs-live schema drift gate:

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --source-collection items \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --dimension 1536 \
  --require-schema-match \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --output /tmp/vdb-guardian-migration-report.json \
  --job-id migration-smoke
```

With `--require-schema-match`, both `--schema-plan` and `--live-schema` are required. The command reuses the same artifact-only comparison as `vdbg compare-applied-schema`; if blocking drift exists, migration does not start.

## Optional checkpoint / resume

Use `--checkpoint-path` to write batch-level progress after every successful pgvector write batch and after a failed write batch before the command returns an error:

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --dimension 1536 \
  --batch-size 1000 \
  --checkpoint-path /secure/artifacts/migration-checkpoint.json
```

Resume from a failed or running checkpoint with `--resume-from`:

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --dimension 1536 \
  --batch-size 1000 \
  --resume-from /secure/artifacts/migration-checkpoint.json
```

If `--resume-from` is supplied without `--checkpoint-path`, the command writes updated progress back to the same checkpoint file. Resume validation fails closed when the checkpoint source collection, target table, dimension, batch size, schema-plan fingerprint, record-mapping fingerprint, or status is unsafe. Completed checkpoints are not accepted for resume.

Checkpoint files are written with `0600` permissions. They contain non-secret migration identity, record counts, completed/failed batch ranges, and resume offsets. They never contain PostgreSQL connection URLs, credentials, tokens, raw vectors, or row payloads.

MVP limitation: the source Milvus reader still loads the source result set before pgvector batch writes. Checkpointing currently protects target write batch progress and resume offset; source cursor/page-level streaming remains future work.

## Required flags

- `--milvus-address`: Milvus gRPC endpoint, for example `localhost:19530`.
- `--pgvector-connection-url`: PostgreSQL connection URL. Redact this in logs and docs.
- `--dimension`: expected vector dimension. The runner validates every migrated vector against this value.

## Optional flags

- `--require-schema-match`: require planned-vs-live schema comparison to pass before migration starts.
- `--schema-plan`: pgvector schema plan JSON path. Required when `--require-schema-match` is set.
- `--live-schema`: live pgvector schema inspection JSON path. Required when `--require-schema-match` is set.
- `--record-mapping`: optional `vdbg map-migration-records` JSON path for mapping-driven full-record migration. The artifact must have `status: pass` and exactly one collection mapping.
- `--checkpoint-path`: optional migration checkpoint JSON path. Written with `0600` permissions.
- `--resume-from`: optional checkpoint JSON path to resume from. Defaults `--checkpoint-path` to the same file when `--checkpoint-path` is omitted.
- `--output`: optional migration result report JSON path. Written with `0600` permissions.
- `--job-id`: optional identifier included in the migration result report and checkpoint artifact.

## Defaults

- `--source-collection`: `items`
- `--target-table`: `items`
- `--milvus-id-field`: `id`
- `--milvus-vector-field`: `embedding`
- `--pgvector-id-column`: `id`
- `--pgvector-vector-column`: `embedding`
- `--batch-size`: `100`

## Scope

`vdbg inspect-milvus` complements this command by generating a read-only migration planning JSON document from Milvus metadata before record transfer. `vdbg plan-pgvector-schema` can then turn that inspection plan into a dry-run pgvector schema/DDL plan, `vdbg compare-schema-plans` validates the two planning artifacts before any DDL apply step, `vdbg apply-pgvector-schema` can dry-run or execute the validated pgvector schema DDL, `vdbg inspect-pgvector-schema` inventories the live target schema after apply, and `vdbg compare-applied-schema` validates planned-vs-live schema drift before record migration. See `docs/inspect-milvus-cli.md`, `docs/plan-pgvector-schema-cli.md`, `docs/compare-schema-plans-cli.md`, `docs/apply-pgvector-schema-cli.md`, and `docs/inspect-pgvector-schema-cli.md`.

Implemented:

- Real Milvus SDK-backed source read.
- Real pgx-backed pgvector target upsert.
- Dimension validation.
- CLI flag parsing and injected-runner unit tests.
- Optional planned-vs-live schema preflight via `--require-schema-match`.
- Optional machine-readable migration result JSON report via `--output`.
- Optional mapping-driven full-record execution via `--record-mapping`, including scalar fields, dynamic metadata, and partition metadata from a passing local mapping artifact.
- Optional batch-level checkpoint and resume via `--checkpoint-path` and `--resume-from`.
- Summary output for records read and written.

Not implemented yet:

- Source/target fingerprint artifact generation inside this command.
- Comparison result artifact generation inside this command.
- Full-record equality comparison inside this command; use `vdbg migrate-and-verify --full-record-compare` for the orchestrated gate.
- Production bulk import.

## Safety notes

Run this only against local migration MVP services or disposable test databases until production migration semantics are explicitly added.

The pgvector writer uses upsert semantics:

```sql
INSERT ... ON CONFLICT (id) DO UPDATE
```

It does not drop the target table. If the target table contains old records not present in the Milvus source, this command does not delete them.

Checkpoint and report artifacts may contain collection names, table names, IDs/ranges, and artifact paths. Store them only in approved secured locations and keep generated artifacts out of chat/log output. Do not put credentials or connection URLs into checkpoint paths or job ids.

## Test command

```bash
go test ./internal/migration -run 'Test.*Checkpoint|Test.*Resume|TestVectorMigrationRunner' -v
go test ./cmd/vdbg -run 'TestParseMigrate|TestRunMigrate' -v
```

Full gate before commit:

```bash
make fmt
make lint
make test
git diff --check
```
