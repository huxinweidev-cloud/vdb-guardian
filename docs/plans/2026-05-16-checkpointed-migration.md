# Checkpointed Milvus to pgvector Migration Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add a production-safety checkpoint/resume MVP for Milvus → pgvector migration so interrupted `vdbg migrate` runs can preserve batch progress and resume safely without leaking credentials.

**Architecture:** Introduce an additive checkpoint artifact contract in `internal/migration` and extend the existing migration runner to write batch-level progress through an injected checkpoint store. Keep database logic behind the existing source/target boundaries; checkpoint artifacts store only non-secret migration identity, schema/mapping fingerprints, batch counters, and failure diagnostics. Wire CLI flags into `vdbg migrate` first, then into `migrate-and-verify` after the standalone migration path is green.

**Tech Stack:** Go CLI/control plane, `encoding/json`, restrictive `0600` artifact writes, existing Milvus/pgvector migration adapters, existing Makefile/pre-commit gates.

---

## Current context

Implemented today:

- `vdbg migrate` can run real Milvus → pgvector migration with optional schema preflight and `--record-mapping`.
- `vdbg migrate-and-verify` composes migration, fingerprint compare, reports, and optional `--full-record-compare`.
- `VectorMigrationRunner.Migrate` currently reads all records into memory and writes once; `BatchSize` is validated/defaulted but not used for checkpointed batch execution.
- Reports are written with `0600` and examples redact connection URLs.

Repository rules from `CLAUDE.md`:

- Plan before complex coding; wait for owner approval before large-scale implementation.
- Strict TDD: write failing tests, verify RED, then implement minimal code.
- Go exported APIs need doc comments.
- Run formatting, tests, lint, pre-commit before commit.
- Never commit real credentials, tokens, passwords, or connection strings.

## Non-goals for this phase

- Do not implement high-performance bulk import/COPY.
- Do not implement stale-row cleanup/reconciliation.
- Do not add API routes or persistent DB-backed job storage.
- Do not change full-record equality semantics.
- Do not store raw vectors, credentials, or database connection URLs in checkpoint artifacts.

## Artifact contract proposal

Create a stable JSON artifact, written with `0600`:

```json
{
  "schema_version": "v1",
  "job_id": "migration-smoke",
  "status": "running|completed|failed",
  "source": {"type": "milvus", "collection": "items"},
  "target": {"type": "pgvector", "table": "items"},
  "dimension": 8,
  "batch_size": 100,
  "records_read": 250,
  "records_written": 200,
  "completed_batches": [
    {"index": 0, "start": 0, "end": 100, "records_written": 100},
    {"index": 1, "start": 100, "end": 200, "records_written": 100}
  ],
  "failed_batches": [
    {"index": 2, "start": 200, "end": 250, "error": "write target records: ..."}
  ],
  "resume": {
    "next_batch_index": 2,
    "next_record_offset": 200,
    "record_mapping_path": "/tmp/vdb-guardian-record-mapping.json",
    "schema_plan_path": "/tmp/vdb-guardian-pgvector-schema-plan.json",
    "mapping_fingerprint": "sha256:...",
    "schema_plan_fingerprint": "sha256:..."
  }
}
```

Notes:

- The initial MVP can use in-memory slicing after source read; that still provides write-batch checkpointing and failure recovery for target write failures.
- Future phases can replace the source boundary with cursor/page reads while keeping the same artifact schema.
- Fingerprints should hash local artifact bytes, never connection URLs.

---

## Task 1: Add checkpoint artifact model and writer tests

**Objective:** Define a machine-readable checkpoint artifact contract and write/read helpers with safe permissions.

**Files:**

- Create: `internal/migration/checkpoint.go`
- Create: `internal/migration/checkpoint_test.go`

**Step 1: Write failing tests**

Add tests for:

- `BuildVectorMigrationCheckpoint` creates schema version `v1`, status, source/target, counters, completed/failed batch entries, and resume info.
- `WriteVectorMigrationCheckpoint` writes `0600` JSON.
- checkpoint JSON does not contain a supplied connection URL-like string when only artifact paths/fingerprints are provided.

Expected command:

```bash
go test ./internal/migration -run 'Test.*Checkpoint' -v
```

Expected RED: compile failure because checkpoint types/functions do not exist.

**Step 2: Implement minimal model**

Add exported structs with Go doc comments:

- `VectorMigrationCheckpoint`
- `VectorMigrationCheckpointEndpoint`
- `VectorMigrationCheckpointBatch`
- `VectorMigrationCheckpointResume`
- `WriteVectorMigrationCheckpoint(path string, checkpoint VectorMigrationCheckpoint) error`

Use `os.WriteFile(path, data, 0o600)` and create parent dirs if needed.

**Step 3: Verify GREEN**

Run:

```bash
gofmt -w internal/migration/checkpoint.go internal/migration/checkpoint_test.go
go test ./internal/migration -run 'Test.*Checkpoint' -v
```

Expected: PASS.

---

## Task 2: Add batch-level checkpointing to `VectorMigrationRunner`

**Objective:** Make the runner use `BatchSize` and write progress after each successful batch.

**Files:**

- Modify: `internal/migration/vector_migration.go`
- Modify: `internal/migration/vector_migration_test.go`

**Step 1: Write failing tests**

Add tests for:

- `TestVectorMigrationRunnerWritesBatchesAndCheckpoints`: five records, batch size two, target receives three `WriteRecords` calls, checkpoint store receives completed batch entries after each successful batch.
- `TestVectorMigrationRunnerWritesFailedCheckpointBeforeReturningWriteError`: second batch write fails; result returns error and checkpoint includes completed first batch plus failed second batch.
- `TestVectorMigrationRunnerResumeSkipsCompletedRecords`: given resume offset 2 and five source records, runner writes only records `[2:]`, preserving total records read and next offset.

Expected RED: no checkpoint config/store exists and runner writes once.

**Step 2: Minimal design**

Extend `VectorMigrationConfig` additively:

```go
CheckpointPath string
ResumeFromPath string
JobID string
SchemaPlanPath string
RecordMappingPath string
SchemaPlanFingerprint string
RecordMappingFingerprint string
```

Add an unexported interface:

```go
type vectorMigrationCheckpointStore interface {
    Save(ctx context.Context, checkpoint VectorMigrationCheckpoint) error
}
```

Provide no-op store when `CheckpointPath == ""`.

Add `NewVectorMigrationRunnerWithCheckpointStore(...)` for tests, or keep constructor and inject via option/helper if cleaner.

**Step 3: Batch execution**

Change `Migrate` to:

1. Read and validate all source records.
2. Determine resume offset if configured.
3. Loop in `BatchSize` chunks.
4. Copy and write each batch.
5. After each success, save running checkpoint.
6. On failure, save failed checkpoint, then return wrapped error.
7. At end, save completed checkpoint and return counts.

Keep old behavior when no checkpoint path is set, except `BatchSize` now controls write batches.

**Step 4: Verify GREEN**

Run:

```bash
gofmt -w internal/migration/vector_migration.go internal/migration/vector_migration_test.go
go test ./internal/migration -run 'TestVectorMigrationRunner.*Checkpoint|TestVectorMigrationRunnerResume|TestVectorMigrationRunnerMigrates' -v
```

Expected: PASS.

---

## Task 3: Add checkpoint/resume validation and artifact fingerprinting

**Objective:** Prevent unsafe resume when schema/mapping/source/target/dimension/batch size differs.

**Files:**

- Modify: `internal/migration/checkpoint.go`
- Modify: `internal/migration/checkpoint_test.go`

**Step 1: Write failing tests**

Add tests for:

- load checkpoint from JSON and validate it matches current config.
- reject mismatched source collection.
- reject mismatched target table.
- reject mismatched dimension.
- reject mismatched mapping fingerprint.
- allow resume from a `running` or `failed` checkpoint but reject `completed` unless an explicit future flag exists; this phase should not add force rerun.

Expected RED: validation functions missing.

**Step 2: Implement helpers**

Add:

```go
func ReadVectorMigrationCheckpoint(path string) (VectorMigrationCheckpoint, error)
func ValidateVectorMigrationResume(checkpoint VectorMigrationCheckpoint, expected VectorMigrationResumeExpectation) error
func FileSHA256Fingerprint(path string) (string, error)
```

`VectorMigrationResumeExpectation` should contain only non-secret fields and local artifact fingerprints.

**Step 3: Verify GREEN**

Run:

```bash
gofmt -w internal/migration/checkpoint.go internal/migration/checkpoint_test.go
go test ./internal/migration -run 'Test.*Checkpoint|Test.*Resume|Test.*Fingerprint' -v
```

Expected: PASS.

---

## Task 4: Wire `vdbg migrate` CLI flags

**Objective:** Expose checkpoint/resume through the standalone migration CLI.

**Files:**

- Modify: `cmd/vdbg/migrate.go`
- Modify: `cmd/vdbg/migrate_test.go`

**Step 1: Write failing tests**

Add tests for parse and orchestration:

- `--checkpoint-path` populates `MigrationConfig.CheckpointPath`.
- `--resume-from` populates `MigrationConfig.ResumeFromPath`.
- `--checkpoint-path` report path is printed/written without connection URL leakage.
- Resume with `--record-mapping` computes mapping fingerprint from artifact bytes and passes it into config.
- Missing resume file returns before factory/runner creation.

Expected RED: flags missing.

**Step 2: Implement CLI wiring**

Add flags:

```bash
--checkpoint-path /tmp/vdb-guardian-checkpoint.json
--resume-from /tmp/vdb-guardian-checkpoint.json
```

Use `--checkpoint-path` for ongoing writes. If omitted and `--resume-from` is set, default checkpoint write path to the resume file unless tests reveal a safer explicit behavior.

Compute local fingerprints for:

- `--schema-plan`, if present
- `--record-mapping`, if present

Never fingerprint connection URLs.

**Step 3: stdout/reporting**

On success, print:

```text
checkpoint: /tmp/vdb-guardian-checkpoint.json
resumed_from: /tmp/vdb-guardian-checkpoint.json
```

only when relevant.

**Step 4: Verify GREEN**

Run:

```bash
gofmt -w cmd/vdbg/migrate.go cmd/vdbg/migrate_test.go
go test ./cmd/vdbg -run 'TestParseMigrate|TestRunMigrate' -v
```

Expected: PASS.

---

## Task 5: Extend migration report with checkpoint summary

**Objective:** Make migration JSON reports audit checkpoint/resume state.

**Files:**

- Modify: `internal/migration/vector_migration_report.go`
- Modify: `internal/migration/vector_migration_test.go`
- Modify: `cmd/vdbg/migrate.go` if report options need checkpoint fields

**Step 1: Write failing tests**

Extend `TestBuildVectorMigrationReport` to assert optional checkpoint fields:

```json
"checkpoint": {
  "path": "/tmp/vdb-guardian-checkpoint.json",
  "resume_from": "/tmp/vdb-guardian-checkpoint.json",
  "completed_batches": 3,
  "failed_batches": 0,
  "next_record_offset": 300
}
```

Expected RED: report has no checkpoint section.

**Step 2: Implement additive report section**

Add optional `checkpoint,omitempty` to `VectorMigrationReport`.

Populate from `VectorMigrationResult` and/or report options. Keep it additive and backward-compatible.

**Step 3: Verify GREEN**

Run:

```bash
gofmt -w internal/migration/vector_migration_report.go internal/migration/vector_migration_test.go cmd/vdbg/migrate.go
go test ./internal/migration ./cmd/vdbg -run 'TestBuildVectorMigrationReport|TestRunMigrate' -v
```

Expected: PASS.

---

## Task 6: Wire `migrate-and-verify` pass-through flags

**Objective:** Let the composed command use checkpointed migration without changing compare semantics.

**Files:**

- Modify: `cmd/vdbg/migrate_and_verify.go`
- Modify: `cmd/vdbg/migrate_and_verify_test.go`
- Modify: `internal/reporting/diagnostic_json.go`
- Modify: `internal/reporting/markdown.go`
- Modify tests in `internal/reporting/*_test.go`

**Step 1: Write failing tests**

Add parse/orchestration tests:

- `migrate-and-verify --checkpoint-path` passes path to migration step.
- `--resume-from` passes resume path.
- Markdown/diagnostic JSON include checkpoint path/resume path when set.
- `--full-record-compare` behavior remains unchanged.

Expected RED: flags/fields missing.

**Step 2: Implement additive pass-through**

Add fields to `migrateAndVerifyOptions` and step call. Do not alter fingerprint/full-record compare gates.

**Step 3: Verify GREEN**

Run:

```bash
gofmt -w cmd/vdbg/migrate_and_verify.go cmd/vdbg/migrate_and_verify_test.go internal/reporting/*.go
go test ./cmd/vdbg -run 'TestParseMigrateAndVerify|TestRunMigrateAndVerify' -v
go test ./internal/reporting -v
```

Expected: PASS.

---

## Task 7: Documentation sync

**Objective:** Document checkpoint/resume safely in English and Chinese docs.

**Files:**

- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/migrate-cli.md`
- Modify: `docs/migrate-and-verify-cli.md`
- Modify: `docs/zh-CN/migrate-and-verify-cli.md`
- Create or modify: `docs/checkpointed-migration.md`
- Create or modify: `docs/zh-CN/checkpointed-migration.md`

**Content requirements:**

- Explain checkpoint JSON contains no secrets and is written `0600`.
- Show redacted examples only.
- Explain resume safety checks and rejected mismatches.
- Explain current limitation: source read is still all-at-once for this MVP; checkpointing protects batch writes and resume offset, not source-side cursor streaming yet.
- Explain relationship to `migrate-and-verify --full-record-compare`.

**Verify:**

```bash
git diff --check
```

---

## Task 8: Targeted smoke and quality gates

**Objective:** Verify the checkpoint feature without requiring live secrets.

**Commands:**

```bash
go test ./internal/migration -run 'Test.*Checkpoint|Test.*Resume|TestVectorMigrationRunner' -v
go test ./cmd/vdbg -run 'TestParseMigrate|TestRunMigrate|TestParseMigrateAndVerify|TestRunMigrateAndVerify' -v
go test ./internal/reporting -v
make fmt && make lint && make test && git diff --check && git diff --cached --check && make pre-commit
```

**Secret scan:**

Scan staged/tracked/untracked diff for:

```text
api_key, secret, password, passwd, token, credential, connection string, postgres://, Bearer ...
```

Real secrets must not appear. Placeholder `[REDACTED]` is allowed in docs.

---

## Task 9: Independent review, commit, push

**Objective:** Ensure the feature is safe to merge.

**Review requirements:**

- Logic review: resume safety, checkpoint status transitions, failed batch behavior.
- Security review: checkpoint/report does not leak URLs/secrets; artifacts `0600`.
- Test review: RED/GREEN evidence, failure paths, report schema.
- Docs review: English/Chinese sync.

**Commit plan:**

1. Commit plan first:

```bash
git add docs/plans/2026-05-16-checkpointed-migration.md
git commit -m "docs: add checkpointed migration plan"
```

2. Commit implementation after gates:

```bash
git add ...
git commit -m "feat(migration): add checkpointed migration resume"
git push origin feat/enterprise-scaffold
```

---

## Risks and mitigations

- **Risk:** Runner currently reads all source records before writing.
  **Mitigation:** Document as MVP limitation; design checkpoint schema around batch offsets so future source cursor support is compatible.

- **Risk:** Resume could run against changed mapping/schema.
  **Mitigation:** Hash local schema/mapping artifacts and validate source/target/dimension/batch size before writing.

- **Risk:** Checkpoint files could leak operational metadata.
  **Mitigation:** No credentials, no connection URLs, `0600`, docs warn to keep artifacts secured.

- **Risk:** Failed checkpoint write after successful DB write could leave progress ambiguous.
  **Mitigation:** Return checkpoint write error immediately; docs recommend rerunning with resume validation and pgvector upsert semantics.

---

## Approval gate

Per `CLAUDE.md`, this plan must be reviewed/approved before implementation begins. After approval, execute via strict TDD with targeted tests first and full gate before commit/push.
