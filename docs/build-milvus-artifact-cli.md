# build-milvus-artifact CLI

`vdbg build-milvus-artifact` generates a Python-compatible source fingerprint artifact from real Milvus search results.

It is the source-side artifact bridge for the Milvus to pgvector migration-and-verification MVP.

## Command

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

## Prerequisites

Seed the Milvus collection first:

```bash
go run ./cmd/vdbg seed-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --metric cosine
```

The command does not start Docker, does not create collections, and does not mutate Milvus data.

## Behavior

The command:

1. Loads a synthetic fixture JSON file.
2. Requires at least one fixture query.
3. Connects to Milvus through the real connector SDK adapter.
4. Searches every fixture query with `expand-k` as the database search limit.
5. Converts connector hits to fingerprint builder hits.
6. Builds `stable_neighbors`, `boundary_candidates`, and `top_k_ids` through `internal/fingerprints`.
7. Writes a source fingerprint artifact JSON file.
8. Prints a compact summary with fixture, output path, collection, query count, and window settings.

`top-k` is the business-visible comparison window. `expand-k` is the larger boundary-observation window. `stable-k` controls the leading stable-neighbor set. `boundary-k` controls the rank window around the topK cutoff.

## Output artifact

The output file follows the shared fingerprint artifact schema described in:

```text
docs/fingerprint-artifact-format.md
```

Example shape:

```json
{
  "fingerprints": [
    {
      "query_id": "query-000001",
      "stable_neighbors": ["vec-000033", "vec-000096"],
      "boundary_candidates": ["vec-000005", "vec-000012"],
      "top_k_ids": ["vec-000033", "vec-000096", "vec-000005"]
    }
  ]
}
```

For the committed small fixture, the expected artifact contains `10` query fingerprints.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--fixture` | required | Synthetic fixture JSON file. |
| `--address` | required | Milvus gRPC address, for example `localhost:19530`. |
| `--output` | required | Path to write the source fingerprint artifact JSON. |
| `--collection` | `items` | Milvus collection to search. |
| `--id-field` | `id` | Primary key field returned in search results. |
| `--vector-field` | `embedding` | FloatVector field to search. |
| `--top-k` | `3` | Business-visible topK result count. |
| `--expand-k` | `5` | Expanded result count used for boundary artifact building. Must be `>= top-k + boundary-k`. |
| `--stable-k` | `2` | Leading hit count used for `stable_neighbors`. Must be `<= top-k`. |
| `--boundary-k` | `1` | Rank-window width around the topK cutoff. |
| `--metric` | `cosine` | Search metric, currently `cosine` or `l2`. |

## Current limitations

- Assumes the Milvus collection is already seeded and loaded.
- Uses fixture queries rather than sampling production queries.
- Does not compare artifacts by itself; pair this with `build-pgvector-artifact` and the Python compare engine.
- Does not perform Milvus-to-pgvector migration; it only captures source-side retrieval behavior.
