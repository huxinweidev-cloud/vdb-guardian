# migrate CLI

`vdbg migrate` runs the first real Milvus-to-pgvector record transfer path.

It reads normalized records from a Milvus source collection through the Milvus SDK query path and writes them into a pgvector target table through the pgx-backed writer.

The command does not start Docker, create services, build fingerprint artifacts, or compare retrieval behavior. It assumes the local migration stack or equivalent disposable test databases are already running.

## Command

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --source-collection items \
  --milvus-id-field id \
  --milvus-vector-field embedding \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --pgvector-id-column id \
  --pgvector-vector-column embedding \
  --dimension 8 \
  --batch-size 100
```

## Output

A successful run prints a compact summary:

```text
migration completed
source_collection: items
target_table: items
dimension: 8
records_read: 100
records_written: 100
```

## Required flags

- `--milvus-address`: Milvus gRPC endpoint, for example `localhost:19530`.
- `--pgvector-connection-url`: PostgreSQL connection URL. Redact this in logs and docs.
- `--dimension`: expected vector dimension. The runner validates every migrated vector against this value.

## Defaults

- `--source-collection`: `items`
- `--target-table`: `items`
- `--milvus-id-field`: `id`
- `--milvus-vector-field`: `embedding`
- `--pgvector-id-column`: `id`
- `--pgvector-vector-column`: `embedding`
- `--batch-size`: `100`

## Scope

`vdbg inspect-milvus` complements this command by generating a read-only migration planning JSON document from Milvus metadata before record transfer. `vdbg plan-pgvector-schema` can then turn that inspection plan into a dry-run pgvector schema/DDL plan. See `docs/inspect-milvus-cli.md` and `docs/plan-pgvector-schema-cli.md`.

Implemented:

- Real Milvus SDK-backed source read.
- Real pgx-backed pgvector target upsert.
- Dimension validation.
- CLI flag parsing and injected-runner unit tests.
- Summary output for records read and written.

Not implemented yet:

- Source/target fingerprint artifact generation inside this command.
- Comparison result artifact generation inside this command.
- Metadata fields.
- Milvus partitions.
- Incremental checkpoints.
- Production bulk import.

## Safety notes

Run this only against local migration MVP services or disposable test databases until production migration semantics are explicitly added.

The pgvector writer uses upsert semantics:

```sql
INSERT ... ON CONFLICT (id) DO UPDATE
```

It does not drop the target table. If the target table contains old records not present in the Milvus source, this first MVP command does not delete them.

## Test command

```bash
go test ./cmd/vdbg -run 'TestParseMigrate|TestRunMigrate' -v
```

Full gate before commit:

```bash
make fmt
make lint
make test
git diff --check
```
