# Fingerprint Artifact Builder

The fingerprint artifact builder converts normalized search results into the JSON artifact format consumed by the Python fingerprint comparison engine.

## Purpose

The builder separates database connectivity from retrieval behavior comparison:

```text
Milvus / pgvector search results
        |
        v
normalized SearchResult values
        |
        v
Go fingerprint artifact builder
        |
        v
source-fingerprint.json / target-fingerprint.json
        |
        v
Python fingerprint engine
```

This makes connector debugging independent from fingerprint distance debugging.

## Package

Implementation lives in:

```text
internal/fingerprints
```

## Input model

```go
type SearchResult struct {
    QueryID string
    Hits    []SearchHit
}

type SearchHit struct {
    ID    string
    Rank  int
    Score float64
}
```

`Rank` is one-based, and lower rank means a better result. The current builder sorts hits by rank before deriving artifact fields.

## Build options

```go
type BuildOptions struct {
    TopK      int
    StableK   int
    BoundaryK int
}
```

- `TopK`: number of visible topK result IDs written to `top_k_ids`.
- `StableK`: number of leading IDs written to `stable_neighbors`.
- `BoundaryK`: rank-window width around the topK cutoff used for `boundary_candidates`.

The first builder uses rank-window boundary selection rather than score-delta selection. This keeps the artifact portable across vector databases whose scores may use different scales or directions.

## Boundary candidate rule

Given rank-sorted hits and `TopK = 3`, `StableK = 2`, `BoundaryK = 2`:

```text
hits: a, b, c, d, e
```

The builder outputs:

```json
{
  "query_id": "q-1",
  "stable_neighbors": ["a", "b"],
  "boundary_candidates": ["b", "c", "d", "e"],
  "top_k_ids": ["a", "b", "c"]
}
```

The boundary window includes hits near the topK cutoff on both sides. These hits are sensitive to migration drift and are later used by the Python engine to compute boundary flip rate.

## Validation

The builder rejects:

- `TopK <= 0`;
- `StableK <= 0`;
- `StableK > TopK`;
- `BoundaryK <= 0`;
- empty `query_id`;
- duplicate `query_id`;
- empty hit IDs;
- non-positive ranks;
- queries with fewer than `TopK` hits.

## Output

`WriteArtifact` writes a Python-compatible artifact JSON file:

```json
{
  "fingerprints": [
    {
      "query_id": "q-1",
      "stable_neighbors": ["a", "b"],
      "boundary_candidates": ["b", "c", "d", "e"],
      "top_k_ids": ["a", "b", "c"]
    }
  ]
}
```

See `docs/fingerprint-artifact-format.md` for the engine-facing artifact contract.
