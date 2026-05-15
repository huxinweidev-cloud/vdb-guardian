# inspect-pgvector-schema CLI

`vdbg inspect-pgvector-schema` connects to PostgreSQL/pgvector and emits a read-only JSON inspection of the live target schema.

It is the phase-five validation layer in the Milvus-to-pgvector planning chain:

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
  -> inspect-pgvector-schema
```

The command only reads PostgreSQL catalog metadata. It does not create, alter, drop, truncate, delete, or migrate data.

## Command

Write an artifact:

```bash
go run ./cmd/vdbg inspect-pgvector-schema \
  --pgvector-connection-url '[REDACTED]' \
  --target-schema public \
  --output /tmp/vdb-guardian-live-pgvector-schema.json
```

Write JSON to stdout:

```bash
go run ./cmd/vdbg inspect-pgvector-schema \
  --pgvector-connection-url '[REDACTED]' \
  --target-schema public
```

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--pgvector-connection-url` | required | PostgreSQL/pgvector connection URL. It is not written to stdout or the artifact. |
| `--target-schema` | `public` | PostgreSQL schema to inspect. |
| `--output` | empty | Optional JSON output path. Without it, JSON is written to stdout. |

## Output artifact

When `--output` is provided, the file is written with `0600` permissions because live schema/topology metadata may be sensitive.

Example shape:

```json
{
  "schema_version": "v1",
  "target": {
    "type": "pgvector",
    "schema": "public"
  },
  "extension": {
    "name": "vector",
    "installed": true,
    "version": "0.8.0"
  },
  "tables": [
    {
      "target_table": "items",
      "columns": [
        {
          "name": "id",
          "type": "bigint",
          "formatted_type": "bigint",
          "nullable": false,
          "primary_key": true
        },
        {
          "name": "embedding",
          "type": "vector",
          "formatted_type": "vector(1536)",
          "nullable": false,
          "primary_key": false,
          "vector_dimension": 1536
        }
      ],
      "indexes": [
        {
          "name": "items_embedding_hnsw_idx",
          "method": "hnsw",
          "definition": "CREATE INDEX items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops)"
        }
      ]
    }
  ],
  "summary": {
    "table_count": 1,
    "column_count": 2,
    "vector_column_count": 1,
    "index_count": 1,
    "warning_count": 0
  }
}
```

## Metadata collected

- pgvector extension presence and version from `pg_extension`.
- Tables and columns from `information_schema.columns` plus `pg_catalog.format_type`.
- Vector dimensions parsed from formatted types such as `vector(1536)`.
- Primary key columns from `information_schema.table_constraints` and `key_column_usage`.
- Index names, access methods, and definitions from `pg_index`, `pg_class`, `pg_namespace`, and `pg_am`.

## Safety notes

- The command is read-only over PostgreSQL catalog metadata.
- Schema names are passed as query parameters, not string-concatenated into SQL.
- It does not inspect row payload values.
- It does not execute DDL or DML.
- It does not repair schema drift.
- It does not migrate data.
- Connection URLs are not printed or written to JSON.

## Current limitations

- This command only inventories live schema; it does not compare it with a schema plan.
- It does not check index operator classes beyond the raw `pg_get_indexdef` definition.
- It does not validate lock settings, transaction policy, or table ownership.

Use a future `compare-applied-schema` phase to compare this live inspection artifact against the planned pgvector schema artifact.
