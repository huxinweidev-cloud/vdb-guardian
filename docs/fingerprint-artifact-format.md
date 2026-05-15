# Fingerprint Artifact Format

Fingerprint artifacts capture query-level retrieval behavior from one vector database. The Python engine compares a source artifact and a target artifact to produce migration consistency metrics.

## File shape

```json
{
  "fingerprints": [
    {
      "query_id": "q-1",
      "stable_neighbors": ["a", "b", "c"],
      "boundary_candidates": ["d", "e"],
      "top_k_ids": ["a", "b", "c", "d"]
    }
  ]
}
```

## Fields

### `fingerprints`

Required list of query-level fingerprints. The list must not be empty.

### `query_id`

Stable query identifier. Source and target artifacts are aligned by this field.

Each artifact must not contain duplicate `query_id` values.

### `stable_neighbors`

Identifiers representing the stable near-neighbor set for the query. The current engine compares source and target values with Jaccard distance.

### `boundary_candidates`

Identifiers near the topK decision boundary. These candidates are especially sensitive to vector index, distance, filtering, and sorting differences across databases.

### `top_k_ids`

Identifiers visible in topK results. The engine uses this set to detect whether boundary candidates entered or left visible topK results after migration.

## Current comparison metrics

For each matched `query_id`, the engine computes:

- `stable_neighbor_distance`: Jaccard distance between stable neighbor sets.
- `boundary_candidate_distance`: Jaccard distance between boundary candidate sets.
- `boundary_flip_rate`: fraction of boundary candidates whose topK visibility changed.

Missing query IDs are treated as full-distance penalties.

The current weighted fingerprint distance is:

```text
fingerprint_distance =
  0.4 * stable_neighbor_distance
+ 0.4 * boundary_flip_rate
+ 0.2 * boundary_candidate_distance
```

The consistency score is:

```text
consistency_score = 1.0 - fingerprint_distance
```

Both values are clamped to `[0.0, 1.0]`.

## Current limitations

The first artifact format intentionally excludes score curves, filter profiles, distance metric metadata, and collection metadata. These fields can be added later without changing the core query alignment model.

## Security notes

Artifacts must contain synthetic or non-sensitive identifiers in tests. Do not place real customer content, credentials, database connection strings, or private metadata in artifact files.
