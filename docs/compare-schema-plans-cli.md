# compare-schema-plans CLI

`vdbg compare-schema-plans` compares a read-only Milvus inspection plan with a dry-run pgvector schema plan before any PostgreSQL DDL is applied.

It is the phase-three safety gate in the Milvus-to-pgvector planning chain:

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema later
```

The command does not connect to Milvus, does not connect to PostgreSQL, and does not execute SQL.

## Command

```bash
go run ./cmd/vdbg compare-schema-plans \
  --inspection-plan /tmp/vdb-guardian-milvus-plan.json \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --output /tmp/vdb-guardian-schema-compare-report.json
```

When `--output` is omitted, the JSON report is written to stdout.

## Output

Successful file output prints a concise summary:

```text
schema comparison completed
output: /tmp/vdb-guardian-schema-compare-report.json
status: pass
mismatches: 0
warnings: 0
unsupported_features: 0
```

If blocking mismatches exist, the command still writes the JSON report, then exits non-zero with a `schema comparison failed` error.

## JSON shape

The emitted report uses schema version `v1`:

```json
{
  "schema_version": "v1",
  "status": "pass",
  "inspection_plan": "/tmp/vdb-guardian-milvus-plan.json",
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "summary": {
    "collections_checked": 1,
    "tables_checked": 1,
    "fields_checked": 3,
    "columns_checked": 5,
    "mismatch_count": 0,
    "warning_count": 0,
    "unsupported_feature_count": 0
  },
  "collections": [
    {
      "source_collection": "items",
      "target_table": "items",
      "status": "pass",
      "checks": [
        {
          "name": "primary_key_preserved",
          "status": "pass",
          "source": "id",
          "target": "id"
        },
        {
          "name": "vector_dimension_preserved",
          "status": "pass",
          "source": "FloatVector(8)",
          "target": "vector(8)",
          "detail": "embedding"
        }
      ]
    }
  ]
}
```

## Comparison rules

The first implementation checks:

- every inspected Milvus collection has one target table plan;
- sanitized collection names match target table names;
- every inspected Milvus field has a target column;
- field target types match the inspection recommendation;
- primary keys remain primary keys;
- nullability is preserved;
- dense float vector dimensions remain `vector(N)`;
- `dynamic_field_enabled=true` maps to `_milvus_dynamic jsonb`;
- source partitions map to `_milvus_partition text`;
- supported non-FLAT index recommendations have target index plans and `create_index_sql`;
- FLAT index recommendations are treated as exact scan / no approximate index;
- unsupported features and warnings are surfaced in the summary instead of silently passing.

## Status values

| Status | Meaning |
| --- | --- |
| `pass` | No blocking mismatches or warnings were found. |
| `warn` | No blocking mismatch was found, but degraded/unsupported features or warnings require review. |
| `fail` | Blocking schema mismatches were found; do not apply the schema plan without review. |

## Safety notes

- The command is read-only over local JSON files.
- It writes reports with `0600` file permissions because schema/topology metadata may be sensitive.
- It does not include database connection strings or credentials in reports.
- It should run before any future `apply-pgvector-schema` execution step.
