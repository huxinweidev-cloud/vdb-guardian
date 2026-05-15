# Local Offline Verification Pipeline

The local offline verification pipeline is the first database-free end-to-end path in vdb-guardian. It connects deterministic connectors, fingerprint artifact building, and the local verification runner without starting Docker or contacting a real vector database.

## Workflow

```text
source connector
        |
        v
target connector
        |
        v
normalized SearchResponse values
        |
        v
fingerprints.BuildArtifact
        |
        v
<job-id>-source-fingerprint.json
<job-id>-target-fingerprint.json
        |
        v
jobs.VerificationRunner
        |
        v
<job-id>-result.json
```

## Package

The implementation lives in:

```text
internal/pipeline
```

The pipeline is intentionally internal because it is orchestration glue for the Go control plane, not a public SDK boundary.

## Core API

```go
pipeline := pipeline.NewOfflinePipeline(
    sourceConnector,
    targetConnector,
    verificationRunner,
    artifactDir,
    fingerprints.BuildOptions{TopK: 3, StableK: 2, BoundaryK: 1},
)

result, err := pipeline.Run(ctx, pipeline.OfflineRequest{
    JobID:    "job-1",
    QueryIDs: []string{"q-1", "q-2"},
    TopK:     3,
    ExpandK:  4,
})
```

## Generated artifacts

The pipeline writes:

```text
<artifact-dir>/<job-id>-source-fingerprint.json
<artifact-dir>/<job-id>-target-fingerprint.json
<artifact-dir>/<job-id>-result.json
```

The source and target fingerprint artifacts use the schema documented in:

```text
docs/fingerprint-artifact-format.md
```

The result artifact is written by the local verification runner documented in:

```text
docs/local-verification-runner.md
```

## Validation

`Run` rejects:

- nil source or target connectors;
- nil verification runner engine;
- empty artifact directory;
- empty job ID;
- empty query ID list;
- non-positive topK;
- expandK smaller than topK;
- connector search errors;
- artifact build errors;
- artifact write errors;
- verification runner errors.

## Current connector convention

When used with `MemoryConnector`, each query ID is passed through `SearchRequest.Collection`. This lets the pipeline exercise the same connector interface without real vector search infrastructure. Real Milvus and pgvector connectors can later map query definitions to embeddings and database collections.

## Limitations

The pipeline is not yet exposed through CLI or HTTP API. It is currently a tested internal orchestration layer for offline functional verification. The next step is to connect it to typed job configuration and then expose a CLI command once the end-to-end behavior is stable.
