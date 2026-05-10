# build-pgvector-artifact CLI

`vdbg build-pgvector-artifact` searches every query in a synthetic fixture against a real pgvector table and writes a Python-compatible fingerprint artifact.

This command is the target-side bridge from real database search results to the existing fingerprint comparison engine:

```text
synthetic fixture queries
    -> PGVectorConnector.Search
    -> fingerprints.BuildArtifact
    -> target-fingerprint.json
```

It does not start Docker and does not mutate the database.

## Example

```bash
go run ./cmd/vdbg build-pgvector-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --output /tmp/vdb-guardian-target-fingerprint.json \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

## Options

| Option | Required | Default | Description |
| --- | --- | --- | --- |
| `--fixture` | yes | none | Synthetic fixture JSON containing query vectors. |
| `--connection-url` | yes | none | PostgreSQL connection URL for the pgvector database. |
| `--output` | yes | none | Path for the generated fingerprint artifact JSON. |
| `--table` | no | `items` | pgvector table to search. |
| `--top-k` | no | `3` | Visible topK result count written to `top_k_ids`. |
| `--expand-k` | no | `5` | Search limit. Must be at least `top-k + boundary-k`. |
| `--stable-k` | no | `2` | Leading hit count written to `stable_neighbors`. Must be `<= top-k`. |
| `--boundary-k` | no | `1` | Rank-window width around the topK cutoff. |
| `--metric` | no | `cosine` | pgvector metric: `cosine` or `l2`. |

## Output

The command writes an artifact shaped like:

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

It also prints a summary:

```text
pgvector fingerprint artifact written
fixture: testdata/migration/synthetic-small.json
output: /tmp/vdb-guardian-target-fingerprint.json
table: items
queries: 10
top_k: 3
expand_k: 5
stable_k: 2
boundary_k: 1
```

## Local migration stack smoke check

After explicit approval to run Docker side effects, start or reuse the local pgvector service and seed it first:

```bash
docker compose -f deployments/docker-compose.migration.yml up -d postgres-pgvector
scripts/check-migration-stack.sh postgres
go run ./cmd/vdbg seed-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --id-column id \
  --vector-column embedding
```

Then generate the target fingerprint artifact:

```bash
go run ./cmd/vdbg build-pgvector-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --output /tmp/vdb-guardian-target-fingerprint.json \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

Verify the artifact shape:

```bash
python -m json.tool /tmp/vdb-guardian-target-fingerprint.json >/dev/null
```

For the committed small fixture, the expected artifact contains `10` query fingerprints.

## Safety

- Connection URLs are runtime-only and must not be committed.
- The command performs reads plus local artifact writes only.
- Docker is never started implicitly.
- The generated artifact is target-side only; it does not prove Milvus source readiness or end-to-end migration correctness.

## Current limitations

Implemented:

- Real pgx-backed pgvector connector usage.
- Multi-query synthetic fixture search.
- Conversion from connector hits to fingerprint builder input.
- Python-compatible target fingerprint artifact writing.
- Unit tests through an injected connector factory.

Not yet implemented:

- Milvus source-side real fingerprint artifact generation.
- Direct compare invocation against a source artifact.
- Automated Docker integration test.
- End-to-end migrate-and-verify command.
