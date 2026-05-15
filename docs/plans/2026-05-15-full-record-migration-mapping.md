# Full Record Migration Mapping Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Define and implement a deterministic record-mapping layer that converts a pgvector schema plan into an executable Milvus-record to pgvector-row mapping plan before wiring full payload migration.

**Architecture:** Keep this phase non-mutating and unit-test only. The new mapping code lives in `internal/migration` and consumes the existing credential-free `internal/schema.PGVectorSchemaPlan` artifact. It produces a stable mapping/report model that later phases can use to migrate scalar, vector, dynamic-field, and partition payloads.

**Tech Stack:** Go, existing `internal/schema` and `internal/inspection` models, strict TDD, JSON-safe report structs, English/Chinese documentation sync.

---

## Current State

Implemented migration chain:

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
  -> inspect-pgvector-schema
  -> compare-applied-schema
  -> migrate --require-schema-match --output
```

The remaining gap is that `vdbg migrate` still primarily accepts manual flags (`--source-collection`, `--target-table`, `--dimension`, id/vector field flags) and moves the minimal normalized record shape. The schema plan already knows target table/column names, dynamic metadata columns, partition metadata columns, nullability, target types, support levels, and warnings. Phase 8 bridges those two worlds with a deterministic record mapping artifact.

## Scope

In scope:

- Build record mapping from existing `schema.PGVectorSchemaPlan`.
- Support primary key, dense vector, scalar, dynamic-field metadata, and partition metadata column classification.
- Detect blocking mapping issues before migration execution.
- Produce deterministic summary counts and issue lists.
- Add CLI-accessible validation/report generation for mapping artifacts.
- Add docs and examples in English and Chinese.

Out of scope for this phase:

- Real Milvus row payload extraction for arbitrary scalar/dynamic fields.
- Real pgvector multi-column row writer changes.
- Checkpoint/resume.
- Retry/rollback/cleanup policy.
- Full-record data compare.
- Production bulk import.

---

## Proposed User-Facing Flow

```bash
go run ./cmd/vdbg map-migration-records \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --output /tmp/vdb-guardian-record-mapping.json
```

Later, `vdbg migrate` can consume the same plan:

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --require-schema-match \
  --output /tmp/vdb-guardian-migration-report.json
```

Phase 8 may stop at generating/validating the mapping; Phase 9 will wire the mapping into real migration execution.

---

## Data Model

Create: `internal/migration/record_mapping.go`

Suggested constants:

```go
const RecordMappingPlanVersion = "v1"

const (
    RecordMappingStatusPass = "pass"
    RecordMappingStatusFail = "fail"
)

const (
    RecordMappingKindPrimaryKey = "primary_key"
    RecordMappingKindVector = "vector"
    RecordMappingKindScalar = "scalar"
    RecordMappingKindDynamicMetadata = "dynamic_metadata"
    RecordMappingKindPartitionMetadata = "partition_metadata"
)
```

Suggested structs:

```go
type RecordMappingPlan struct {
    SchemaVersion string                 `json:"schema_version"`
    SchemaPlan    string                 `json:"schema_plan,omitempty"`
    Status        string                 `json:"status"`
    Mappings      []CollectionRecordMapping `json:"mappings"`
    Issues        []RecordMappingIssue   `json:"issues,omitempty"`
    Summary       RecordMappingSummary   `json:"summary"`
}

type CollectionRecordMapping struct {
    SourceCollection string               `json:"source_collection"`
    TargetSchema     string               `json:"target_schema"`
    TargetTable      string               `json:"target_table"`
    PrimaryKey       *RecordFieldMapping  `json:"primary_key,omitempty"`
    Vector           *RecordFieldMapping  `json:"vector,omitempty"`
    Scalars          []RecordFieldMapping `json:"scalars,omitempty"`
    DynamicMetadata  *RecordFieldMapping  `json:"dynamic_metadata,omitempty"`
    PartitionMetadata *RecordFieldMapping `json:"partition_metadata,omitempty"`
    Issues           []RecordMappingIssue `json:"issues,omitempty"`
}

type RecordFieldMapping struct {
    Kind         string `json:"kind"`
    SourceField  string `json:"source_field"`
    TargetColumn string `json:"target_column"`
    TargetType   string `json:"target_type"`
    Nullable     bool   `json:"nullable"`
    SupportLevel string `json:"support_level"`
    Warning      string `json:"warning,omitempty"`
}

type RecordMappingIssue struct {
    Severity         string `json:"severity"`
    SourceCollection string `json:"source_collection,omitempty"`
    SourceField      string `json:"source_field,omitempty"`
    TargetColumn     string `json:"target_column,omitempty"`
    Message          string `json:"message"`
}

type RecordMappingSummary struct {
    CollectionCount            int `json:"collection_count"`
    ScalarMappingCount         int `json:"scalar_mapping_count"`
    DynamicMetadataMappingCount int `json:"dynamic_metadata_mapping_count"`
    PartitionMetadataMappingCount int `json:"partition_metadata_mapping_count"`
    IssueCount                 int `json:"issue_count"`
    BlockingIssueCount         int `json:"blocking_issue_count"`
}
```

Suggested API:

```go
type RecordMappingOptions struct {
    SchemaPlanPath string
}

func BuildRecordMappingPlan(plan schema.PGVectorSchemaPlan, options RecordMappingOptions) (RecordMappingPlan, error)
```

---

## Mapping Rules

1. Reject unsupported schema plan version.
2. Each table must have exactly one primary key column.
3. Each table must have exactly one dense vector column (`target_type` starts with `vector`).
4. Columns with `SourceField == "_milvus_dynamic"` map to `dynamic_metadata`.
5. Columns with `SourceField == "_milvus_partition"` map to `partition_metadata`.
6. Other non-primary-key/non-vector columns map to scalar mappings.
7. `support_level == unsupported` creates a blocking issue.
8. Missing primary key creates a blocking issue.
9. Missing vector creates a blocking issue.
10. Multiple primary keys or vectors create blocking issues in this first phase.
11. Degraded columns are allowed but preserved with warning text.
12. Mapping output order follows schema plan table/column order for deterministic artifacts.

---

## Task 1: Internal RED tests for successful mapping

**Objective:** Prove the desired internal API maps primary key, vector, scalar, dynamic metadata, and partition metadata columns from a schema plan.

**Files:**

- Create/modify: `internal/migration/record_mapping_test.go`
- Later create: `internal/migration/record_mapping.go`

**Step 1: Write failing test**

Add `TestBuildRecordMappingPlanMapsSchemaPlanColumns`.

Assertions:

- `SchemaVersion == "v1"`.
- `Status == "pass"`.
- one collection mapping exists.
- primary key source `id` maps to target `id`.
- vector source `embedding` maps to target `embedding`, type `vector(8)`.
- scalar source `title` maps to target `title`.
- dynamic metadata source `_milvus_dynamic` maps to JSONB metadata kind.
- partition metadata source `_milvus_partition` maps to text metadata kind.
- summary counts are deterministic.

**Step 2: Verify RED**

```bash
go test ./internal/migration -run 'TestBuildRecordMappingPlanMapsSchemaPlanColumns' -v
```

Expected: FAIL because `BuildRecordMappingPlan` and record mapping structs do not exist.

**Step 3: Implement minimal code**

Create `internal/migration/record_mapping.go` with structs/constants and mapping logic.

**Step 4: Verify GREEN**

```bash
go test ./internal/migration -run 'TestBuildRecordMappingPlanMapsSchemaPlanColumns' -v
```

Expected: PASS.

---

## Task 2: Internal RED tests for blocking issues

**Objective:** Ensure invalid record mappings fail before real migration execution.

**Files:**

- Modify: `internal/migration/record_mapping_test.go`
- Modify: `internal/migration/record_mapping.go`

**Step 1: Write failing tests**

Add tests:

- `TestBuildRecordMappingPlanBlocksMissingPrimaryKey`
- `TestBuildRecordMappingPlanBlocksMissingVector`
- `TestBuildRecordMappingPlanBlocksUnsupportedColumn`

Assertions:

- `Status == "fail"`.
- `Summary.BlockingIssueCount > 0`.
- issues include collection/field context.
- unsupported column issue does not silently drop the field.

**Step 2: Verify RED**

```bash
go test ./internal/migration -run 'TestBuildRecordMappingPlanBlocks' -v
```

Expected: FAIL until issue logic exists.

**Step 3: Implement minimal issue detection**

Add issue construction helpers and status calculation.

**Step 4: Verify GREEN**

```bash
go test ./internal/migration -run 'TestBuildRecordMappingPlan' -v
```

Expected: PASS.

---

## Task 3: CLI RED tests for mapping artifact output

**Objective:** Add a local-artifact CLI that lets users validate mapping independently before wiring it into real migration execution.

**Files:**

- Create: `cmd/vdbg/map_migration_records_test.go`
- Later create: `cmd/vdbg/map_migration_records.go`
- Modify later: `cmd/vdbg/main.go`

**Step 1: Write failing CLI tests**

Add tests for:

- required `--schema-plan`.
- writes `--output` JSON with `0600` permissions.
- stdout JSON when `--output` omitted.
- blocking mapping returns non-zero but still writes report.

Use existing `writeJSONFixture` helper if available in `cmd/vdbg` tests.

**Step 2: Verify RED**

```bash
go test ./cmd/vdbg -run 'TestRunMapMigrationRecords' -v
```

Expected: FAIL because CLI does not exist.

**Step 3: Implement CLI**

Create `cmd/vdbg/map_migration_records.go`:

- parse `--schema-plan`, `--output`;
- load `schema.PGVectorSchemaPlan`;
- call `migration.BuildRecordMappingPlan`;
- write JSON to stdout or `--output`;
- use `0600` for output file;
- return error when report status is fail after writing report.

Modify `cmd/vdbg/main.go` to register:

```text
map-migration-records
```

**Step 4: Verify GREEN**

```bash
go test ./cmd/vdbg -run 'TestRunMapMigrationRecords' -v
```

Expected: PASS.

---

## Task 4: Migration report mapping metadata

**Objective:** Extend the existing migration result report model with optional mapping summary metadata, without changing current migration behavior.

**Files:**

- Modify: `internal/migration/vector_migration_report.go`
- Modify: `internal/migration/vector_migration_test.go`
- Modify if needed: `cmd/vdbg/migrate.go`

**Step 1: Write failing test**

Add/extend test proving `BuildVectorMigrationReport` can include optional mapping summary when provided in options.

Expected JSON fields:

```json
"mapping": {
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "status": "pass",
  "scalar_mapping_count": 1,
  "dynamic_metadata_mapping_count": 1,
  "partition_metadata_mapping_count": 1,
  "blocking_issue_count": 0
}
```

**Step 2: Verify RED**

```bash
go test ./internal/migration -run 'TestBuildVectorMigrationReport' -v
```

Expected: FAIL because report has no mapping block.

**Step 3: Implement additive optional field**

Add optional mapping report struct and option. Keep old output valid when absent.

**Step 4: Verify GREEN**

```bash
go test ./internal/migration -run 'TestBuildVectorMigrationReport' -v
```

Expected: PASS.

---

## Task 5: Documentation sync

**Objective:** Document the new mapping validation phase and keep English/Chinese docs aligned.

**Files:**

- Create: `docs/map-migration-records-cli.md`
- Create: `docs/zh-CN/map-migration-records-cli.md`
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/migrate-cli.md`
- Modify: `docs/zh-CN/migrate-cli.md`

Docs must state:

- command is local-artifact only;
- does not connect to Milvus/PostgreSQL;
- does not migrate rows;
- validates record mapping before full-record migration;
- output file uses `0600`;
- blocking issues return non-zero after writing report.

---

## Task 6: Full verification and commit

**Objective:** Prove the phase is complete and safe to merge.

Run:

```bash
make fmt && make lint && make test && git diff --check && make pre-commit
```

Then run a secret scan on diff/staged diff:

```bash
git diff | grep -Ei '(api[_-]?key|token|secret|password|BEGIN (RSA|OPENSSH|PRIVATE)|postgres(ql)?://[^[:space:]]+:[^[:space:]@]+@)' || true
```

Commit:

```bash
git add internal/migration/record_mapping.go internal/migration/record_mapping_test.go \
  cmd/vdbg/map_migration_records.go cmd/vdbg/map_migration_records_test.go cmd/vdbg/main.go \
  internal/migration/vector_migration_report.go internal/migration/vector_migration_test.go \
  docs/map-migration-records-cli.md docs/zh-CN/map-migration-records-cli.md \
  README.md README.zh-CN.md CHANGELOG.md docs/migrate-cli.md docs/zh-CN/migrate-cli.md

git commit -m "feat(migration): add full record mapping plan CLI"
git push origin feat/enterprise-scaffold
```

---

## Risks

- Schema plan currently lacks source data type on `PGVectorColumnPlan`; mapping classification should therefore rely on `TargetType`, `PrimaryKey`, and metadata sentinel source fields in this phase.
- Multiple-vector collections may exist; first phase treats multiple dense vector columns as blocking to avoid ambiguous migration behavior.
- Dynamic fields and partitions are mapping-only here; real payload migration remains Phase 9.
- The mapping CLI must not imply that rows are migrated.

## Acceptance Criteria

- `internal/migration` has deterministic mapping tests for success and blockers.
- `vdbg map-migration-records` can generate mapping JSON from a schema plan.
- Failed mappings write a diagnostic report before returning non-zero.
- Output artifacts are `0600`.
- Existing `migrate` behavior remains backward compatible.
- English/Chinese docs, README, and CHANGELOG are updated.
- Full quality gate passes.
