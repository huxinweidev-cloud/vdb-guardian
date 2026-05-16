# Phase 9 Full-Record Migration Execution Plan

> **For Hermes:** Use test-driven-development for each code task. This repository requires plan-before-code in `CLAUDE.md`; this plan is the approval artifact before large coding changes.

**Goal:** Extend the real Milvus→pgvector `vdbg migrate` execution path from id/vector-only records to mapping-driven full-record rows that can carry scalar fields, dynamic metadata JSON, and partition metadata.

**Architecture:** Build on the existing Phase 8 `RecordMappingPlan` artifact rather than adding more ad-hoc migrate flags. Keep connector SDK details behind the existing migration reader/writer adapters, extend the neutral `VectorMigrationRecord` model additively, and make `vdbg migrate --record-mapping` opt-in so current id/vector smoke behavior remains compatible.

**Tech Stack:** Go, Milvus Go SDK, pgx/pgvector, existing `internal/migration`, `cmd/vdbg`, JSON artifacts with `0600` permissions.

---

## Current State

Implemented before this phase:

- `inspect-milvus` captures source schema/index/partition metadata.
- `plan-pgvector-schema` creates target table/column/index plans, including `_milvus_dynamic` and `_milvus_partition` columns where applicable.
- `compare-schema-plans`, `apply-pgvector-schema`, `inspect-pgvector-schema`, and `compare-applied-schema` provide schema safety gates.
- `map-migration-records` emits a deterministic `RecordMappingPlan` artifact.
- `vdbg migrate` can run real Milvus reads and pgvector writes, but only writes `id` + vector.

Phase 9 changes only execution. Full-record data comparison, checkpoint/resume, retry, rollback, bulk import, and business-scale performance tests remain later phases.

## Scope

### In scope

- Add neutral record payload fields:
  - scalar values keyed by source field;
  - dynamic metadata map;
  - partition name.
- Add mapping-aware migration config and adapter method paths.
- Extend Milvus reader query output fields from id/vector-only to all mapped source fields.
- Extend pgvector writer SQL from fixed two-column upsert to mapping-driven column lists.
- Add `vdbg migrate --record-mapping <path>` as an opt-in full-record execution mode.
- Preserve id/vector-only behavior when `--record-mapping` is omitted.
- Update reports/docs/CHANGELOG with explicit current limitations.
- Verify with unit tests first, then clean local smoke if the stack remains available.

### Out of scope

- Cross-collection execution in one command. If the mapping artifact has multiple collections, Phase 9 should fail with a clear error.
- Unsupported Milvus scalar types beyond values the SDK exposes as JSON/pgx-compatible primitives in this phase.
- Full-record source/target equality reports.
- Resume/checkpoint/idempotency beyond the current upsert behavior.
- Dropping or recreating target tables.

## Files

Likely modified:

- `internal/migration/vector_migration.go`
- `internal/migration/vector_migration_test.go`
- `internal/migration/vector_migration_adapters.go`
- `internal/migration/vector_migration_adapters_test.go`
- `internal/migration/vector_migration_real_adapters.go`
- `internal/migration/vector_migration_real_adapters_test.go`
- `cmd/vdbg/migrate.go`
- `cmd/vdbg/migrate_test.go`
- `docs/migrate-cli.md`
- `docs/zh-CN/migrate-cli.md`
- `docs/map-migration-records-cli.md`
- `docs/zh-CN/map-migration-records-cli.md`
- `README.md`
- `README.zh-CN.md`
- `CHANGELOG.md`

Optional if needed:

- `testdata/migration/synthetic-full-record.json`
- `docs/local-migration-stack.md`
- `docs/zh-CN/local-migration-stack.md` if present later.

## Data Model Design

Extend `VectorMigrationRecord` additively:

```go
type VectorMigrationRecord struct {
    ID               string
    Vector           []float64
    Scalars          map[string]any
    DynamicMetadata  map[string]any
    Partition        string
}
```

Copy/validation must deep-copy maps and continue rejecting empty IDs, dimension mismatch, and non-finite vectors.

Add config:

```go
type VectorMigrationConfig struct {
    SourceCollection string
    TargetTable      string
    Dimension        int
    BatchSize        int
    RecordMapping    *CollectionRecordMapping
}
```

Behavior:

- Without `RecordMapping`: keep existing id/vector-only execution.
- With `RecordMapping`: resolve source collection, target table, id/vector/scalar/dynamic/partition columns from the mapping artifact.
- Validate the mapping status was pass before constructing the runner.
- Validate exactly one collection mapping in Phase 9.

## Task 1: Extend neutral migration record model

**Objective:** Allow copied records to carry scalar, dynamic metadata, and partition payloads without changing existing id/vector behavior.

**Files:**

- Modify: `internal/migration/vector_migration.go`
- Modify/Test: `internal/migration/vector_migration_test.go`

**RED tests:**

- `TestVectorMigrationRunnerCopiesFullRecordPayloads`
  - source returns one record with `Scalars`, `DynamicMetadata`, and `Partition`.
  - after `Migrate`, mutate source maps/vectors.
  - assert target received independent copies.
- `TestVectorMigrationRunnerRejectsNonFiniteScalarFloat`
  - if scalar values include `math.NaN()` or `math.Inf`, runner rejects before writing.

**Commands:**

```bash
go test ./internal/migration -run 'TestVectorMigrationRunnerCopiesFullRecordPayloads|TestVectorMigrationRunnerRejectsNonFiniteScalarFloat' -v
```

Expected RED: fails because fields/validation do not exist.

**GREEN implementation:**

- Add fields to `VectorMigrationRecord`.
- Extend `copyVectorMigrationRecords` with map deep-copy.
- Extend validation for scalar/dynamic non-finite numeric values only; preserve existing id/vector checks.

## Task 2: Add mapping-aware source/target adapter contracts

**Objective:** Let adapters use `CollectionRecordMapping` when available while preserving existing interfaces.

**Files:**

- Modify: `internal/migration/vector_migration_adapters.go`
- Modify/Test: `internal/migration/vector_migration_adapters_test.go`

**RED tests:**

- `TestMilvusVectorMigrationSourceUsesRecordMappingFields`
  - configure mapping with custom source collection, id, vector, one scalar, dynamic metadata, partition metadata.
  - assert fake reader receives a full-record request containing those fields.
- `TestPGVectorMigrationTargetUsesRecordMappingColumns`
  - configure mapping with custom target table/columns.
  - assert fake writer receives mapping-driven write request.

Expected RED: fails because adapter contracts accept only id/vector.

**GREEN implementation:**

- Introduce internal request structs, e.g. `MilvusMigrationReadRequest` and `PGVectorMigrationWriteRequest`.
- Keep old id/vector paths by populating minimal request when no mapping is set.
- Validate simple identifiers for all mapped source/target names.

## Task 3: Read full-record fields from Milvus SDK

**Objective:** Query all mapped fields and normalize scalar/dynamic/partition payloads into `VectorMigrationRecord`.

**Files:**

- Modify: `internal/migration/vector_migration_real_adapters.go`
- Modify/Test: `internal/migration/vector_migration_real_adapters_test.go`

**RED tests:**

- `TestMilvusSDKMigrationReaderReadsScalarPayloads`
  - fake SDK iterator exposes columns for string/int/float/bool scalar fields.
  - assert normalized `Scalars` map contains values keyed by source field.
- `TestMilvusSDKMigrationReaderReadsDynamicAndPartitionPayloads`
  - fake row has dynamic metadata JSON/map and partition value.
  - assert record fields are populated.

Expected RED: current query only outputs id/vector and iterator only reads id/vector.

**GREEN implementation:**

- Extend query request with output field list from mapping.
- Convert supported Milvus column values through `Get`/`GetAsString` where appropriate.
- Store unknown scalar primitives as returned Go values when JSON/pgx-compatible.
- If dynamic metadata is missing, use nil/empty map; if partition is missing, use empty string unless mapping requires it non-null.

## Task 4: Write mapping-driven full rows to pgvector

**Objective:** Generate safe dynamic upsert SQL for mapped columns and bind scalar/dynamic/partition values.

**Files:**

- Modify: `internal/migration/vector_migration_real_adapters.go`
- Modify/Test: `internal/migration/vector_migration_real_adapters_test.go`

**RED tests:**

- `TestPGVectorMigrationWriterWritesMappedFullRecords`
  - mapping includes id, vector, scalar `title`, dynamic `milvus_dynamic`, partition `milvus_partition`.
  - assert SQL inserts all mapped columns and args preserve values.
- `TestPGVectorMigrationWriterRejectsMissingRequiredScalar`
  - non-nullable scalar mapping missing from record should fail before Exec.
- `TestPGVectorMigrationWriterDoesNotLeakConnectionURLInErrors`
  - injected exec error should wrap record/table context but not include connection URL.

Expected RED: current writer only writes id/vector.

**GREEN implementation:**

- Add mapping-driven SQL builder with quoted identifiers.
- Use `$2::vector` only for vector arg; other args are regular placeholders.
- Encode `DynamicMetadata` as JSONB-compatible value, likely `[]byte`/string JSON with `::jsonb` cast.
- Include `ON CONFLICT (id) DO UPDATE` for all non-id target columns.

## Task 5: Wire `vdbg migrate --record-mapping`

**Objective:** Expose full-record execution through CLI while preserving old flags.

**Files:**

- Modify: `cmd/vdbg/migrate.go`
- Modify/Test: `cmd/vdbg/migrate_test.go`

**RED tests:**

- `TestParseMigrateOptionsWithRecordMapping`
  - parses `--record-mapping /tmp/mapping.json`.
- `TestRunMigrateLoadsRecordMappingAndConfiguresRunner`
  - writes mapping fixture and uses fake factory to assert migration config contains mapping-derived collection/table/dimension fields.
- `TestRunMigrateRejectsFailingRecordMapping`
  - mapping status fail causes error before runner executes.
- `TestRunMigrateRejectsMultiCollectionRecordMappingForPhase9`
  - multiple mappings cause clear error.

Expected RED: flag/options do not exist.

**GREEN implementation:**

- Add `RecordMappingPath` to options.
- Load and validate JSON before factory construction.
- Derive source collection, target table, id/vector config columns from the single mapping.
- Keep `--dimension` required for now unless safely derivable from `vector(n)` in mapping. Prefer derive if already available and tested; otherwise document requirement.

## Task 6: Reports and documentation

**Objective:** Make current capability and limitations explicit.

**Files:**

- Modify: `internal/migration/vector_migration_report.go` if report summary should include mapping mode.
- Modify/Test: corresponding report tests if changed.
- Modify docs/readmes/changelog listed above.

Docs must say:

- `--record-mapping` enables Phase 9 full-record execution.
- Mapping must have status `pass` and exactly one collection for now.
- Existing id/vector-only mode remains available without the flag.
- Full-record compare and checkpoint/resume are not yet implemented.
- Artifacts/reports remain `0600` and must not contain connection URLs.

## Task 7: Verification

Run targeted tests after each RED/GREEN task, then full gate:

```bash
make fmt && make lint && make test && git diff --check && make pre-commit
```

Run secret scan before commit:

```bash
git diff --cached | grep -Ei '(api[_-]?key|token|secret|password|BEGIN (RSA|OPENSSH|PRIVATE)|postgres(ql)?://[^[:space:]]+:[^[:space:]@]+@)' || true
```

If local Docker stack is healthy and disk allows, run clean smoke:

```text
seed-milvus -> inspect/plan/compare/apply/inspect/compare -> map-migration-records -> migrate --record-mapping --require-schema-match -> row count/vector/fingerprint checks
```

Full-record scalar/dynamic E2E should wait until a dedicated full-record fixture exists.

## Risks

- Milvus SDK column APIs may expose scalar/dynamic fields differently by type; keep conversions conservative and tested with fake adapters first.
- Dynamic metadata shape can vary; encode JSON defensively and block non-JSON-compatible values.
- SQL placeholder ordering is easy to break; tests should assert both SQL and args.
- Multi-collection mapping support is intentionally deferred to avoid accidental partial migrations.
- Existing id/vector smoke must remain stable.

## Commit Plan

1. `docs: add full-record migration execution plan`
2. `feat(migration): carry full-record payloads through runner`
3. `feat(migration): add mapping-aware migration adapters`
4. `feat(migration): write mapped full records to pgvector`
5. `feat(cli): wire record mapping into migrate`
6. `docs: document full-record migration execution`

Small commits are preferred if each task is independently green.
