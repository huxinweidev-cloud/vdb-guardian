# Live Full-Record Artifact Builders Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add live Milvus and pgvector full-record artifact builders that read records according to a passing `map-migration-records` artifact and emit local `FullRecordArtifact` JSON suitable for `vdbg compare-full-records`.

**Why now:** Phase 10 added artifact-only full-record equality comparison. This phase closes the loop by generating source and target full-record artifacts from live services after `vdbg migrate --record-mapping`.

## Non-goals

- No data mutation.
- No DDL/DML writes.
- No checkpoint/resume.
- No cross-collection artifact in one command; first phase requires exactly one mapping.
- No raw vector payload in artifacts; only deterministic hash and dimension.
- No real credentials in docs, tests, stdout, or committed artifacts.

## Commands

Add:

```bash
vdbg build-milvus-record-artifact \
  --milvus-address localhost:19530 \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --output /tmp/source-full-records.json
```

Add:

```bash
vdbg build-pgvector-record-artifact \
  --pgvector-connection-url '[REDACTED]' \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --output /tmp/target-full-records.json
```

Both commands must:

- require `--record-mapping`;
- require exactly one mapping with `status: pass`;
- reject missing primary key/vector mappings;
- write output with final permissions `0600`, including overwrite cases;
- never print connection strings;
- produce deterministic ordering by record ID;
- output `FullRecordArtifact` with `schema_version: v1`.

## Files

Planned files:

```text
internal/migration/full_record_artifact_builder.go
internal/migration/full_record_artifact_builder_test.go
cmd/vdbg/build_milvus_record_artifact.go
cmd/vdbg/build_milvus_record_artifact_test.go
cmd/vdbg/build_pgvector_record_artifact.go
cmd/vdbg/build_pgvector_record_artifact_test.go
cmd/vdbg/main.go
docs/full-record-artifact-builders.md
docs/zh-CN/full-record-artifact-builders.md
README.md
README.zh-CN.md
CHANGELOG.md
docs/local-migration-stack.md
```

May patch existing:

```text
internal/migration/vector_migration_real_adapters.go
internal/migration/vector_migration_adapters.go
```

if helper visibility or shared mapping request construction is needed.

## Internal model

Reuse existing:

```go
type FullRecordArtifact struct {
    SchemaVersion     string
    System            string
    Collection        string
    RecordMappingPath string
    Records           []FullRecordArtifactRecord
}
```

Add builder helpers:

```go
type FullRecordArtifactBuildOptions struct {
    System            string
    Collection        string
    RecordMappingPath string
}

func BuildFullRecordArtifact(records []VectorMigrationRecord, options FullRecordArtifactBuildOptions) (FullRecordArtifact, error)
```

Rules:

- `System` required;
- `Collection` required;
- record ID required and unique;
- vector required;
- vector hash deterministic across Milvus float32-read and pgvector float64-read paths;
- records sorted by ID;
- scalar/dynamic maps copied and normalized enough for JSON stable diagnostics.

Vector hash proposal:

- hash canonical vector literal/JSON using fixed float formatting;
- use `sha256:<hex>`;
- include `vector_dimension` separately;
- reject non-finite vector values.

## Milvus builder

Use existing real migration reader path:

```go
newMilvusSDKMigrationReader(address)
ReadMilvusMigrationRecordsWithMapping(ctx, request)
```

Construct `MilvusMigrationReadRequest` from `CollectionRecordMapping`.

Artifact:

```text
system: milvus
collection: mapping.SourceCollection
record_mapping_path: provided path
```

## pgvector builder

Need a read-only target reader matching the mapping:

```go
type pgvectorFullRecordReader interface {
    ReadPGVectorFullRecords(ctx context.Context, request PGVectorFullRecordReadRequest) ([]VectorMigrationRecord, error)
}
```

Request fields should mirror target mapping:

```go
type PGVectorFullRecordReadRequest struct {
    Table           string
    IDColumn        string
    VectorColumn    string
    ScalarColumns   []PGVectorMigrationScalarColumn
    DynamicColumn   string
    PartitionColumn string
}
```

SQL requirements:

- validate/quote identifiers using existing identifier helpers;
- dynamic values must be read via query result scanning, not interpolated;
- SQL values must never contain connection string;
- stable query ordering by quoted ID column:

```sql
SELECT id, embedding, title, price, metadata, partition
FROM items
ORDER BY id
```

Vector scan must support at least pgvector textual representation (`[0.1,0.2]`) and common numeric slice scan types. Tests should use fake rows first; real-service smoke can validate actual pgvector output later.

## CLI design

Common helper should load and validate record mapping plan:

```go
loadSinglePassingRecordMapping(path string) (migration.CollectionRecordMapping, error)
```

Avoid duplicating load logic across migrate/builders if practical.

Command output must be safe:

```text
Milvus full-record artifact written
output: /tmp/source-full-records.json
collection: items
records: 100
```

pgvector command must not print `--pgvector-connection-url`.

## TDD plan

### RED 1: artifact builder

Tests:

- converts `VectorMigrationRecord` to sorted `FullRecordArtifact`;
- produces stable vector hash;
- rejects duplicate IDs;
- rejects non-finite vectors;
- preserves scalar/dynamic/partition fields.

### GREEN 1

Implement minimal `BuildFullRecordArtifact`.

### RED 2: Milvus CLI builder

Tests with fake reader/factory:

- parses required flags;
- loads passing mapping;
- rejects failing/multi mapping;
- calls reader with expected mapped source fields;
- writes `0600` artifact;
- overwrites `0644` output as `0600`;
- stdout excludes connection string.

### GREEN 2

Implement `build-milvus-record-artifact`.

### RED 3: pgvector reader/CLI builder

Tests with fake DB/rows:

- generated SELECT uses quoted identifiers and `ORDER BY` id;
- args empty for read-only full table scan;
- maps target columns back to source scalar keys so source and target artifacts compare correctly;
- reads JSONB dynamic metadata;
- writes `0600` artifact;
- stdout excludes connection string.

### GREEN 3

Implement pgvector reader and CLI.

### RED 4: compare integration

Test flow:

- build source artifact from fake Milvus records;
- build target artifact from fake pgvector records;
- compare with `CompareFullRecordArtifacts` returns pass.

### GREEN 4

Polish and refactor shared helpers.

## Verification

Targeted:

```bash
go test ./internal/migration -run 'TestBuildFullRecordArtifact|TestPGVectorFullRecordReader' -v
go test ./cmd/vdbg -run 'TestRunBuildMilvusRecordArtifact|TestRunBuildPGVectorRecordArtifact|TestParseBuild.*RecordArtifact' -v
```

Sample artifact-only smoke:

```bash
go run ./cmd/vdbg compare-full-records \
  --source testdata/migration/source-full-record-artifact.json \
  --target testdata/migration/target-full-record-artifact.json \
  --output /tmp/vdb-guardian-full-record-compare.json
stat -c '%a %n' /tmp/vdb-guardian-full-record-compare.json
```

Full gate:

```bash
make fmt && make lint && make test && git diff --check && make pre-commit
```

Secret scan:

```bash
git diff --cached | grep -Ei '(api[_-]?key|token|secret|password|BEGIN (RSA|OPENSSH|PRIVATE)|postgres(ql)?://[^[:space:]]+:[^[:space:]@]+@)' || true
git ls-files --others --exclude-standard -z | xargs -0 -r grep -nEi '(api_key|secret|password|token|credential|client_secret|private key|BEGIN .*PRIVATE|Authorization:|Bearer |postgres://|postgresql://|redis://|mysql://|sk-|ghp_|github_pat_|AKIA|AIza|ya29\.)' || true
```

## Acceptance criteria

- `build-milvus-record-artifact` implemented and tested.
- `build-pgvector-record-artifact` implemented and tested.
- Both write final `0600` outputs.
- Both reject invalid mapping artifacts before connecting where possible.
- CLI stdout is credential-safe.
- Full-record artifacts from fake source/target compare as `pass`.
- Docs updated in English and Chinese.
- Full quality gate passes.
- Independent review passes before push.

## Risks

- pgvector scan representation may differ by pgx/pgvector codecs. Start with robust parser and fake rows; follow with real-service smoke when local stack contains full-record fixture.
- Existing `map-migration-records` source scalar keys may differ from target columns; target artifact builder must store scalar keys by source field for compare compatibility.
- Dynamic metadata `nil` vs `{}` can cause false mismatch; preserve actual semantics and document behavior.
- This phase still does not generate richer full-record fixture in live services unless time allows; if not, call out as follow-up.
