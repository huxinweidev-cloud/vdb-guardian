# Compare Applied pgvector Schema Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add Phase 6 `compare-applied-schema`, a read-only gate that compares a planned pgvector schema artifact with a live `inspect-pgvector-schema` artifact before full-record migration.

**Architecture:** Reuse the existing schema comparison pattern in `internal/schema/pgvector_compare.go`, but compare `PGVectorSchemaPlan` to `PGVectorLiveSchemaInspection` instead of Milvus inspection to schema plan. The CLI consumes only local JSON artifacts, writes a deterministic `0600` JSON report, and returns non-zero when blocking drift is found while still leaving the report for diagnosis.

**Tech Stack:** Go standard library, existing `internal/schema` plan/live inspection structs, existing `cmd/vdbg` CLI patterns, Markdown docs in English and Chinese.

---

## Repository reconnaissance

### Existing internal plan / roadmap fragments

The repository now documents this phased chain:

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
  -> inspect-pgvector-schema
  -> future compare-applied-schema
```

Relevant docs:

- `docs/apply-pgvector-schema-cli.md` says future `compare-applied-schema` should validate live PostgreSQL schema after execution.
- `docs/inspect-pgvector-schema-cli.md` says future `compare-applied-schema` should compare live inspection against planned pgvector schema.
- `docs/migrate-cli.md` now mentions the planning/apply/live-inspection chain before record transfer.

### Implemented capabilities

Already implemented and committed:

- `vdbg inspect-milvus`
- `vdbg plan-pgvector-schema`
- `vdbg compare-schema-plans`
- `vdbg apply-pgvector-schema`
- `vdbg inspect-pgvector-schema`

### Missing coherent design / unresolved gaps

Missing is the read-only comparison gate between:

```text
pgvector schema plan artifact
  vs
live PostgreSQL/pgvector schema inspection artifact
```

Without it, `apply-pgvector-schema` success plus `inspect-pgvector-schema` inventory still does not produce a single machine-readable pass/fail gate for drift.

### Proposed phase

Implement Phase 6:

```bash
go run ./cmd/vdbg compare-applied-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --output /tmp/vdb-guardian-applied-schema-compare-report.json
```

---

## Scope

### Do

- Compare schema plan table/column/index expectations against live pgvector schema inspection.
- Emit deterministic JSON report with `schema_version: "v1"`.
- Return non-zero when blocking mismatches exist.
- Write report before returning mismatch error.
- Write output files with `0600` permissions.
- Keep command read-only over local JSON files.
- Keep stdout/report free of database connection URLs.
- Update English and Chinese docs, README, and CHANGELOG.

### Do not

- Do not connect to PostgreSQL.
- Do not execute DDL/DML.
- Do not repair drift.
- Do not migrate records.
- Do not inspect row payloads.
- Do not implement checkpoint/resume.

---

## JSON report shape

Use a new artifact type, not `PlanCompareReport`, because this compares planned schema to live schema.

```json
{
  "schema_version": "v1",
  "status": "pass",
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "live_schema": "/tmp/vdb-guardian-live-pgvector-schema.json",
  "summary": {
    "tables_checked": 1,
    "columns_checked": 4,
    "indexes_checked": 1,
    "mismatch_count": 0,
    "warning_count": 0
  },
  "tables": [
    {
      "target_table": "items",
      "status": "pass",
      "checks": [
        {
          "name": "table_present",
          "status": "pass",
          "target": "items"
        },
        {
          "name": "column_type_matches",
          "status": "pass",
          "source": "embedding vector(1536)",
          "target": "embedding vector(1536)"
        }
      ]
    }
  ]
}
```

Reuse status strings from existing schema comparison if practical:

```go
SchemaPlanCompareStatusPass
SchemaPlanCompareStatusFail
SchemaPlanCompareStatusWarn
```

---

## Comparison rules

Blocking mismatches:

1. `schema_version` mismatch for either input.
2. Planned target schema differs from live target schema.
3. Planned table missing in live inspection.
4. Planned column missing in live table.
5. Planned column target type does not match live formatted/type:
   - plan `bigint` matches live `bigint`;
   - plan `text` matches live `text`;
   - plan `jsonb` matches live `jsonb`;
   - plan `vector(N)` matches live `FormattedType == vector(N)` or `Type == vector` plus `VectorDimension == N`.
6. Planned primary key column is not primary key live.
7. Planned nullable differs from live nullable.
8. Planned supported index with `CreateIndexSQL != ""` missing live index by target index name.
9. Planned supported index method differs from live method when method can be parsed from plan target index type.

Warnings, not blockers:

1. Live table exists but is not in plan.
2. Live column exists but is not in plan.
3. Live index exists but is not in plan.
4. Plan has unsupported features already represented as warnings.
5. pgvector extension missing should be a blocking mismatch if any vector column is planned; otherwise warning.

---

## Files

Create:

- `internal/schema/pgvector_applied_compare.go`
- `internal/schema/pgvector_applied_compare_test.go`
- `cmd/vdbg/compare_applied_schema.go`
- `cmd/vdbg/compare_applied_schema_test.go`
- `docs/compare-applied-schema-cli.md`
- `docs/zh-CN/compare-applied-schema-cli.md`

Modify:

- `cmd/vdbg/main.go`
- `README.md`
- `README.zh-CN.md`
- `CHANGELOG.md`
- `docs/apply-pgvector-schema-cli.md`
- `docs/zh-CN/apply-pgvector-schema-cli.md`
- `docs/inspect-pgvector-schema-cli.md`
- `docs/zh-CN/inspect-pgvector-schema-cli.md`
- `docs/migrate-cli.md`
- `docs/zh-CN/migrate-cli.md`

---

## Task 1: RED core compare happy path

**Objective:** Add failing tests for a matching schema plan and live schema inspection.

**Files:**

- Create: `internal/schema/pgvector_applied_compare_test.go`
- Later create: `internal/schema/pgvector_applied_compare.go`

**Step 1: Write failing test**

Add test:

```go
func TestCompareAppliedPGVectorSchemaPassesForMatchingLiveSchema(t *testing.T) {
    plan := appliedCompareSchemaPlanFixture()
    live := appliedCompareLiveSchemaFixture()

    report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{
        SchemaPlanPath: "/tmp/schema-plan.json",
        LiveSchemaPath: "/tmp/live-schema.json",
    })
    if err != nil {
        t.Fatalf("CompareAppliedPGVectorSchema returned error: %v", err)
    }
    if report.Status != SchemaPlanCompareStatusPass {
        t.Fatalf("expected pass, got %#v", report)
    }
    if report.Summary.MismatchCount != 0 || report.Summary.TablesChecked != 1 || report.Summary.ColumnsChecked != 2 {
        t.Fatalf("unexpected summary: %#v", report.Summary)
    }
    assertAppliedCheck(t, report.Tables[0].Checks, "table_present")
    assertAppliedCheck(t, report.Tables[0].Checks, "column_type_matches")
    assertAppliedCheck(t, report.Tables[0].Checks, "primary_key_preserved")
    assertAppliedCheck(t, report.Tables[0].Checks, "vector_dimension_preserved")
}
```

**Step 2: Run RED**

```bash
go test ./internal/schema -run 'TestCompareAppliedPGVectorSchema' -v
```

Expected: FAIL because `CompareAppliedPGVectorSchema` and report types do not exist.

---

## Task 2: GREEN minimal report structs and matching comparison

**Objective:** Implement minimal structs and matching comparison to satisfy the happy path.

**Files:**

- Create: `internal/schema/pgvector_applied_compare.go`

**Implementation notes:**

Add:

```go
const AppliedSchemaCompareReportVersion = "v1"

type AppliedSchemaCompareOptions struct {
    SchemaPlanPath string
    LiveSchemaPath string
}

type AppliedSchemaCompareReport struct {
    SchemaVersion string                         `json:"schema_version"`
    Status        string                         `json:"status"`
    SchemaPlan    string                         `json:"schema_plan,omitempty"`
    LiveSchema    string                         `json:"live_schema,omitempty"`
    Summary       AppliedSchemaCompareSummary    `json:"summary"`
    Tables        []AppliedTableComparison       `json:"tables"`
}
```

Compare by `PGVectorTablePlan.TargetTable` to `PGVectorLiveTableInspection.TargetTable`.

**Step 2: Run GREEN**

```bash
go test ./internal/schema -run 'TestCompareAppliedPGVectorSchemaPassesForMatchingLiveSchema' -v
```

Expected: PASS.

---

## Task 3: RED/GREEN table and column drift tests

**Objective:** Add blockers for missing table, missing column, type drift, nullable drift, and primary key drift.

**Files:**

- Modify: `internal/schema/pgvector_applied_compare_test.go`
- Modify: `internal/schema/pgvector_applied_compare.go`

**Tests:**

```go
func TestCompareAppliedPGVectorSchemaFailsWhenTableMissing(t *testing.T)
func TestCompareAppliedPGVectorSchemaFailsWhenColumnMissing(t *testing.T)
func TestCompareAppliedPGVectorSchemaFailsWhenColumnTypeDiffers(t *testing.T)
func TestCompareAppliedPGVectorSchemaFailsWhenNullableDiffers(t *testing.T)
func TestCompareAppliedPGVectorSchemaFailsWhenPrimaryKeyMissing(t *testing.T)
```

**Run each RED before implementation:**

```bash
go test ./internal/schema -run 'TestCompareAppliedPGVectorSchemaFailsWhenTableMissing' -v
```

Expected: FAIL until implemented.

**Implementation notes:**

Add helper functions:

```go
func compareAppliedColumn(table *AppliedTableComparison, planned PGVectorColumnPlan, live PGVectorLiveColumnInspection)
func appliedColumnTypeMatches(plannedType string, live PGVectorLiveColumnInspection) bool
func formatLiveColumnType(live PGVectorLiveColumnInspection) string
```

---

## Task 4: RED/GREEN vector extension and dimension checks

**Objective:** Ensure planned vector columns require installed pgvector extension and matching dimensions.

**Files:**

- Modify: `internal/schema/pgvector_applied_compare_test.go`
- Modify: `internal/schema/pgvector_applied_compare.go`

**Tests:**

```go
func TestCompareAppliedPGVectorSchemaFailsWhenVectorDimensionDiffers(t *testing.T)
func TestCompareAppliedPGVectorSchemaFailsWhenVectorExtensionMissingForVectorPlan(t *testing.T)
```

**Implementation notes:**

- If any plan column target type starts with `vector(` and `live.Extension.Installed == false`, add report-level/table-level mismatch `pgvector_extension_installed`.
- Dimension check should parse planned `vector(N)` and compare to live `VectorDimension`.

---

## Task 5: RED/GREEN index checks

**Objective:** Validate planned supported indexes are present live and method-compatible.

**Files:**

- Modify: `internal/schema/pgvector_applied_compare_test.go`
- Modify: `internal/schema/pgvector_applied_compare.go`

**Tests:**

```go
func TestCompareAppliedPGVectorSchemaFailsWhenPlannedIndexMissing(t *testing.T)
func TestCompareAppliedPGVectorSchemaFailsWhenIndexMethodDiffers(t *testing.T)
func TestCompareAppliedPGVectorSchemaIgnoresFlatExactScanIndexes(t *testing.T)
```

**Implementation notes:**

- Existing `PGVectorIndexPlan` has `TargetIndex`, `TargetIndexType`, `CreateIndexSQL`, `SupportLevel`.
- If `CreateIndexSQL == ""` or target index type is `flat`, do not require a live index.
- If target index type is `hnsw` or `ivfflat`, require live index by `Name == TargetIndex` and `Method == TargetIndexType`.

---

## Task 6: RED/GREEN extra live objects as warnings

**Objective:** Surface live drift that is non-blocking but should be visible.

**Files:**

- Modify: `internal/schema/pgvector_applied_compare_test.go`
- Modify: `internal/schema/pgvector_applied_compare.go`

**Tests:**

```go
func TestCompareAppliedPGVectorSchemaWarnsForExtraLiveColumn(t *testing.T)
func TestCompareAppliedPGVectorSchemaWarnsForExtraLiveIndex(t *testing.T)
func TestCompareAppliedPGVectorSchemaWarnsForExtraLiveTable(t *testing.T)
```

**Implementation notes:**

Report status should be:

- `fail` if mismatches > 0;
- `warn` if no mismatches but warnings > 0;
- `pass` otherwise.

---

## Task 7: RED/GREEN artifact schema version tests

**Objective:** Reject unsupported input schema versions clearly.

**Files:**

- Modify: `internal/schema/pgvector_applied_compare_test.go`
- Modify: `internal/schema/pgvector_applied_compare.go`

**Tests:**

```go
func TestCompareAppliedPGVectorSchemaRejectsUnsupportedPlanVersion(t *testing.T)
func TestCompareAppliedPGVectorSchemaRejectsUnsupportedLiveVersion(t *testing.T)
```

Expected error messages:

```text
unsupported pgvector schema plan version
unsupported pgvector live schema inspection version
```

---

## Task 8: RED CLI tests

**Objective:** Add failing CLI tests for local JSON artifact comparison.

**Files:**

- Create: `cmd/vdbg/compare_applied_schema_test.go`
- Later create: `cmd/vdbg/compare_applied_schema.go`

**Tests:**

```go
func TestRunCompareAppliedSchemaWritesReportWith0600Permissions(t *testing.T)
func TestRunCompareAppliedSchemaReturnsErrorButWritesReportOnMismatch(t *testing.T)
func TestRunCompareAppliedSchemaRequiresSchemaPlan(t *testing.T)
func TestRunCompareAppliedSchemaRequiresLiveSchema(t *testing.T)
func TestRunCompareAppliedSchemaWritesJSONToStdoutWhenNoOutput(t *testing.T)
```

**Run RED:**

```bash
go test ./cmd/vdbg -run 'TestRunCompareAppliedSchema' -v
```

Expected: FAIL because CLI function does not exist.

---

## Task 9: GREEN CLI implementation

**Objective:** Implement `vdbg compare-applied-schema` command.

**Files:**

- Create: `cmd/vdbg/compare_applied_schema.go`
- Modify: `cmd/vdbg/main.go`

**CLI behavior:**

Required flags:

```text
--schema-plan
--live-schema
```

Optional:

```text
--output
```

If `--output` provided:

- write pretty JSON report with `0600`;
- print compact summary;
- if mismatch, return non-zero after writing report.

If no `--output`:

- write pretty JSON report to stdout;
- if mismatch, still return non-zero after stdout write.

Error string for mismatch:

```text
applied schema comparison failed with N mismatch(es)
```

**Run GREEN:**

```bash
go test ./cmd/vdbg -run 'TestRunCompareAppliedSchema' -v
```

Expected: PASS.

---

## Task 10: Docs and README/CHANGELOG sync

**Objective:** Document the new gate in English and Chinese.

**Files:**

- Create: `docs/compare-applied-schema-cli.md`
- Create: `docs/zh-CN/compare-applied-schema-cli.md`
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/apply-pgvector-schema-cli.md`
- Modify: `docs/zh-CN/apply-pgvector-schema-cli.md`
- Modify: `docs/inspect-pgvector-schema-cli.md`
- Modify: `docs/zh-CN/inspect-pgvector-schema-cli.md`
- Modify: `docs/migrate-cli.md`
- Modify: `docs/zh-CN/migrate-cli.md`

**Docs must include:**

- Command examples.
- JSON report shape.
- Pass/warn/fail semantics.
- Read-only safety notes.
- Mismatch rules.
- Current limitations.

**Example command:**

```bash
go run ./cmd/vdbg compare-applied-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --output /tmp/vdb-guardian-applied-schema-compare-report.json
```

---

## Task 11: Full verification and commit

**Objective:** Run all quality gates, secret-scan staged diff, commit, and push.

**Commands:**

```bash
make fmt && make lint && make test && git diff --check && make pre-commit
```

Then inspect status and staged diff:

```bash
git status --short --branch
git diff --stat
git diff | grep -Ei '(api[_-]?key|token|secret|password|BEGIN (RSA|OPENSSH|PRIVATE)|postgres(ql)?://[^[:space:]]+:[^[:space:]@]+@)' || true
```

Stage and scan:

```bash
git add CHANGELOG.md README.md README.zh-CN.md \
  cmd/vdbg/main.go cmd/vdbg/compare_applied_schema.go cmd/vdbg/compare_applied_schema_test.go \
  docs/compare-applied-schema-cli.md docs/zh-CN/compare-applied-schema-cli.md \
  docs/apply-pgvector-schema-cli.md docs/zh-CN/apply-pgvector-schema-cli.md \
  docs/inspect-pgvector-schema-cli.md docs/zh-CN/inspect-pgvector-schema-cli.md \
  docs/migrate-cli.md docs/zh-CN/migrate-cli.md \
  internal/schema/pgvector_applied_compare.go internal/schema/pgvector_applied_compare_test.go

git diff --cached | grep -Ei '(api[_-]?key|token|secret|password|BEGIN (RSA|OPENSSH|PRIVATE)|postgres(ql)?://[^[:space:]]+:[^[:space:]@]+@)' || true
```

Commit:

```bash
git commit -m "feat(schema): add applied schema comparison CLI" \
  -m "- Compare pgvector schema plans against live schema inspections before data migration
- Report table, column, vector dimension, primary key, nullable, and index drift
- Add read-only CLI, bilingual docs, and diagnostic report artifacts"

git push origin feat/enterprise-scaffold
```

---

## Acceptance criteria

- `vdbg compare-applied-schema` exists and is wired in `cmd/vdbg/main.go`.
- It accepts `--schema-plan`, `--live-schema`, and optional `--output`.
- It writes deterministic JSON with `schema_version: "v1"`.
- It writes `--output` files as `0600`.
- It returns non-zero on blocking mismatch after writing report.
- It does not connect to PostgreSQL or mutate anything.
- It compares table, column, type, nullable, primary key, vector dimension, extension presence, and planned index presence/method.
- Extra live objects are warnings, not blockers.
- English and Chinese docs are synced.
- Full gate passes:

```bash
make fmt && make lint && make test && git diff --check && make pre-commit
```

---

## Risk notes

- Index operator-class comparison is intentionally deferred; first phase validates method and presence by planned index name.
- Existing live primary key indexes may appear as extra live indexes; do not block on extra indexes.
- Matching type strings must handle `vector(N)` via `FormattedType` and `VectorDimension`, not only `Type`.
- If docs include PostgreSQL URL examples, use `[REDACTED]` or username-only placeholders without passwords to avoid secret-scan noise.
