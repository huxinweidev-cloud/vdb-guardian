# Engine Protocol

The engine protocol defines how the Go control plane invokes the Python retrieval behavior fingerprint engine.

## Current execution mode

The current implementation uses a Python subprocess:

```text
Go PythonRunner
  -> python -m vdb_fingerprint_engine.cli compare --input input.json --output output.json
  -> JSON CompareOutput
```

This keeps the first enterprise deployment simple while preserving a stable boundary that can later evolve into gRPC, HTTP, or a remote Python service.

## Go runner

The Go implementation lives in `internal/engine/python_runner.go`.

```go
runner := engine.NewPythonRunner("/path/to/python", "/path/to/repo/python")
output, err := runner.Compare(ctx, engine.CompareInput{
    JobID: "job-1",
    SourceFingerprintPath: "source.json",
    TargetFingerprintPath: "target.json",
})
```

The runner:

1. Creates a temporary working directory.
2. Writes `input.json`.
3. Runs the Python CLI compare command.
4. Reads `output.json`.
5. Converts snake_case JSON into Go structs.
6. Returns diagnostic errors with subprocess output when execution fails.

## Input JSON

```json
{
  "job_id": "job-1",
  "source_fingerprint_path": "source.json",
  "target_fingerprint_path": "target.json"
}
```

Fields:

- `job_id`: stable verification job identifier.
- `source_fingerprint_path`: artifact path for source retrieval behavior fingerprints.
- `target_fingerprint_path`: artifact path for target retrieval behavior fingerprints.

The source and target files must use the format documented in `docs/fingerprint-artifact-format.md`.

## Output JSON

```json
{
  "job_id": "job-1",
  "consistency_score": 0.76,
  "metrics": {
    "fingerprint_distance": 0.24,
    "stable_neighbor_distance": 0.25,
    "boundary_candidate_distance": 0.1,
    "boundary_flip_rate": 0.2,
    "matched_query_count": 10,
    "missing_source_query_count": 0,
    "missing_target_query_count": 0
  }
}
```

Fields:

- `job_id`: copied from the input payload.
- `consistency_score`: normalized score in `[0, 1]`; higher means more consistent.
- `metrics.fingerprint_distance`: weighted normalized fingerprint distance.
- `metrics.stable_neighbor_distance`: average Jaccard distance between stable-neighbor sets.
- `metrics.boundary_candidate_distance`: average Jaccard distance between boundary-candidate sets.
- `metrics.boundary_flip_rate`: normalized boundary candidate topK flip rate.
- `metrics.matched_query_count`: number of query IDs present in both artifacts.
- `metrics.missing_source_query_count`: number of target query IDs missing from the source artifact.
- `metrics.missing_target_query_count`: number of source query IDs missing from the target artifact.

## Current behavior

The Python compare command now reads source and target fingerprint artifact JSON files and computes artifact-backed consistency metrics. Missing query IDs are treated as full-distance penalties.

## Security notes

- Do not place credentials in engine input JSON.
- Do not log production DSNs or secret-bearing artifact paths.
- The Go runner writes temporary input files with `0600` permissions.
- Temporary files are removed after each run.
