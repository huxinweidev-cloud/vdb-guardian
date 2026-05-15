# inspect-milvus CLI

`vdbg inspect-milvus` performs a read-only inspection of Milvus collection metadata and emits a machine-readable migration planning JSON document.

It is the first phase toward broader Milvus-to-pgvector migration planning. The command does not migrate records, create PostgreSQL tables, create indexes, start Docker, or mutate Milvus.

## Command

Inspect all collections:

```bash
go run ./cmd/vdbg inspect-milvus \
  --milvus-address localhost:19530 \
  --output /tmp/vdb-guardian-milvus-plan.json
```

Inspect one collection:

```bash
go run ./cmd/vdbg inspect-milvus \
  --milvus-address localhost:19530 \
  --collection items \
  --output /tmp/vdb-guardian-items-plan.json
```

Omit `--output` to print formatted JSON to stdout.

## Output summary

When `--output` is set, a successful run prints:

```text
inspection completed
output: /tmp/vdb-guardian-milvus-plan.json
collections: 1
warnings: 0
unsupported_features: 0
```

## JSON shape

The emitted plan uses schema version `v1`:

```json
{
  "schema_version": "v1",
  "source": {
    "type": "milvus",
    "address": "localhost:19530"
  },
  "collections": [
    {
      "name": "items",
      "row_count": 100,
      "description": "product embeddings",
      "auto_id": false,
      "dynamic_field_enabled": false,
      "primary_key": "id",
      "fields": [
        {
          "name": "id",
          "source_type": "VarChar",
          "target_type": "varchar(64)",
          "max_length": 64,
          "primary_key": true,
          "nullable": false,
          "support_level": "supported"
        },
        {
          "name": "embedding",
          "source_type": "FloatVector",
          "target_type": "vector(8)",
          "dimension": 8,
          "nullable": false,
          "support_level": "supported"
        }
      ],
      "indexes": [
        {
          "field": "embedding",
          "source_index_type": "HNSW",
          "source_metric": "COSINE",
          "target_index_type": "hnsw",
          "target_ops": "vector_cosine_ops",
          "support_level": "degraded"
        }
      ],
      "partitions": [
        {
          "name": "_default",
          "support_level": "degraded",
          "recommended_strategy": "metadata_column"
        }
      ]
    }
  ],
  "summary": {
    "collection_count": 1,
    "supported_collection_count": 1,
    "warning_count": 0,
    "unsupported_feature_count": 0
  }
}
```

## Type mapping

The phase-one planner recommends target types but does not execute DDL.

| Milvus type | Target recommendation | Support level |
|---|---|---|
| Bool | `boolean` | supported |
| Int8 / Int16 | `smallint` | supported |
| Int32 | `integer` | supported |
| Int64 | `bigint` | supported |
| Float | `real` | supported |
| Double | `double precision` | supported |
| VarChar | `varchar(n)` or `text` | supported |
| JSON | `jsonb` | supported |
| FloatVector | `vector(dim)` | supported |
| BinaryVector | `bytea` | degraded |
| SparseFloatVector | `jsonb` | degraded |
| Array | `jsonb` | degraded |

## Index and partition planning

- Milvus `HNSW` is recommended as pgvector `hnsw` with metric-specific operator classes when known.
- Milvus `IVF_FLAT` / `IVFFLAT` is recommended as pgvector `ivfflat` with metric-specific operator classes when known.
- Milvus `FLAT` is represented as exact scan/no approximate index.
- Unknown index types are reported as `unsupported` metadata-only features.
- Partitions are preserved as metadata with recommended strategy `metadata_column`; phase one does not create PostgreSQL declarative partitions.

## Safety notes

- The command only reads collection names, schema metadata, row counts, vector index metadata, and partition names.
- It does not read vector payloads or scalar record values.
- It does not write to Milvus or PostgreSQL.
- Redact real production addresses in shared logs if they are sensitive in your environment.

## Test command

```bash
go test ./internal/inspection ./cmd/vdbg -run 'TestMilvusInspector|TestMapMilvus|TestBuildMilvus|TestRunInspectMilvus|TestParseInspectMilvus' -v
```

Full gate before commit:

```bash
make fmt
make lint
make test
git diff --check
```
