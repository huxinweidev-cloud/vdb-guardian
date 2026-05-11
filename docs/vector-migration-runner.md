# Minimal vector migration runner

The internal vector migration runner provides the first database-neutral boundary for moving records from a Milvus source reader into a pgvector target writer.

It is intentionally not a CLI yet. The runner is the tested core that the next `vdbg migrate` command will wrap.

## Scope

Implemented:

- Fixed source collection and target table names.
- Fixed vector dimension validation.
- Source reader boundary returning normalized records.
- Target writer boundary accepting normalized records.
- Defensive vector copying before writes.
- Context cancellation checks.
- Wrapped read/write errors for diagnostics.
- Unit tests for success, defaults, invalid config, invalid records, reader errors, writer errors, and context cancellation.

Not implemented yet:

- Real Milvus scan/query adapter.
- Real pgvector migration writer adapter.
- Public `vdbg migrate` CLI.
- One-shot `migrate-and-verify` orchestration.
- Metadata columns.
- Milvus partitions.
- Incremental checkpoints.
- Production bulk import.

## Go boundary

The runner lives in:

```text
internal/migration/vector_migration.go
```

Core types:

```go
type VectorMigrationConfig struct {
    SourceCollection string
    TargetTable      string
    Dimension        int
    BatchSize        int
}

type VectorMigrationRecord struct {
    ID     string
    Vector []float64
}

type VectorMigrationResult struct {
    SourceCollection string
    TargetTable      string
    Dimension        int
    RecordsRead      int
    RecordsWritten   int
}
```

Internal source and target boundaries:

```go
type vectorMigrationSource interface {
    ReadRecords(ctx context.Context, collection string) ([]VectorMigrationRecord, error)
}

type vectorMigrationTarget interface {
    WriteRecords(ctx context.Context, table string, records []VectorMigrationRecord) error
}
```

These interfaces are package-private on purpose. The next increment can add real SDK-backed adapters in the same package without exposing unstable adapter details to callers.

## Defaults

When omitted:

- `SourceCollection`: `items`
- `TargetTable`: `items`
- `BatchSize`: `100`

`Dimension` is required and must be in `1..2000`, matching the current pgvector-compatible synthetic fixture limit.

## Validation

The runner rejects:

- Missing source reader.
- Missing target writer.
- Invalid source collection or target table identifiers.
- Non-positive or too-large dimensions.
- Non-positive batch size.
- Empty record IDs.
- Vector dimension mismatch.
- NaN or infinite vector values.

## Test command

```bash
go test ./internal/migration -run 'TestVectorMigration|TestNewVectorMigration' -v
```

Full gate before commit:

```bash
make fmt
make lint
make test
git diff --check
```

## Next step

Add real adapters behind this runner:

```text
Milvus collection reader -> VectorMigrationRecord[] -> pgvector writer
```

Then expose the flow via:

```text
vdbg migrate
```

After that, compose migration, artifact generation, and comparison into:

```text
vdbg migrate-and-verify
```
