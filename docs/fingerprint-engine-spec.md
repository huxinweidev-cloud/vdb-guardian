# Fingerprint Engine Specification

The fingerprint engine computes retrieval behavior differences between source and target vector databases.

## Initial metrics

The current implementation includes:

- Boundary candidate selection.
- Jaccard distance.
- Boundary flip rate.
- Weighted fingerprint distance.
- Artifact-backed source/target fingerprint comparison.
- Consistency scoring.

## Boundary candidates

Boundary candidates are hits near the topK decision boundary whose score is close to the K-th result. They are important because migration-related indexing, distance, or filtering differences often cause these candidates to enter or leave visible topK results.

## Artifact-backed comparison

The compare command reads source and target fingerprint artifact files. See `docs/fingerprint-artifact-format.md` for the artifact schema.

For each matched `query_id`, the engine computes:

- `stable_neighbor_distance`: Jaccard distance between stable neighbor sets.
- `boundary_candidate_distance`: Jaccard distance between boundary candidate sets.
- `boundary_flip_rate`: fraction of boundary candidates whose topK visibility changed.

Missing query IDs are treated as full-distance penalties.

## Distance metrics

The engine returns normalized values in `[0, 1]` where possible. Lower fingerprint distance means source and target retrieval behavior are more similar. Higher consistency score means better migration consistency.

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

## Protocol direction

The Go control plane invokes the Python engine with a subprocess runner and a JSON file protocol:

```text
python -m vdb_fingerprint_engine.cli compare --input input.json --output output.json
```

The Python engine returns a compact JSON summary and will write detailed artifacts through the artifact boundary in later phases.

See `docs/engine-protocol.md` for the current schema.
