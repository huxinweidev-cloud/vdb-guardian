# Local Milvus to pgvector Migration Stack

The local migration stack provides the database services needed for the first Milvus to pgvector migration-and-verification MVP. It is intentionally local-only and should not be used as production infrastructure.

## Services

The stack is defined in:

```text
deployments/docker-compose.migration.yml
```

It includes:

- `milvus-etcd`: etcd dependency for Milvus standalone.
- `milvus-minio`: object storage dependency for Milvus standalone.
- `milvus-standalone`: Milvus source vector database.
- `postgres-pgvector`: PostgreSQL target database with pgvector initialized.

## Ports

Default host ports:

| Service | Port | Purpose |
| --- | ---: | --- |
| Milvus | 19530 | gRPC SDK endpoint |
| Milvus | 9091 | HTTP health/metrics endpoint |
| PostgreSQL | 5432 | PostgreSQL endpoint |
| MinIO | 9000 | S3-compatible API |
| MinIO | 9001 | MinIO console |
| etcd | 2379 | etcd client endpoint |

Ports can be overridden with environment variables such as:

```bash
VDB_GUARDIAN_POSTGRES_PORT=15432 \
VDB_GUARDIAN_MILVUS_PORT=19531 \
docker compose -f deployments/docker-compose.migration.yml up -d
```

## Local-only credentials

The PostgreSQL service uses local-only defaults:

```text
POSTGRES_DB=vdb_guardian
POSTGRES_USER=vdb_guardian
POSTGRES_PASSWORD=vdb_guardian_local_password
```

These values are not production credentials. Do not reuse them outside local development.

## Validate without starting containers

```bash
make migration-stack-config
```

or:

```bash
scripts/check-migration-stack.sh config
```

This only validates the Compose file and does not create containers, networks, or volumes.

## Start the stack

Starting Docker has side effects: it creates containers, networks, and volumes.

```bash
make migration-stack-up
```

Equivalent command:

```bash
docker compose -f deployments/docker-compose.migration.yml up -d
```

## Check status

```bash
make migration-stack-status
```

## Health checks

After the stack is running, verify PostgreSQL and pgvector:

```bash
scripts/check-migration-stack.sh postgres
```

Verify the Milvus gRPC port:

```bash
scripts/check-migration-stack.sh milvus-port
```

## Stop and remove containers

```bash
make migration-stack-down
```

This removes containers and the Compose network. Named volumes are preserved unless removed manually.

## pgvector seed smoke check

After the PostgreSQL pgvector service is healthy, the target-side seed loop can be verified with:

```bash
go run ./cmd/vdbg seed-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --id-column id \
  --vector-column embedding
```

Then verify the seeded row count and vector dimensions:

```bash
docker compose -f deployments/docker-compose.migration.yml exec -T postgres-pgvector psql \
  -U vdb_guardian \
  -d vdb_guardian \
  -c "SELECT COUNT(*) AS seeded_records FROM items; SELECT id, vector_dims(embedding) AS dims FROM items ORDER BY id LIMIT 3;"
```

For the committed small fixture, the expected row count is `100` and the vector dimension is `8`.

## pgvector search smoke check

After the seed smoke check succeeds, the target-side read path can be verified with:

```bash
go run ./cmd/vdbg search-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

For the committed small fixture, the expected row count is `100` and the command should print `5` hits when `--expand-k 5` is used.

## pgvector target fingerprint artifact check

After the search smoke check succeeds, build a target-side fingerprint artifact from all fixture queries:

```bash
go run ./cmd/vdbg build-pgvector-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --output /tmp/vdb-guardian-target-fingerprint.json \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

Then verify the artifact JSON shape:

```bash
python -m json.tool /tmp/vdb-guardian-target-fingerprint.json >/dev/null
```

For the committed small fixture, the expected artifact contains `10` query fingerprints.

## Milvus seed smoke check

After the Milvus standalone service is healthy, seed the source-side fixture collection through the real Milvus Go SDK adapter:

```bash
go run ./cmd/vdbg seed-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --metric cosine
```

For the committed small fixture, the expected row count is `100` and the vector dimension is `8`.

## Milvus search smoke check

After the Milvus seed smoke check succeeds, the source-side read path can be verified with:

```bash
go run ./cmd/vdbg search-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

For the committed small fixture, the expected row count is `100` and the command should print `5` hits when `--expand-k 5` is used.

## Milvus source fingerprint artifact check

After the search smoke check succeeds, build a source-side fingerprint artifact from all fixture queries:

```bash
go run ./cmd/vdbg build-milvus-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --output /tmp/vdb-guardian-source-fingerprint.json \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

Then verify the artifact JSON shape:

```bash
python -m json.tool /tmp/vdb-guardian-source-fingerprint.json >/dev/null
```

For the committed small fixture, the expected artifact contains `10` query fingerprints.

## Real Milvus-to-pgvector migration check

After the Milvus source collection has been seeded and the PostgreSQL pgvector service is healthy, run the real migration path:

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
  --output /tmp/vdb-guardian-migration-report.json \
  --job-id migration-smoke
```

For the committed small fixture, the expected summary is `records_read: 100` and `records_written: 100`. The optional `--output` artifact is written as a machine-readable migration result JSON report with `0600` permissions. When schema plan/live inspection artifacts are available, add `--require-schema-match --schema-plan ... --live-schema ...` to block migration on schema drift.

## One-shot migrate-and-verify check

After the Milvus source collection has been seeded and the PostgreSQL pgvector service is healthy, run the full one-shot consistency loop:

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

For the committed small fixture and compatible source/target search behavior, the command should print `records_read: 100`, `records_written: 100`, and `matched_queries: 10`. It also writes `<artifact-dir>/<job-id>-diagnostic-report.json` with machine-readable migration counts, fingerprint metrics, artifact paths, safety flags, and quality gate status.

## Real schema-gated Milvus-to-pgvector E2E smoke

For the schema-gated real-service path, run the chain below after the stack is healthy and Milvus has been seeded. It uses only local Docker services and the committed small fixture:

```bash
export VDB_GUARDIAN_LOCAL_PG_URL='[REDACTED]'

go run ./cmd/vdbg inspect-milvus \
  --milvus-address localhost:19530 \
  --collection items \
  --output /tmp/vdb-guardian-milvus-plan.json

go run ./cmd/vdbg plan-pgvector-schema \
  --inspection-plan /tmp/vdb-guardian-milvus-plan.json \
  --output /tmp/vdb-guardian-pgvector-schema-plan.json

go run ./cmd/vdbg compare-schema-plans \
  --inspection-plan /tmp/vdb-guardian-milvus-plan.json \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --output /tmp/vdb-guardian-schema-compare-report.json

go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke \
  --pgvector-connection-url "$VDB_GUARDIAN_LOCAL_PG_URL" \
  --execute

go run ./cmd/vdbg inspect-pgvector-schema \
  --pgvector-connection-url "$VDB_GUARDIAN_LOCAL_PG_URL" \
  --target-schema public \
  --output /tmp/vdb-guardian-live-pgvector-schema.json

go run ./cmd/vdbg compare-applied-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --output /tmp/vdb-guardian-applied-schema-compare-report.json

go run ./cmd/vdbg map-migration-records \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --output /tmp/vdb-guardian-record-mapping.json

go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --source-collection items \
  --milvus-id-field id \
  --milvus-vector-field embedding \
  --pgvector-connection-url "$VDB_GUARDIAN_LOCAL_PG_URL" \
  --target-table items \
  --pgvector-id-column id \
  --pgvector-vector-column embedding \
  --dimension 8 \
  --batch-size 100 \
  --require-schema-match \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --output /tmp/vdb-guardian-migration-report.json \
  --job-id migration-smoke
```

Expected small-fixture success markers:

- `compare-schema-plans` status `pass`.
- `apply-pgvector-schema` succeeds without creating an `exact_scan` index for Milvus `FLAT`; `FLAT` means exact scan/no physical pgvector index.
- `compare-applied-schema` has `mismatch_count: 0`; extra live primary-key backing indexes may remain warnings.
- `map-migration-records` status `pass` and output permission `0600`.
- `migrate` reports `records_read: 100`, `records_written: 100`, and output permission `0600`. With `--record-mapping`, it uses the mapping artifact for source/target field projection and migrates mapped scalar/dynamic/partition metadata columns when present.
- Target row count is `100` and `vector_dims(embedding)` is `8`.

If default ports are occupied, override Compose ports before startup, for example `VDB_GUARDIAN_POSTGRES_PORT=15432` and `VDB_GUARDIAN_MILVUS_PORT=19531`, then use those same ports in every command.

## Source/target artifact comparison check

If source and target fingerprint artifacts already exist, compare them directly through the Python engine:

```bash
go run ./cmd/vdbg compare-artifacts \
  --source /tmp/vdb-guardian-source-fingerprint.json \
  --target /tmp/vdb-guardian-target-fingerprint.json \
  --artifact-dir /tmp/vdb-guardian-compare \
  --job-id real-artifact-smoke
```

The command writes:

```text
/tmp/vdb-guardian-compare/real-artifact-smoke-result.json
```

When both artifacts are built from the same committed fixture and compatible settings, the comparison should report `matched_queries: 10` and no missing source or target queries. Exact distances depend on source/target retrieval behavior.

## Full-record artifact comparison check

If source and target full-record artifacts already exist, compare them locally for scalar, dynamic metadata, partition, vector hash, and vector dimension equality:

```bash
go run ./cmd/vdbg compare-full-records \
  --source testdata/migration/source-full-record-artifact.json \
  --target testdata/migration/target-full-record-artifact.json \
  --output /tmp/vdb-guardian-full-record-compare.json
```

The command writes a `0600` JSON report. It exits non-zero on `status: fail` after preserving the report for diagnostics. Live Milvus/pgvector full-record artifact builders are not part of this stack slice yet; the committed sample artifacts exercise the artifact-only contract.

## Milvus connector smoke check

The low-level Milvus readiness check validates that the gRPC SDK endpoint is reachable:

```bash
scripts/check-migration-stack.sh milvus-port
```

## Current limitations

This stack now supports validating the pgvector target-side seed, search, and fingerprint artifact loops, source-side Milvus fixture seeding, search, and fingerprint artifact loops, real Milvus-to-pgvector migration, one-shot migrate-and-verify orchestration, direct source/target fingerprint artifact comparison, and artifact-only full-record equality comparison. Production checkpointing, live full-record artifact builders, Milvus partitions in real smoke fixtures, and cleanup policies remain future work.
