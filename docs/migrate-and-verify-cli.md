# migrate-and-verify CLI

`vdbg migrate-and-verify` runs the first one-shot local Milvus-to-pgvector migration consistency loop.

It composes existing tested commands and boundaries:

```text
migrate Milvus -> pgvector
build Milvus source fingerprint artifact
build pgvector target fingerprint artifact
compare artifacts through the Python engine
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
  --artifact-dir /tmp/vdb-guardian-run \
  --job-id migrate-and-verify-smoke \
  --dimension 8 \
  --batch-size 100 \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
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
```

## Required flags

- `--fixture`: synthetic fixture containing verification query vectors.
- `--milvus-address`: Milvus gRPC endpoint.
- `--pgvector-connection-url`: PostgreSQL connection URL. Redact this in logs and docs.
- `--artifact-dir`: directory for source, target, and result artifacts.
- `--dimension`: expected vector dimension.

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

## Scope

Implemented:

- Real source-to-target migration.
- Source fingerprint artifact generation from Milvus.
- Target fingerprint artifact generation from pgvector.
- Artifact comparison through the Python engine.
- Summary output with record counts and primary consistency metrics.
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

Not implemented yet:

- Production checkpointing.
- Metadata columns.
- Milvus partitions.
- Automatic source/target cleanup.
- Rich Markdown/JSON report rendering beyond the existing result artifact.

## Safety notes

Run this first against the local migration stack or disposable test databases.

The migration step uses pgvector upsert semantics and does not delete stale target records. For strict production equivalence, future increments should add explicit cleanup/checkpoint semantics and metadata/partition support.

## Test command

```bash
go test ./cmd/vdbg -run 'TestParseMigrateAndVerify|TestRunMigrateAndVerify' -v
```

Full gate before commit:

```bash
make fmt
make lint
make test
git diff --check
```
