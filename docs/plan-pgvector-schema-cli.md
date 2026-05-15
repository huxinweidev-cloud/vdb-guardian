# plan-pgvector-schema CLI

`vdbg plan-pgvector-schema` reads a Milvus inspection plan and emits a dry-run PostgreSQL/pgvector schema plan with deterministic DDL previews.

It is phase two of broader Milvus-to-pgvector migration planning. The command does not connect to PostgreSQL, execute DDL, create tables, create indexes, or migrate records.

## Command

```bash
go run ./cmd/vdbg plan-pgvector-schema \
  --inspection-plan /tmp/vdb-guardian-milvus-plan.json \
  --output /tmp/vdb-guardian-pgvector-schema-plan.json
```

Use a custom target schema:

```bash
go run ./cmd/vdbg plan-pgvector-schema \
  --inspection-plan /tmp/vdb-guardian-milvus-plan.json \
  --target-schema public \
  --output /tmp/vdb-guardian-pgvector-schema-plan.json
```

Omit `--output` to print formatted JSON to stdout.

## Output summary

When `--output` is set, a successful run prints:

```text
schema plan completed
output: /tmp/vdb-guardian-pgvector-schema-plan.json
tables: 1
warnings: 0
unsupported_features: 0
```

## JSON shape

The emitted plan uses schema version `v1`:

```json
{
  "schema_version": "v1",
  "source_plan": "/tmp/vdb-guardian-milvus-plan.json",
  "target": {
    "type": "pgvector",
    "schema": "public"
  },
  "tables": [
    {
      "source_collection": "items",
      "target_schema": "public",
      "target_table": "items",
      "columns": [
        {
          "source_field": "id",
          "target_column": "id",
          "target_type": "varchar(64)",
          "primary_key": true,
          "nullable": false,
          "support_level": "supported"
        },
        {
          "source_field": "embedding",
          "target_column": "embedding",
          "target_type": "vector(8)",
          "nullable": false,
          "support_level": "supported"
        }
      ],
      "create_table_sql": "CREATE EXTENSION IF NOT EXISTS vector;\nCREATE TABLE IF NOT EXISTS public.items (...);",
      "indexes": [
        {
          "source_field": "embedding",
          "target_schema": "public",
          "target_table": "items",
          "target_column": "embedding",
          "target_index": "items_embedding_hnsw_idx",
          "target_index_type": "hnsw",
          "target_ops": "vector_cosine_ops",
          "create_index_sql": "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops);",
          "support_level": "degraded"
        }
      ]
    }
  ],
  "summary": {
    "table_count": 1,
    "warning_count": 0,
    "unsupported_feature_count": 0
  }
}
```

## Identifier rules

Target schemas, tables, columns, and index names are generated from source names with deterministic PostgreSQL-safe identifiers:

- lowercase names;
- unsupported characters become underscores;
- leading digits are prefixed with `t_`;
- PostgreSQL reserved words receive a trailing underscore;
- duplicate sanitized field names receive numeric suffixes.

Examples:

| Source | Target |
|---|---|
| `Items.Collection` | `items_collection` |
| `User-Profile.Vector` | `user_profile_vector` |
| `123items` | `t_123items` |
| `select` | `select_` |

## Dynamic fields and partitions

- If the Milvus collection has dynamic fields enabled, the plan adds `_milvus_dynamic jsonb` with support level `degraded`.
- If the Milvus collection has partitions, the plan adds `_milvus_partition text` with support level `degraded`.
- Phase two does not create PostgreSQL declarative partitions.

## Safety notes

- The command reads only the local inspection plan JSON.
- It does not connect to Milvus or PostgreSQL.
- It does not execute generated SQL.
- Output files are written with `0600` permissions because schema/topology metadata may be sensitive.

## Test command

```bash
go test ./internal/schema ./cmd/vdbg -run 'TestSanitizePGIdentifier|TestBuildPGVectorSchemaPlan|TestRenderCreateTableSQL|TestRenderPGVectorIndexSQL|TestRunPlanPGVectorSchema|TestParsePlanPGVectorSchema' -v
```

Full gate before commit:

```bash
make fmt
make lint
make test
git diff --check
```
