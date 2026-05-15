# apply-pgvector-schema CLI

`vdbg apply-pgvector-schema` applies, or dry-runs application of, a previously generated pgvector schema plan.

It is the phase-four execution boundary in the Milvus-to-pgvector planning chain:

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
```

The command is safe by default: if neither `--dry-run` nor `--execute` is provided, it runs in dry-run mode and does not connect to PostgreSQL.

## Commands

Dry-run without database credentials:

```bash
go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke
```

Explicit dry-run:

```bash
go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke \
  --dry-run
```

Execute DDL:

```bash
go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --pgvector-connection-url '[REDACTED]' \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke \
  --execute
```

Skip index DDL while creating extension/table DDL:

```bash
go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --pgvector-connection-url '[REDACTED]' \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke \
  --execute \
  --skip-indexes
```

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--schema-plan` | required | Path to a `vdbg plan-pgvector-schema` JSON artifact. |
| `--artifact-dir` | `artifacts` | Directory for the apply report. |
| `--job-id` | `pgvector-schema-apply` | Job identifier used in the report filename. |
| `--dry-run` | implicit default | Do not connect to PostgreSQL or execute SQL. |
| `--execute` | false | Execute schema DDL through PostgreSQL/pgvector. |
| `--pgvector-connection-url` | required only with `--execute` | PostgreSQL connection URL. It is not written to the report. |
| `--skip-indexes` | false | In execute mode, create tables but skip index DDL. |
| `--allow-unsupported` | false | Allow execute mode when the schema plan contains unsupported features. |

## Output artifact

The command writes:

```text
<artifact-dir>/<job-id>-pgvector-schema-apply-report.json
```

The report file is written with `0600` permissions because schema artifacts may contain topology or field metadata.

Example shape:

```json
{
  "schema_version": "v1",
  "job_id": "schema-apply-smoke",
  "mode": "dry_run",
  "status": "planned",
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "target": {
    "type": "pgvector",
    "schema": "public"
  },
  "tables": [
    {
      "source_collection": "items",
      "target_table": "items",
      "create_table_sql": "CREATE EXTENSION IF NOT EXISTS vector;\nCREATE TABLE IF NOT EXISTS public.items (...);",
      "applied": false,
      "indexes": [
        {
          "source_field": "embedding",
          "target_index": "items_embedding_hnsw_idx",
          "create_index_sql": "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops);",
          "applied": false
        }
      ]
    }
  ],
  "summary": {
    "table_count": 1,
    "table_applied_count": 0,
    "index_count": 1,
    "index_applied_count": 0,
    "warning_count": 0,
    "unsupported_feature_count": 0
  }
}
```

Statuses:

- `planned`: dry-run completed without executing SQL.
- `applied`: execute mode applied all selected table/index statements.
- `blocked`: execution was blocked before SQL ran, such as unsupported features without `--allow-unsupported`.
- `failed`: SQL execution failed after zero or more statements.

## Safety notes

- Default mode is dry-run.
- Execute mode requires explicit `--execute` and `--pgvector-connection-url`.
- The connection URL is not written to stdout or JSON report.
- The command executes only `CREATE EXTENSION IF NOT EXISTS vector`, `CREATE TABLE IF NOT EXISTS ...`, and optionally `CREATE INDEX IF NOT EXISTS ...` from a generated schema plan.
- It does not drop, truncate, delete, or alter existing data.
- Unsupported features block execute mode unless `--allow-unsupported` is provided.
- Dry-run does not connect to PostgreSQL.

## Current limitations

- Existing table drift is not inspected or repaired.
- No transaction/lock policy is exposed yet.
- No data migration is performed.
- No checkpoint/resume semantics are implemented for schema application.

Use future `inspect-pgvector-schema` and `compare-applied-schema` phases to validate live PostgreSQL schema after execution.
