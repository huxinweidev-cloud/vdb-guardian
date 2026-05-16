# pgvector Bulk Import / COPY Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add a safe, opt-in pgvector bulk write path for Milvus → pgvector migrations so production-sized trial runs can move records faster without weakening checkpoint/resume, schema preflight, full-record verification, or stale cleanup safety.

**Architecture:** Keep the current row-by-row upsert writer as the default and add an explicit write-mode boundary around pgvector target writes. Implement `batch-upsert`, `copy`, and `auto` modes behind the existing `PGVectorMigrationTarget` abstraction; `copy` writes one runner batch using pgx `CopyFrom` into a temporary staging table and then performs a set-based `INSERT ... ON CONFLICT DO UPDATE` into the target. `auto` tries COPY first and falls back to batch upsert only for clearly recoverable COPY-path failures, while reporting the mode actually used.

**Tech Stack:** Go, pgx v5, PostgreSQL/pgvector, existing vdbg CLI, existing checkpoint/report artifacts, Docker migration stack smoke tests.

---

## Current State Reconnaissance

### Already implemented

- `VectorMigrationRunner` in `internal/migration/vector_migration.go` reads normalized records and writes fixed-size batches through `vectorMigrationTarget.WriteRecords`.
- Checkpoints are batch-boundary based. A batch is marked completed only after `target.WriteRecords` returns successfully.
- `PGVectorMigrationTarget` in `internal/migration/vector_migration_adapters.go` delegates to `pgvectorMigrationRecordWriter` / `pgvectorMappingMigrationRecordWriter`.
- Real pgvector writer in `internal/migration/vector_migration_real_adapters.go` currently loops over `request.Records` and executes one mapped upsert per record.
- Full-record artifacts, compare, target reconciliation, stale cleanup, coverage gate, and Docker smoke gates already exist.
- `migrate` CLI currently exposes `--batch-size`, checkpoint/resume, schema preflight, and record mapping, but no write mode.

### Missing

- No bulk write abstraction or mode selection.
- No `CopyFrom` adapter seam for unit testing.
- No machine-readable report fields for write mode, fallback count, or approximate throughput.
- No Docker smoke proving COPY migration works with scalar fields, dynamic metadata, partition metadata, full-record compare, and reconciliation.

### Design constraints

- Default behavior must remain `batch-upsert` to avoid surprising production users.
- COPY must not bypass schema preflight, record mapping, checkpoint/resume, artifact permissions, or secret hygiene.
- No real connection URL, password, token, or credential may be committed. Use `[REDACTED]` in docs/examples.
- Errors stored in checkpoints/reports must remain sanitized and must not include connection URLs or raw vectors.
- COPY must preserve upsert semantics; stale cleanup remains a separate explicit command.
- Each runner batch must remain atomic from the checkpoint perspective: if COPY/staging/merge fails, the batch is not marked completed.

---

## Write Mode Contract

Introduce write modes:

```go
type PGVectorMigrationWriteMode string

const (
    PGVectorMigrationWriteModeBatchUpsert PGVectorMigrationWriteMode = "batch-upsert"
    PGVectorMigrationWriteModeCopy        PGVectorMigrationWriteMode = "copy"
    PGVectorMigrationWriteModeAuto        PGVectorMigrationWriteMode = "auto"
)
```

Semantics:

- `batch-upsert`: current row-by-row parameterized upsert path. Default.
- `copy`: use staging table + `CopyFrom` + merge upsert. If COPY fails, return an error and let checkpoint mark the batch failed.
- `auto`: try COPY once per batch; if the failure is classified recoverable for COPY path only, retry the same batch through `batch-upsert` and record fallback. Do not fallback for context cancellation, validation errors, schema errors, or unsafe identifier errors.

Report fields to add:

```go
type VectorMigrationResult struct {
    // existing fields...
    WriteModeRequested string
    WriteModeUsed      string
    CopyBatches        int
    BatchUpsertBatches int
    CopyFallbacks      int
}
```

JSON report should include the same fields under stable snake_case names.

---

## Task 1: Add Write Mode Types and Validation

**Objective:** Define the public write-mode contract and validate it before any database writes.

**Files:**
- Modify: `internal/migration/vector_migration_adapters.go`
- Modify: `internal/migration/vector_migration_real_adapters_test.go`
- Modify: `internal/migration/vector_migration.go`

**Step 1: Write failing tests**

Add tests covering:

```go
func TestValidatePGVectorMigrationWriteModeAcceptsSupportedModes(t *testing.T) {
    for _, mode := range []PGVectorMigrationWriteMode{"", "batch-upsert", "copy", "auto"} {
        if err := validatePGVectorMigrationWriteMode(mode); err != nil {
            t.Fatalf("mode %q rejected: %v", mode, err)
        }
    }
}

func TestValidatePGVectorMigrationWriteModeRejectsUnsupportedMode(t *testing.T) {
    err := validatePGVectorMigrationWriteMode("truncate-and-load")
    if err == nil || !strings.Contains(err.Error(), "write mode") {
        t.Fatalf("expected write mode error, got %v", err)
    }
}
```

**Step 2: Run test to verify failure**

Run:

```bash
go test ./internal/migration -run 'TestValidatePGVectorMigrationWriteMode' -v
```

Expected: FAIL because the type/helper does not exist.

**Step 3: Implement minimal types**

Add the constants and helper. Empty mode should normalize to `batch-upsert`.

**Step 4: Add config field**

Add to `connectors.PGVectorConfig` usage if the struct can accept it cleanly, otherwise add to `PGVectorMigrationTarget` config path:

```go
WriteMode PGVectorMigrationWriteMode
```

Do not change default behavior.

**Step 5: Verify**

Run:

```bash
gofmt -w internal/migration/vector_migration*.go
go test ./internal/migration -run 'TestValidatePGVectorMigrationWriteMode|TestNewPGVectorMigrationTarget' -v
```

Expected: PASS.

---

## Task 2: Add Writer Result Metrics Without Behavior Change

**Objective:** Let target writes return per-batch write-mode metrics while preserving existing target interface behavior.

**Files:**
- Modify: `internal/migration/vector_migration.go`
- Modify: `internal/migration/vector_migration_test.go`
- Modify: `internal/migration/vector_migration_adapters.go`

**Step 1: Write failing tests**

Add a fake target that records two batches with write stats:

```go
func TestVectorMigrationRunnerAggregatesWriteStats(t *testing.T) {
    target := &fakeStatsMigrationTarget{
        results: []VectorMigrationWriteResult{
            {WriteModeUsed: "copy", CopyBatches: 1},
            {WriteModeUsed: "batch-upsert", BatchUpsertBatches: 1, CopyFallbacks: 1},
        },
    }
    // Run 2 batches and assert result.CopyBatches == 1, BatchUpsertBatches == 1, CopyFallbacks == 1.
}
```

**Step 2: Run test to verify failure**

```bash
go test ./internal/migration -run 'TestVectorMigrationRunnerAggregatesWriteStats' -v
```

Expected: FAIL because write result metrics do not exist.

**Step 3: Implement result seam**

Add:

```go
type VectorMigrationWriteResult struct {
    WriteModeUsed      string
    CopyBatches        int
    BatchUpsertBatches int
    CopyFallbacks      int
}
```

Prefer adding an optional interface:

```go
type vectorMigrationTargetWithResult interface {
    WriteRecordsWithResult(ctx context.Context, table string, records []VectorMigrationRecord) (VectorMigrationWriteResult, error)
}
```

The runner should use `WriteRecordsWithResult` when available, otherwise call `WriteRecords` and count the batch as `batch-upsert` for compatibility.

**Step 4: Verify checkpoint behavior unchanged**

Existing checkpoint tests must still pass. The runner must only aggregate stats after successful write.

Run:

```bash
gofmt -w internal/migration/vector_migration.go internal/migration/vector_migration_test.go
go test ./internal/migration -run 'TestVectorMigrationRunner.*Checkpoint|TestVectorMigrationRunnerAggregatesWriteStats' -v
```

Expected: PASS.

---

## Task 3: Add Report Fields for Write Mode Metrics

**Objective:** Surface write-mode metrics in migration JSON/Markdown reports without exposing secrets.

**Files:**
- Modify: `internal/migration/report.go` or the current migration report file
- Modify: matching report tests under `internal/migration/*report*_test.go`
- Modify: `cmd/vdbg/migrate.go` only if report construction requires options plumbing

**Step 1: Locate report builder**

Search:

```bash
rg "BuildVectorMigrationReport|VectorMigrationReport" internal/migration cmd/vdbg
```

**Step 2: Write failing tests**

Test JSON fields:

```go
func TestBuildVectorMigrationReportIncludesWriteModeMetrics(t *testing.T) {
    report := BuildVectorMigrationReport(VectorMigrationResult{
        WriteModeRequested: "auto",
        WriteModeUsed: "batch-upsert",
        CopyBatches: 1,
        BatchUpsertBatches: 1,
        CopyFallbacks: 1,
    }, VectorMigrationReportOptions{})
    // assert snake_case fields are present and no connection URL field exists.
}
```

**Step 3: Implement fields**

Add stable JSON names:

- `write_mode_requested`
- `write_mode_used`
- `copy_batches`
- `batch_upsert_batches`
- `copy_fallbacks`

**Step 4: Verify**

```bash
gofmt -w internal/migration/*report*.go internal/migration/*report*_test.go
go test ./internal/migration -run 'Test.*VectorMigrationReport.*WriteMode' -v
```

Expected: PASS.

---

## Task 4: Add pgx COPY Adapter Seam

**Objective:** Introduce a testable pgx database interface for `CopyFrom`, transactions, staging DDL, and merge DML.

**Files:**
- Modify: `internal/migration/vector_migration_real_adapters.go`
- Modify: `internal/migration/vector_migration_real_adapters_test.go`

**Step 1: Write failing tests**

Create fake DB that records:

- `CREATE TEMP TABLE ... ON COMMIT DROP`
- `CopyFrom` table/columns/rows
- `INSERT INTO target ... SELECT ... FROM staging ON CONFLICT ... DO UPDATE`
- transaction begin/commit/rollback ordering

Tests:

```go
func TestPGXPGVectorMigrationWriterCopyUsesStagingAndMerge(t *testing.T) {}
func TestPGXPGVectorMigrationWriterCopyRollsBackOnCopyError(t *testing.T) {}
func TestPGXPGVectorMigrationWriterCopyRejectsUnsafeIdentifier(t *testing.T) {}
```

**Step 2: Run RED**

```bash
go test ./internal/migration -run 'TestPGXPGVectorMigrationWriterCopy' -v
```

Expected: FAIL because COPY seam does not exist.

**Step 3: Implement interfaces**

Add internal interfaces similar to:

```go
type pgvectorMigrationCopyDB interface {
    Begin(ctx context.Context) (pgvectorMigrationCopyTx, error)
}

type pgvectorMigrationCopyTx interface {
    Exec(ctx context.Context, sql string, args ...any) error
    CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}
```

The concrete pgx wrapper should delegate to `pgx.Conn.Begin`, `tx.Exec`, `tx.CopyFrom`, etc.

**Step 4: Verify**

```bash
gofmt -w internal/migration/vector_migration_real_adapters.go internal/migration/vector_migration_real_adapters_test.go
go test ./internal/migration -run 'TestPGXPGVectorMigrationWriterCopy' -v
```

Expected: PASS.

---

## Task 5: Implement COPY Row Encoding

**Objective:** Convert `VectorMigrationRecord` into COPY rows consistent with mapped upsert semantics.

**Files:**
- Modify: `internal/migration/vector_migration_real_adapters.go`
- Modify: `internal/migration/vector_migration_real_adapters_test.go`

**Step 1: Write failing tests**

Cover:

- vector literal formatting matches existing upsert path
- scalar columns use `record.Scalars[source_field]`
- dynamic metadata is JSONB-compatible
- partition column is included when mapped
- empty batch is no-op
- vector dimension mismatch fails before DML if existing validation is available at this layer; otherwise keep dimension validation in runner and document it

**Step 2: Run RED**

```bash
go test ./internal/migration -run 'TestPGVectorMigrationCopyRows' -v
```

Expected: FAIL.

**Step 3: Implement row builder**

Reuse existing helpers where possible:

- `pgvectorMigrationMappedArgs`
- `marshalPGVectorMigrationJSON`
- `formatPGVectorMigrationLiteral`
- `quotePGVectorSeedIdentifier`

Do not duplicate vector/JSON formatting logic.

**Step 4: Verify**

```bash
gofmt -w internal/migration/vector_migration_real_adapters.go internal/migration/vector_migration_real_adapters_test.go
go test ./internal/migration -run 'TestPGVectorMigrationCopyRows|TestPGXPGVectorMigrationWriterCopy' -v
```

Expected: PASS.

---

## Task 6: Implement `copy` Write Mode

**Objective:** Make `PGVectorMigrationTarget` use COPY when requested.

**Files:**
- Modify: `internal/migration/vector_migration_adapters.go`
- Modify: `internal/migration/vector_migration_real_adapters.go`
- Modify: `internal/migration/vector_migration_real_adapters_test.go`

**Step 1: Write failing tests**

Tests:

```go
func TestPGVectorMigrationTargetUsesCopyMode(t *testing.T) {}
func TestPGVectorMigrationTargetDefaultRemainsBatchUpsert(t *testing.T) {}
func TestPGVectorMigrationTargetCopyModePropagatesCopyError(t *testing.T) {}
```

**Step 2: Run RED**

```bash
go test ./internal/migration -run 'TestPGVectorMigrationTarget.*Copy|TestPGVectorMigrationTargetDefaultRemainsBatchUpsert' -v
```

Expected: FAIL.

**Step 3: Implement mode routing**

`PGVectorMigrationTarget.WriteRecordsWithResult` should build a `PGVectorMigrationWriteRequest` and route based on mode.

- `batch-upsert`: current method.
- `copy`: new COPY method.
- empty: normalize to `batch-upsert`.

**Step 4: Verify**

```bash
gofmt -w internal/migration/vector_migration_adapters.go internal/migration/vector_migration_real_adapters.go
go test ./internal/migration -run 'TestPGVectorMigrationTarget.*Copy|TestPGVectorMigrationTargetDefaultRemainsBatchUpsert' -v
```

Expected: PASS.

---

## Task 7: Implement `auto` Mode Fallback Rules

**Objective:** Allow `auto` mode to try COPY and safely fallback to batch upsert for recoverable COPY execution failures.

**Files:**
- Modify: `internal/migration/vector_migration_adapters.go`
- Modify: `internal/migration/vector_migration_real_adapters.go`
- Modify: tests in `internal/migration/`

**Step 1: Write failing tests**

Tests:

- COPY success in auto reports `copy_batches=1`, no fallback.
- Recoverable COPY error falls back to batch upsert and reports `copy_fallbacks=1`, `batch_upsert_batches=1`.
- Context cancellation does not fallback.
- Unsafe identifier / validation error does not fallback.

**Step 2: Run RED**

```bash
go test ./internal/migration -run 'Test.*Auto.*Copy|Test.*Fallback' -v
```

Expected: FAIL.

**Step 3: Implement classifier**

Add small helper:

```go
func isRecoverablePGVectorCopyFailure(err error) bool
```

Start conservative. Recover only errors explicitly wrapped as COPY execution errors, not validation, context, or schema/identifier errors.

**Step 4: Verify**

```bash
gofmt -w internal/migration/*.go
go test ./internal/migration -run 'Test.*Auto.*Copy|Test.*Fallback' -v
```

Expected: PASS.

---

## Task 8: Add CLI Flags and Parse Tests

**Objective:** Expose write mode from `vdbg migrate` and pass it to target config.

**Files:**
- Modify: `cmd/vdbg/migrate.go`
- Modify: `cmd/vdbg/migrate_test.go`
- Modify: `docs/migrate-cli.md`
- Modify: `docs/zh-CN/migrate-cli.md`

**Step 1: Write failing tests**

Add parse tests:

```go
func TestParseMigrateOptionsWriteModeDefaultsToBatchUpsert(t *testing.T) {}
func TestParseMigrateOptionsAcceptsCopyWriteMode(t *testing.T) {}
func TestParseMigrateOptionsRejectsUnknownWriteMode(t *testing.T) {}
```

**Step 2: Run RED**

```bash
go test ./cmd/vdbg -run 'TestParseMigrateOptions.*WriteMode' -v
```

Expected: FAIL.

**Step 3: Add flag**

```go
flagSet.StringVar(&pgvectorWriteMode, "pgvector-write-mode", "batch-upsert", "pgvector write mode: batch-upsert, copy, or auto")
```

Pass to target config. Do not add `--copy-batch-size` yet unless there is a proven need; the existing runner `--batch-size` remains the COPY batch boundary.

**Step 4: Verify**

```bash
gofmt -w cmd/vdbg/migrate.go cmd/vdbg/migrate_test.go
go test ./cmd/vdbg -run 'TestParseMigrateOptions.*WriteMode|TestRunMigrate' -v
```

Expected: PASS.

---

## Task 9: Add Docker COPY Smoke

**Objective:** Prove COPY mode works against real pgvector and Milvus in the disposable migration stack.

**Files:**
- Create: `scripts/smoke-migration-copy.sh`
- Modify: `Makefile`
- Optional modify: `CONTRIBUTING.md`

**Step 1: Write script skeleton**

Base it on:

- `scripts/smoke-migration-checkpoint-resume.sh`
- `scripts/smoke-target-reconciliation-cleanup.sh`

Required checks:

1. start/check migration stack
2. seed Milvus fixture
3. inspect/schema/map artifacts
4. run `vdbg migrate --pgvector-write-mode copy --checkpoint-path ...`
5. build source and target full-record artifacts
6. run `compare-full-records` and expect pass
7. run `reconcile-target` and expect pass / stale count 0
8. verify report/checkpoint/artifact permissions `0600`
9. scan generated artifacts for obvious secret markers

**Step 2: Add Make target**

```make
smoke-migration-copy:
	scripts/smoke-migration-copy.sh
```

**Step 3: Verify syntax**

```bash
bash -n scripts/smoke-migration-copy.sh
make -n smoke-migration-copy
```

Expected: PASS.

**Step 4: Run smoke**

```bash
make smoke-migration-copy
```

Expected key output:

```text
stack_ready=pass
copy_migration=pass
full_record_compare=pass
target_reconciliation=pass
artifact_permissions=pass
secret_scan=pass
smoke_result=pass
```

---

## Task 10: Documentation Updates

**Objective:** Document write modes, safety behavior, and verification gates in English and Chinese.

**Files:**
- Modify: `docs/migrate-cli.md`
- Modify: `docs/zh-CN/migrate-cli.md`
- Modify: `CONTRIBUTING.md`
- Modify: `CHANGELOG.md`

**Content to add:**

- `--pgvector-write-mode batch-upsert|copy|auto`
- Default remains `batch-upsert`.
- `copy` uses staging + set-based upsert, not automatic stale deletion.
- `auto` fallback behavior and conservative failure classification.
- Existing `--batch-size` controls runner/checkpoint/COPY batch boundary.
- Recommended validation sequence:

```bash
make smoke-migration-copy
make smoke-target-reconciliation-cleanup
```

- Secret hygiene: never paste connection URLs into docs/reports/logs; use `[REDACTED]`.

**Verification:**

```bash
git diff --check docs/migrate-cli.md docs/zh-CN/migrate-cli.md CONTRIBUTING.md CHANGELOG.md
```

Expected: PASS.

---

## Task 11: Final Gates, Review, Commit, Push

**Objective:** Verify the full feature, scan for secrets, get independent review, commit, and push.

**Files:** all changed files.

**Step 1: Run full quality gates**

```bash
make fmt
make lint
make test
make coverage-check
git diff --check
git diff --cached --check
make pre-commit
```

Expected: all PASS.

**Step 2: Run Docker smoke**

```bash
make smoke-migration-copy
```

Expected: PASS. If Docker is unavailable, document the blocker explicitly and do not claim smoke passed.

**Step 3: Secret scan**

Run a staged/tracked/untracked scan. Treat likely real secrets as blockers. Redacted placeholders like `[REDACTED]` are allowed.

```bash
{
  git diff -- . ':(exclude)docs/plans/**'
  git diff --cached -- . ':(exclude)docs/plans/**'
  git ls-files --others --exclude-standard -z | xargs -0 -r grep -InE 'api[_-]?key|secret|password|token|credential|postgres://|postgresql://|Bearer'
} > /tmp/vdb-guardian-secret-scan.txt
```

Review output manually. Do not commit `/tmp/vdb-guardian-secret-scan.txt`.

**Step 4: Independent review**

Use `delegate_task` for final code/spec/security review. Reviewer must check:

- default behavior unchanged
- COPY staging SQL is safe and parameterized where values are involved
- identifiers are validated/quoted
- transaction rollback is guaranteed on failure
- checkpoint marks batches completed only after successful merge
- no connection URL/raw vector leakage in reports/checkpoints/docs
- Docker smoke covers full-record compare and reconciliation

**Step 5: Commit and push**

```bash
git add <changed files>
git commit -m "feat(migration): add pgvector copy write mode"
git push
```

---

## Acceptance Criteria

- `vdbg migrate` defaults to current row-by-row `batch-upsert` behavior.
- `vdbg migrate --pgvector-write-mode copy` writes mapped records through pgx COPY staging + merge upsert.
- `vdbg migrate --pgvector-write-mode auto` safely falls back only for recoverable COPY execution failures.
- Checkpoint/resume remains batch-boundary safe.
- Migration reports include requested/used write mode and fallback counters.
- Full-record compare and target reconciliation still pass after COPY smoke.
- No real secrets, connection URLs, raw vectors, or credentials are committed in docs/tests/reports.
- Full gates and Docker COPY smoke pass before commit.

## Risks and Mitigations

- **Risk:** COPY cannot directly perform upsert. **Mitigation:** COPY into temp staging table, then set-based merge into target using `ON CONFLICT DO UPDATE`.
- **Risk:** fallback could hide schema bugs. **Mitigation:** fallback only for explicitly recoverable COPY execution errors, never for validation/schema/context errors.
- **Risk:** checkpoint could mark partial COPY success. **Mitigation:** wrap staging COPY + merge in one transaction and only return success after commit.
- **Risk:** dynamic metadata formatting diverges from upsert path. **Mitigation:** reuse existing JSON/vector formatting helpers and add row encoding tests.
- **Risk:** tests assert implementation details too tightly. **Mitigation:** assert safety-relevant SQL shape, transaction order, columns, row values, and public metrics; avoid brittle temp table random suffix assertions.
