# compare-applied-schema CLI

`vdbg compare-applied-schema` compares a planned pgvector schema artifact with a live PostgreSQL/pgvector schema inspection artifact.

It is the phase-six read-only drift gate in the Milvus-to-pgvector planning chain:

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
  -> inspect-pgvector-schema
  -> compare-applied-schema
```

The command only reads local JSON files. It does not connect to PostgreSQL, does not execute DDL/DML, and does not repair drift.

## Usage

```bash
go run ./cmd/vdbg compare-applied-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --output /tmp/vdb-guardian-applied-schema-compare-report.json
```

Without `--output`, the JSON report is written to stdout:

```bash
go run ./cmd/vdbg compare-applied-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json
```

## Flags

| Flag | Required | Description |
| --- | --- | --- |
| `--schema-plan` | yes | Path to a `plan-pgvector-schema` JSON artifact. |
| `--live-schema` | yes | Path to an `inspect-pgvector-schema` JSON artifact. |
| `--output` | no | Path for the comparison report. When omitted, report JSON is printed to stdout. |

## Report statuses

| Status | Meaning |
| --- | --- |
| `pass` | Planned schema and live schema satisfy all blocking checks. |
| `warn` | No blocking drift was found, but extra live objects or degraded planned features require review. |
| `fail` | Blocking drift was found. The command returns non-zero after writing the report. |

## Blocking checks

The command fails the comparison when any of these are true:

- unsupported schema plan artifact version;
- unsupported live schema inspection artifact version;
- planned target schema differs from live target schema;
- planned table is missing live;
- planned column is missing live;
- planned column type differs live;
- planned vector dimension differs live;
- planned nullable flag differs live;
- planned primary key is not primary key live;
- pgvector extension is missing while vector columns are planned;
- planned supported index is missing live;
- planned supported index method differs live.

## Warnings

Warnings are reported but do not fail the command by themselves:

- extra live table not present in the schema plan;
- extra live column not present in the schema plan;
- extra live index not present in the schema plan;
- warning text already carried by planned columns or indexes.

## Example report

```json
{
  "schema_version": "v1",
  "status": "pass",
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "live_schema": "/tmp/vdb-guardian-live-pgvector-schema.json",
  "summary": {
    "tables_checked": 1,
    "columns_checked": 2,
    "indexes_checked": 1,
    "mismatch_count": 0,
    "warning_count": 0
  },
  "tables": [
    {
      "target_table": "items",
      "status": "pass",
      "checks": [
        {
          "name": "table_present",
          "status": "pass",
          "source": "items",
          "target": "items"
        },
        {
          "name": "vector_dimension_preserved",
          "status": "pass",
          "source": "vector(1536)",
          "target": "vector(1536)",
          "detail": "embedding"
        }
      ]
    }
  ]
}
```

## Safety notes

- The command is read-only over local JSON artifacts.
- It never connects to PostgreSQL.
- It never executes SQL.
- Report files are written with `0600` permissions because schema/topology metadata may be sensitive.
- Database connection strings are not part of the input artifact contract and are not emitted.

## Current limitations

- Index operator class equivalence is not yet checked; the first gate validates planned index name and method.
- Extra live objects are warnings rather than blockers.
- The command does not repair drift.
- The command does not validate row data or vector payloads.

After this gate passes, a later full-record migration phase can use the schema confidence established by the planning/apply/live-inspection chain.
