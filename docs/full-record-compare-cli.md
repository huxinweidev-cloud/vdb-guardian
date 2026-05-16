# Full-record comparison CLI

`vdbg compare-full-records` compares two local full-record artifacts and writes a machine-readable equality report.

This command is artifact-only: it does not connect to Milvus, PostgreSQL, or pgvector, and it does not mutate either system. It is intended to follow mapping-driven migration execution from `vdbg migrate --record-mapping` after source and target full-record artifacts have been produced by a controlled workflow.

## Usage

```bash
go run ./cmd/vdbg compare-full-records \
  --source /tmp/vdb-guardian-source-records.json \
  --target /tmp/vdb-guardian-target-records.json \
  --output /tmp/vdb-guardian-full-record-compare.json
```

For the committed sample artifacts:

```bash
go run ./cmd/vdbg compare-full-records \
  --source testdata/migration/source-full-record-artifact.json \
  --target testdata/migration/target-full-record-artifact.json \
  --output /tmp/vdb-guardian-full-record-compare.json
```

The output report is written with `0600` permissions.

## Artifact schema

Input artifacts use schema version `v1`:

```json
{
  "schema_version": "v1",
  "system": "milvus",
  "collection": "items",
  "record_mapping_path": "/tmp/vdb-guardian-record-mapping.json",
  "records": [
    {
      "id": "sku-1",
      "vector_hash": "sha256:...",
      "vector_dimension": 8,
      "scalars": {"title": "First", "price": 9.5},
      "dynamic_metadata": {"brand": "acme", "tags": ["sale"]},
      "partition": "tenant_a"
    }
  ]
}
```

The artifact stores vector hashes and dimensions instead of raw vectors so reports stay compact and deterministic.

## Report schema

The report includes endpoint summaries, missing ID lists, field-level mismatches, and aggregate counters:

```json
{
  "schema_version": "v1",
  "status": "pass",
  "source": {"system": "milvus", "collection": "items", "record_count": 100},
  "target": {"system": "pgvector", "collection": "items", "record_count": 100},
  "summary": {
    "matched_records": 100,
    "missing_source_records": 0,
    "missing_target_records": 0,
    "mismatched_records": 0,
    "scalar_mismatches": 0,
    "dynamic_metadata_mismatches": 0,
    "partition_mismatches": 0,
    "vector_mismatches": 0
  },
  "missing_source_ids": [],
  "missing_target_ids": [],
  "mismatches": []
}
```

## Status semantics

- `pass`: all record IDs match and all compared fields are equal.
- `fail`: any source-only row, target-only row, scalar mismatch, dynamic metadata mismatch, partition mismatch, vector hash mismatch, or vector dimension mismatch was detected.

When the comparison fails, the command still writes the diagnostic JSON report and exits non-zero.

## Current boundary

This slice provides the artifact contract and local comparison CLI. Live Milvus/pgvector full-record artifact builders and automatic `migrate-and-verify --full-record-compare` orchestration are intentionally left for follow-up phases after the artifact-only comparison contract is stable.
