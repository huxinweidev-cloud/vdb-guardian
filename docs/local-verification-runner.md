# Local Verification Runner

The local verification runner is the first Go orchestration layer above the fingerprint engine. It runs an artifact-backed verification job without connecting to a real vector database.

## Purpose

The runner proves the local end-to-end path before Milvus and pgvector connectors are added:

```text
source fingerprint artifact
        |
target fingerprint artifact
        |
        v
Go VerificationRunner
        |
        v
engine.Engine Compare
        |
        v
result artifact JSON
```

This keeps algorithm validation separate from database connectivity and makes later integration debugging easier.

## Go API

The implementation lives in `internal/jobs/runner.go`.

```go
runner := jobs.NewVerificationRunner(engine, "artifacts")
result, err := runner.Run(ctx, jobs.VerificationRequest{
    JobID: "job-1",
    SourceFingerprintPath: "source.json",
    TargetFingerprintPath: "target.json",
})
```

## Request

```go
type VerificationRequest struct {
    JobID string
    SourceFingerprintPath string
    TargetFingerprintPath string
}
```

The source and target paths must point to files using `docs/fingerprint-artifact-format.md`.

## Result artifact

The runner writes one JSON result file per job:

```text
<artifact-dir>/<job-id>-result.json
```

Example:

```json
{
  "job_id": "job-1",
  "state": "SUCCEEDED",
  "consistency_score": 0.76,
  "metrics": {
    "FingerprintDistance": 0.24,
    "StableNeighborDistance": 0.25,
    "BoundaryCandidateDistance": 0.1,
    "BoundaryFlipRate": 0.2,
    "MatchedQueryCount": 10,
    "MissingSourceQueryCount": 0,
    "MissingTargetQueryCount": 0
  }
}
```

The current Go result artifact uses the Go metric field names for the nested `metrics` object. The Python engine protocol uses snake_case. A later reporting layer may normalize external report JSON independently from the internal local runner artifact.

## Validation behavior

The runner rejects:

- nil engine;
- empty `job_id`;
- empty source fingerprint path;
- empty target fingerprint path.

If the engine returns an error, the runner propagates the error and does not write a success result artifact.

## Current limitations

The runner does not yet:

- load `configs/*.yaml` directly;
- collect search results from Milvus or pgvector;
- generate fingerprint artifacts from database query results;
- expose a CLI command;
- render Markdown reports.

Those capabilities will be layered on top of this runner.