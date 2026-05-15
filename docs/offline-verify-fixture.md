# Offline Verify Fixture Command

The `vdbg offline-verify` command runs a database-free local verification workflow from a JSON fixture. It is intended for deterministic functional checks before Milvus and pgvector connectors are implemented.

## Command

```bash
go run ./cmd/vdbg offline-verify \
  --fixture testdata/offline/basic.json \
  --artifact-dir /tmp/vdb-guardian-offline
```

The command loads source and target ranked hits from the fixture, builds memory connectors, runs the internal offline pipeline, invokes the Python fingerprint engine, and writes fingerprint/result artifacts.

## Fixture shape

```json
{
  "job_id": "offline-basic",
  "top_k": 3,
  "expand_k": 4,
  "stable_k": 2,
  "boundary_k": 1,
  "queries": [
    {
      "query_id": "q-1",
      "source_hits": [
        {"id": "a", "rank": 1, "score": 0.99}
      ],
      "target_hits": [
        {"id": "a", "rank": 1, "score": 0.99}
      ]
    }
  ]
}
```

Each query must contain at least `expand_k` source and target hits.

## Generated artifacts

The command writes:

```text
<artifact-dir>/<job-id>-source-fingerprint.json
<artifact-dir>/<job-id>-target-fingerprint.json
<artifact-dir>/<job-id>-result.json
```

The result path and consistency score are printed to stdout.

## Python engine discovery

The command prefers:

```text
python/.venv/bin/python
```

If that path does not exist, it falls back to:

```text
python3
python
```

The Python subprocess working directory is `python/`.

## Limitations

This command does not connect to Milvus or pgvector. It does not perform real vector search. It only verifies the local fixture-backed workflow: memory connector input, fingerprint artifact generation, Python artifact comparison, and result artifact writing.
