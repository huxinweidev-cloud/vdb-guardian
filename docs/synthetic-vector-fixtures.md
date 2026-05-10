# Synthetic Vector Fixtures

Synthetic vector fixtures provide deterministic datasets for the Milvus to pgvector migration-and-verification MVP. They avoid customer data and make local integration tests reproducible across machines.

## Generator command

Generate the default small fixture:

```bash
go run ./cmd/vdbg generate-synthetic-fixture \
  --output testdata/migration/synthetic-small.json \
  --seed 42 \
  --dimension 8 \
  --records 100 \
  --queries 10 \
  --metric cosine
```

The command only writes JSON. It does not start Docker, connect to Milvus, or connect to PostgreSQL.

## JSON format

```json
{
  "seed": 42,
  "dimension": 8,
  "record_count": 100,
  "query_count": 10,
  "metric": "cosine",
  "records": [
    {"id": "vec-000001", "vector": [0.1, 0.2]}
  ],
  "queries": [
    {"id": "query-000001", "vector": [0.3, 0.4]}
  ]
}
```

Records are intended to be inserted into the source Milvus collection and later migrated into pgvector. Queries are used to collect comparable search results from both databases.

## Supported dimensions

The first migration MVP validates dimensions in the range:

```text
1..2000
```

The upper bound follows pgvector's standard `vector` type compatibility boundary. The generator defaults to dimension `8` because small vectors are easier to inspect while wiring the first migration loop.

Recommended stages:

| Stage | Dimension | Purpose |
| --- | ---: | --- |
| MVP debug | 8 or 16 | Fast local loop and readable fixtures |
| Basic realistic check | 128 | Early real connector validation |
| Embedding simulation | 384 / 768 / 1536 | Common embedding-model-like dimensions |
| Boundary check | 2000 | pgvector `vector` upper-bound validation |

## Metrics

Supported metrics:

- `cosine`: vectors are L2-normalized by the generator.
- `l2`: vectors are left unnormalized for Euclidean-distance tests.

## Determinism

The generator uses a fixed seed. Re-running the same command with the same options produces the same records and queries. This property is important for comparing migration results across commits and environments.

## Current limitations

The fixture generator does not yet seed Milvus or pgvector directly. Database seeders and real connector tests will consume this fixture in later MVP steps.