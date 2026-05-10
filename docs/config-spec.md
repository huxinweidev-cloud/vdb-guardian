# Configuration Specification

`vdb-guardian` uses YAML configuration files to describe verification jobs. The Go control plane loads these files into typed structs and validates them before any connector opens a database connection.

## Supported examples

Example files live in `configs/`:

- `configs/local.yaml`
- `configs/milvus-to-pgvector.example.yaml`

Example files must not contain real credentials. Sensitive fields must use `[REDACTED]`.

## Root fields

```yaml
job:
  name: milvus-to-pgvector-demo

runtime:
  artifact_store:
    type: local
    path: ./artifacts

source:
  type: milvus
  address: localhost:19530
  collection: patent_demo

target:
  type: pgvector
  dsn: postgresql://postgres:[REDACTED]@localhost:5433/postgres
  table: items

query:
  top_k: 10
  expand_k: 20
  sample_size: 100
  filters:
    enabled: true

fingerprint:
  boundary:
    rank_before_k: 2
    delta: 0.03
  weights:
    stable_diff: 0.25
    boundary_flip: 0.40
    curve_diff: 0.20
    filter_diff: 0.15

report:
  formats:
    - json
    - markdown
```

## Validation rules

### Job

- `job.name` must not be empty.

### Runtime

- `runtime.artifact_store.type` may be empty, `local`, or `memory`.
- `runtime.artifact_store.path` is used when the store type is `local`.

### Source and target

- `source.type` must not be empty.
- `target.type` must not be empty.

Connector-specific fields are intentionally permissive at the config layer. Concrete connector implementations will add deeper validation for Milvus, pgvector, and future backends.

### Query

- `query.top_k` must be greater than zero.
- `query.expand_k` must be greater than or equal to `query.top_k`.
- `query.sample_size` must be greater than zero.

### Fingerprint

- `fingerprint.boundary.rank_before_k` must not be negative.
- `fingerprint.boundary.delta` must not be negative.
- `fingerprint.weights` must not be empty.
- Each fingerprint weight must be greater than or equal to zero.
- The total fingerprint weight must be greater than zero.

### Report

- Empty `report.formats` is allowed so the future runner can apply a default.
- Explicit formats currently support `json` and `markdown`.

## Security requirements

Do not commit real tokens, passwords, private keys, cloud credentials, or production database connection strings. Use `[REDACTED]` in examples and documentation.

## Go API

The typed configuration loader lives in `internal/config`.

```go
cfg, err := config.LoadFile("configs/milvus-to-pgvector.example.yaml")
```

The package exposes:

- `LoadFile(path string) (Config, error)`
- `LoadReader(reader io.Reader) (Config, error)`
- `Config.Validate() error`

`LoadFile` and `LoadReader` validate the configuration before returning it.