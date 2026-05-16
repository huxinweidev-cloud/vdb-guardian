# Target Reconciliation and Stale Cleanup Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add a safe target reconciliation workflow that detects stale pgvector rows after Milvus-to-pgvector migration, reports missing/extra/changed records from local artifacts, and only deletes stale target rows behind an explicit destructive confirmation gate.

**Architecture:** Build this as an artifact-first extension of the existing full-record artifact model. The first command (`vdbg reconcile-target`) is local and read-only: it compares source/target full-record artifacts and writes a `0600` machine-readable reconciliation report. The second command (`vdbg cleanup-target-stale`) consumes a failing reconciliation report and deletes only explicitly reported stale target IDs from pgvector when `--confirm-delete-stale` is present; orchestration into `migrate-and-verify` is documentation-only for this phase.

**Tech Stack:** Go CLI and internal migration package, existing full-record artifacts, pgx-backed pgvector connector/target helper, Docker migration stack smoke scripts, Markdown/JSON docs in English and Chinese.

---

## Repository Rules and Current State

From `CLAUDE.md`:

- Complex work must be planned and approved before production code changes.
- TDD is mandatory: write failing tests first, run and observe RED, then implement minimal code.
- Go code must be `gofmt` formatted.
- Public exported Go identifiers need meaningful Go doc comments.
- Do not commit real secrets, tokens, passwords, or connection strings. Use `[REDACTED]` in docs and summaries.
- File artifacts that may contain IDs/metadata should be written with `0600` where practical.
- Before commit run the established quality gates, including coverage and pre-commit.

Current implemented migration surface:

- `vdbg migrate` supports Milvus -> pgvector migration, schema preflight, full-record mapping, checkpoint/resume, JSON report.
- `vdbg migrate-and-verify` supports migration + fingerprint compare + optional full-record compare + checkpoint/resume passthrough.
- `vdbg build-milvus-record-artifact` and `vdbg build-pgvector-record-artifact` build local full-record artifacts.
- `vdbg compare-full-records` already detects missing source records, missing target records, and mismatches between artifacts.
- `make coverage-check` and `make smoke-migration-checkpoint` are available.

Gap this plan closes:

- The existing migration uses pgvector upsert semantics and does not remove target rows that are absent from Milvus.
- Existing compare reports can show stale target IDs as `missing_source_ids`, but there is no explicit reconciliation report, no operator-focused stale/missing/changed classification, and no safe cleanup command.

---

## Non-Goals for This Phase

- Do not add automatic cleanup to `migrate` or `migrate-and-verify`.
- Do not default to destructive deletion.
- Do not introduce source-side streaming cursor semantics.
- Do not implement bulk import/COPY.
- Do not generalize provider abstraction beyond naming report fields in a future-friendly way.
- Do not delete rows based on live source reads; cleanup must consume a local reconciliation report produced from approved artifacts.

---

## Artifact Contract

Create a new report schema in `internal/migration/target_reconciliation.go`:

```go
const TargetReconciliationReportVersion = "v1"

const (
    TargetReconciliationStatusPass = "pass"
    TargetReconciliationStatusFail = "fail"
)

type TargetReconciliationReport struct {
    SchemaVersion string                       `json:"schema_version"`
    Status        string                       `json:"status"`
    Source        FullRecordCompareEndpoint    `json:"source"`
    Target        FullRecordCompareEndpoint    `json:"target"`
    Summary       TargetReconciliationSummary  `json:"summary"`
    StaleTargetIDs []string                    `json:"stale_target_ids"`
    MissingTargetIDs []string                  `json:"missing_target_ids"`
    ChangedRecordIDs []string                  `json:"changed_record_ids"`
    Mismatches     []FullRecordMismatch        `json:"mismatches,omitempty"`
}

type TargetReconciliationSummary struct {
    SourceRecordCount int `json:"source_record_count"`
    TargetRecordCount int `json:"target_record_count"`
    MatchedRecordCount int `json:"matched_record_count"`
    StaleTargetCount int `json:"stale_target_count"`
    MissingTargetCount int `json:"missing_target_count"`
    ChangedRecordCount int `json:"changed_record_count"`
}
```

Mapping from existing full-record comparison:

- `FullRecordCompareReport.MissingSourceIDs` -> `StaleTargetIDs` because target contains IDs not present in source.
- `FullRecordCompareReport.MissingTargetIDs` -> `MissingTargetIDs` because source contains IDs not present in target.
- `FullRecordCompareReport.Mismatches` grouped by ID -> `ChangedRecordIDs`.
- `status=pass` only when stale, missing, and changed counts are all zero.

Security and artifact rules:

- `reconcile-target` must never connect to a database.
- `cleanup-target-stale` must never read Milvus.
- Reports can include record IDs and mismatch values; write with `0600` and document secured artifact handling.
- Reports must not include connection URLs or credentials.
- Cleanup output must print counts and paths, not row payloads.

---

## Task 1: Add Reconciliation Model RED Tests

**Objective:** Define the expected report mapping and deterministic output before implementation.

**Files:**

- Create/modify test: `internal/migration/target_reconciliation_test.go`
- Later implementation: `internal/migration/target_reconciliation.go`

**Step 1: Write failing tests**

Add tests that construct two `FullRecordArtifact` values:

- source records: `sku-1`, `sku-2`, `sku-3`
- target records: `sku-1`, `sku-3`, `sku-stale`
- `sku-3` has a scalar or vector mismatch

Expected reconciliation report:

- `Status == TargetReconciliationStatusFail`
- `Summary.SourceRecordCount == 3`
- `Summary.TargetRecordCount == 3`
- `Summary.MatchedRecordCount == 2`
- `Summary.StaleTargetCount == 1`
- `Summary.MissingTargetCount == 1`
- `Summary.ChangedRecordCount == 1`
- `StaleTargetIDs == []string{"sku-stale"}`
- `MissingTargetIDs == []string{"sku-2"}`
- `ChangedRecordIDs == []string{"sku-3"}`
- IDs sorted deterministically

Also add a pass case where artifacts match exactly.

**Step 2: Run RED**

```bash
go test ./internal/migration -run 'TestBuildTargetReconciliationReport' -v
```

Expected: FAIL because `BuildTargetReconciliationReport` and related types do not exist.

**Step 3: Implement minimal model**

Create `internal/migration/target_reconciliation.go` with exported types and Go doc comments. Implement:

```go
func BuildTargetReconciliationReport(source, target FullRecordArtifact) (TargetReconciliationReport, error)
```

It should call `CompareFullRecordArtifacts` and map the existing report to the reconciliation report.

**Step 4: Run GREEN**

```bash
gofmt -w internal/migration/target_reconciliation.go internal/migration/target_reconciliation_test.go
go test ./internal/migration -run 'TestBuildTargetReconciliationReport' -v
```

Expected: PASS.

---

## Task 2: Add Reconciliation JSON Marshaling and Permissions Tests

**Objective:** Ensure reconciliation reports have stable JSON and are safe to persist.

**Files:**

- Modify: `internal/migration/target_reconciliation.go`
- Modify: `internal/migration/target_reconciliation_test.go`

**Step 1: Write failing tests**

Add tests for:

- `MarshalTargetReconciliationReport` emits indented JSON ending with newline.
- `WriteTargetReconciliationReport(path, report)` creates parent directories, writes `0600`, and tightens an existing broad-permission file to `0600`.
- JSON does not include obvious connection-string fields. Use a simple string assertion that no `connection_url` or `pgvector_connection_url` appears.

**Step 2: Run RED**

```bash
go test ./internal/migration -run 'Test.*TargetReconciliation.*Marshal|Test.*TargetReconciliation.*Write' -v
```

Expected: FAIL until helpers exist.

**Step 3: Implement minimal helpers**

Add:

```go
func MarshalTargetReconciliationReport(report TargetReconciliationReport) ([]byte, error)
func WriteTargetReconciliationReport(path string, report TargetReconciliationReport) error
```

Use `json.MarshalIndent`, append newline, `os.MkdirAll(filepath.Dir(path), 0o755)`, `os.WriteFile(path, data, 0o600)`, then `os.Chmod(path, 0o600)`.

**Step 4: Run GREEN**

```bash
gofmt -w internal/migration/target_reconciliation.go internal/migration/target_reconciliation_test.go
go test ./internal/migration -run 'Test.*TargetReconciliation' -v
```

Expected: PASS.

---

## Task 3: Add `vdbg reconcile-target` CLI RED Tests

**Objective:** Add an artifact-only CLI for stale row audit.

**Files:**

- Create test: `cmd/vdbg/reconcile_target_test.go`
- Later implementation: `cmd/vdbg/reconcile_target.go`
- Modify dispatch: `cmd/vdbg/main.go`

**Step 1: Write failing tests**

Test parse/validation:

- missing `--source` fails with `source full-record artifact path is required`
- missing `--target` fails with `target full-record artifact path is required`
- missing `--output` fails with `output path is required`

Test run:

- Write source/target artifact fixtures to temp files.
- Run `runReconcileTargetCommand([]string{"--source", sourcePath, "--target", targetPath, "--output", outputPath})`.
- Expect non-nil error when status fail, but output file still exists.
- Assert output mode `0600`.
- Decode JSON and assert stale/missing/changed counts.
- Add pass case where command returns nil for matching artifacts.

**Step 2: Run RED**

```bash
go test ./cmd/vdbg -run 'TestParseReconcileTarget|TestRunReconcileTarget' -v
```

Expected: FAIL because command does not exist.

**Step 3: Implement CLI**

Create `cmd/vdbg/reconcile_target.go`:

- Parse flags: `--source`, `--target`, `--output`.
- Reuse existing `readFullRecordArtifact` from `compare_full_records.go`.
- Call `migration.BuildTargetReconciliationReport`.
- Call `migration.WriteTargetReconciliationReport`.
- Print summary:

```text
target reconciliation completed
status: fail
source_records: 100
target_records: 103
stale_target_records: 3
missing_target_records: 0
changed_records: 0
result: /tmp/reconcile.json
```

Return non-zero error on `status: fail` after preserving output.

Modify `cmd/vdbg/main.go` to dispatch `reconcile-target`.

**Step 4: Run GREEN**

```bash
gofmt -w cmd/vdbg/reconcile_target.go cmd/vdbg/reconcile_target_test.go cmd/vdbg/main.go
go test ./cmd/vdbg -run 'TestParseReconcileTarget|TestRunReconcileTarget' -v
```

Expected: PASS.

---

## Task 4: Add Cleanup Target Stale Internal Contract RED Tests

**Objective:** Define destructive cleanup safety contract before any DML implementation.

**Files:**

- Create/modify test: `internal/migration/target_cleanup_test.go`
- Later implementation: `internal/migration/target_cleanup.go`

**Step 1: Write failing tests**

Define a small interface for testability:

```go
type TargetStaleDeleter interface {
    DeleteTargetRecords(ctx context.Context, table string, ids []string) (int64, error)
}
```

Test a pure cleanup runner/helper such as:

```go
func CleanupStaleTargetRecords(ctx context.Context, deleter TargetStaleDeleter, request TargetStaleCleanupRequest) (TargetStaleCleanupResult, error)
```

RED tests:

- Reject when `ConfirmDeleteStale == false` and report has stale IDs.
- No-op/pass when stale ID list empty.
- Reject if report schema version unsupported.
- Reject if report status is `pass` but stale IDs are non-empty, or count mismatches length.
- Delete only `StaleTargetIDs`, not missing/changed IDs.
- Preserve stable sorted ID order.
- Return deleted count.

**Step 2: Run RED**

```bash
go test ./internal/migration -run 'TestCleanupStaleTargetRecords' -v
```

Expected: FAIL until types/helpers exist.

**Step 3: Implement minimal helper**

Create `internal/migration/target_cleanup.go` with exported docs:

```go
type TargetStaleCleanupRequest struct {
    TargetTable string
    Report TargetReconciliationReport
    ConfirmDeleteStale bool
}

type TargetStaleCleanupResult struct {
    TargetTable string `json:"target_table"`
    RequestedDeleteCount int `json:"requested_delete_count"`
    DeletedCount int64 `json:"deleted_count"`
    DryRun bool `json:"dry_run"`
}
```

Implement strict validation.

**Step 4: Run GREEN**

```bash
gofmt -w internal/migration/target_cleanup.go internal/migration/target_cleanup_test.go
go test ./internal/migration -run 'TestCleanupStaleTargetRecords' -v
```

Expected: PASS.

---

## Task 5: Add pgvector Deleter Implementation

**Objective:** Provide the live pgvector deletion adapter used by CLI cleanup.

**Files:**

- Modify or create: `internal/migration/pgvector_stale_deleter.go`
- Test: `internal/migration/pgvector_stale_deleter_test.go` if existing pgx test style allows mock/pool injection; otherwise unit-test SQL builder separately.

**Step 1: Inspect existing pgvector writer**

Read:

- `internal/migration/pgvector_target.go` or similarly named pgvector writer file.
- Any existing fake or SQL helper tests.

**Step 2: Write RED tests for SQL safety helper**

If live pgx injection is awkward, create a small helper:

```go
func buildDeleteTargetRecordsSQL(table, idColumn string, ids []string) (string, []any, error)
```

Test:

- Empty ids rejected/no-op before SQL.
- Table and id column identifiers are quoted or validated using the existing identifier strategy from pgvector writer/schema code.
- Uses parameterized ids, not string interpolation for values.

**Step 3: Implement deleter**

`PGVectorTargetStaleDeleter` should:

- Open pgx connection with provided config.
- Close it after command-level use.
- Delete IDs with parameterized SQL.
- Return affected row count.
- Never log or store the connection URL.

Prefer adding this near existing pgvector migration target code to reuse identifier quoting/validation.

**Step 4: Run tests**

```bash
gofmt -w internal/migration/pgvector_stale_deleter.go internal/migration/pgvector_stale_deleter_test.go
go test ./internal/migration -run 'Test.*DeleteTargetRecords|Test.*StaleDeleter' -v
```

Expected: PASS.

---

## Task 6: Add `vdbg cleanup-target-stale` CLI RED Tests

**Objective:** Add explicit destructive cleanup CLI with safe defaults.

**Files:**

- Create: `cmd/vdbg/cleanup_target_stale.go`
- Create: `cmd/vdbg/cleanup_target_stale_test.go`
- Modify: `cmd/vdbg/main.go`

**Step 1: Write failing parse tests**

Flags:

- `--reconcile-report`: required
- `--pgvector-connection-url`: required for real cleanup
- `--target-table`: required or default `items`? Prefer required for safety unless existing CLI default convention strongly suggests `items`.
- `--pgvector-id-column`: default `id`
- `--confirm-delete-stale`: required for deletion
- `--output`: optional cleanup result JSON path, written `0600` if present

Parse tests:

- missing report fails
- missing connection URL fails
- missing `--confirm-delete-stale` fails when report has stale IDs
- pass report/no stale IDs returns nil without deletion

**Step 2: Write run tests using fake deleter**

Implement `runCleanupTargetStaleWithDeleter` or factory injection.

Tests:

- Failing reconciliation report with stale IDs calls fake deleter with exactly those IDs.
- Missing/changed IDs are not deleted.
- Output JSON contains deleted count and is `0600`.
- Cleanup errors are returned without leaking connection URL.

**Step 3: Run RED**

```bash
go test ./cmd/vdbg -run 'TestParseCleanupTargetStale|TestRunCleanupTargetStale' -v
```

Expected: FAIL until command exists.

**Step 4: Implement CLI**

Create command file and main dispatch. Print summary:

```text
stale target cleanup completed
target_table: items
requested_delete_count: 3
deleted_count: 3
result: /tmp/cleanup-result.json
```

Never print connection URL.

**Step 5: Run GREEN**

```bash
gofmt -w cmd/vdbg/cleanup_target_stale.go cmd/vdbg/cleanup_target_stale_test.go cmd/vdbg/main.go
go test ./cmd/vdbg -run 'TestParseCleanupTargetStale|TestRunCleanupTargetStale' -v
```

Expected: PASS.

---

## Task 7: Docker E2E Stale Row Smoke

**Objective:** Prove stale detection and explicit cleanup against real pgvector in the local migration stack.

**Files:**

- Create: `scripts/smoke-target-reconciliation.sh`
- Modify: `Makefile`
- Maybe reuse artifacts and helpers from `scripts/smoke-migration-checkpoint-resume.sh`

**Step 1: Write script skeleton**

Behavior:

1. Start/check local migration stack.
2. Seed Milvus fixture.
3. Apply pgvector schema and migrate records.
4. Insert one stale row directly into pgvector with ID `stale-row-1` and valid vector dimension.
5. Build live source and target full-record artifacts with existing builders.
6. Run `vdbg reconcile-target` and assert it fails after writing report.
7. Assert report has `stale_target_count=1` and includes `stale-row-1`.
8. Run `vdbg cleanup-target-stale --confirm-delete-stale`.
9. Rebuild target artifact and rerun `vdbg reconcile-target`, expecting pass.
10. Check report/result artifact permissions are `0600`.
11. Secret scan generated artifacts.

**Step 2: Add Make target**

Add:

```make
smoke-target-reconciliation:
	scripts/smoke-target-reconciliation.sh
```

Do not add it to default `make test`.

**Step 3: Run smoke**

```bash
bash -n scripts/smoke-target-reconciliation.sh
make smoke-target-reconciliation
```

Expected markers:

```text
stack_ready=pass
milvus_seed=pass
migration=pass
stale_inserted=pass
reconcile_detected_stale=pass
cleanup_deleted=pass
reconcile_after_cleanup=pass
artifact_permissions=pass
secret_scan=pass
smoke_result=pass
```

---

## Task 8: Documentation Updates

**Objective:** Document safe reconciliation and cleanup for operators.

**Files:**

- Create: `docs/target-reconciliation-cli.md`
- Create: `docs/zh-CN/target-reconciliation-cli.md`
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `CHANGELOG.md`
- Modify as relevant: `docs/migrate-and-verify-cli.md`, `docs/zh-CN/migrate-and-verify-cli.md`, `docs/local-migration-stack.md`

**Content requirements:**

- Explain why stale target rows happen with upsert migration.
- Show artifact-only audit command.
- Show explicit cleanup command with `[REDACTED]` connection URL.
- State that cleanup deletes only IDs listed in `stale_target_ids` from an existing reconciliation report.
- Warn not to run cleanup against production without approved report review.
- Mention generated artifacts are `0600` but may contain IDs/scalars/metadata.
- Document `make smoke-target-reconciliation` as opt-in Docker smoke.
- Keep English and Chinese docs aligned.

---

## Task 9: Final Gates, Secret Scan, Review, Commit, Push

**Objective:** Verify and ship the phase.

**Commands:**

```bash
go test ./internal/migration -run 'Test.*TargetReconciliation|TestCleanupStaleTargetRecords|Test.*StaleDeleter' -v
go test ./cmd/vdbg -run 'TestParseReconcileTarget|TestRunReconcileTarget|TestParseCleanupTargetStale|TestRunCleanupTargetStale' -v
make smoke-target-reconciliation
make fmt
make lint
make test
make coverage-check
git diff --check
git diff --cached --check
make pre-commit
```

Secret scan:

```bash
git diff --cached | grep -E -i 'api[_-]?key|secret|password|token|credential|postgres://|postgresql://|Bearer' || true
git ls-files --others --exclude-standard -z | xargs -0 -r grep -nE -i 'api[_-]?key|secret|password|token|credential|postgres://|postgresql://|Bearer' || true
```

Expected: only documentation warnings, `[REDACTED]` examples, or intentional test marker strings. No real secrets.

Independent review:

- Request review focusing on destructive safety, SQL injection, artifact contract, report permissions, and docs consistency.
- Fix any must-fix issues before commit.

Commit:

```bash
git add ...
git commit -m "feat(migration): add target reconciliation cleanup"
git push
```

---

## Acceptance Criteria

- `vdbg reconcile-target` exists and is artifact-only/read-only.
- Reconciliation report deterministically classifies stale target, missing target, and changed records.
- Reconciliation report is written with `0600` permissions and no connection URL fields.
- `vdbg cleanup-target-stale` refuses to delete without `--confirm-delete-stale`.
- Cleanup deletes only `stale_target_ids` from a supplied reconciliation report.
- Cleanup does not delete missing/changed IDs.
- Cleanup output/report avoids connection URL leakage.
- Docker smoke proves stale detection, explicit deletion, and post-cleanup pass against local pgvector.
- English and Chinese docs are updated together.
- Full gates, coverage gate, pre-commit, secret scan, and independent review pass.

---

## Risks and Mitigations

- **Risk:** Accidental destructive deletion.
  **Mitigation:** Separate audit and cleanup commands; require `--confirm-delete-stale`; cleanup consumes explicit report; no automatic integration in this phase.

- **Risk:** SQL injection through table/column names.
  **Mitigation:** Reuse existing identifier validation/quoting strategy; parameterize row IDs; unit-test SQL builder.

- **Risk:** Stale report from wrong table/run.
  **Mitigation:** Report includes source/target endpoint metadata. CLI requires explicit target table. Future phase can add report fingerprint/checkpoint linkage.

- **Risk:** Artifact leakage of IDs/scalars/metadata.
  **Mitigation:** `0600` reports, docs warnings, secret scan gates, do not print row payloads.

- **Risk:** Docker smoke flakiness.
  **Mitigation:** Keep it opt-in, reuse existing migration stack checks, emit clear pass markers.
