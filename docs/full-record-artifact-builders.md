# Full-record artifact builders

`vdbg build-milvus-record-artifact` and `vdbg build-pgvector-record-artifact` generate local full-record artifacts from live services using a passing `map-migration-records` artifact.

These builders close the loop for mapping-driven full-record migration validation:

```text
map-migration-records
-> migrate --record-mapping
-> build-milvus-record-artifact
-> build-pgvector-record-artifact
-> compare-full-records
```

## Safety boundary

Both builder commands are read-only:

- they require an existing mapping artifact with `status: pass`;
- they reject multi-collection mapping artifacts in this phase;
- they reject mappings missing primary-key or vector fields;
- they do not execute DDL/DML;
- they write output JSON with final permission `0600`, including overwrite cases;
- they do not print PostgreSQL connection URLs.

## Milvus source artifact

```bash
go run ./cmd/vdbg build-milvus-record-artifact \
  --milvus-address localhost:19530 \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --output /tmp/vdb-guardian-source-full-records.json
```

The command reads the mapped Milvus source fields:

- primary key source field;
- vector source field;
- mapped scalar source fields;
- dynamic metadata source field, if present;
- partition metadata source field, if present.

Output metadata:

```json
{
  "schema_version": "v1",
  "system": "milvus",
  "collection": "items",
  "record_mapping_path": "/tmp/vdb-guardian-record-mapping.json",
  "records": []
}
```

## pgvector target artifact

```bash
go run ./cmd/vdbg build-pgvector-record-artifact \
  --pgvector-connection-url '[REDACTED]' \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --output /tmp/vdb-guardian-target-full-records.json
```

The command reads the mapped pgvector target columns and maps scalar values back to the source field names used by the Milvus artifact, so `compare-full-records` can compare source and target artifacts without target-column naming drift.

The pgvector reader builds a read-only `SELECT` with validated quoted identifiers and orders by the mapped ID column for deterministic output.

Output metadata:

```json
{
  "schema_version": "v1",
  "system": "pgvector",
  "collection": "items",
  "record_mapping_path": "/tmp/vdb-guardian-record-mapping.json",
  "records": []
}
```

## Vector representation

Artifacts store vector hashes and dimensions instead of raw vector payloads:

```json
{
  "id": "sku-1",
  "vector_hash": "sha256:...",
  "vector_dimension": 8,
  "scalars": {"product_title": "First"},
  "dynamic_metadata": {"brand": "acme"},
  "partition": "tenant_a"
}
```

Vector values are normalized through a deterministic float32-compatible representation before hashing so Milvus float32 reads and pgvector float64/text reads compare consistently for the same migrated vector.

## Compare artifacts

```bash
go run ./cmd/vdbg compare-full-records \
  --source /tmp/vdb-guardian-source-full-records.json \
  --target /tmp/vdb-guardian-target-full-records.json \
  --output /tmp/vdb-guardian-full-record-compare.json
```

`compare-full-records` remains artifact-only: it does not connect to Milvus or PostgreSQL.

## Current boundary

This phase adds the live read-only artifact builders and local full-record equality comparison chain. Automatic orchestration inside `migrate-and-verify --full-record-compare` remains a follow-up phase.
