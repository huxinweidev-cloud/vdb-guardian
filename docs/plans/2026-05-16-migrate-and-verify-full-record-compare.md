# Migrate-and-Verify Full-Record Compare Orchestration Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add `vdbg migrate-and-verify --full-record-compare` so the existing one-shot migration verifier can optionally build live source/target full-record artifacts and run `compare-full-records` as an additional equality gate.

**Architecture:** Keep full-record comparison additive and opt-in. `migrate-and-verify` remains the orchestrator; existing builder and comparer commands keep their standalone contracts. The orchestrator calls narrow `migrateAndVerifySteps` methods so unit tests use fakes and real Milvus/PostgreSQL are not required.

**Tech Stack:** Go CLI (`cmd/vdbg`), migration/reporting packages, existing JSON artifacts, Markdown report renderer, strict TDD.

---

## Existing state

Implemented:

- `vdbg migrate-and-verify` migrates records, builds fingerprint artifacts, compares fingerprints, and writes Markdown + diagnostic JSON reports.
- `vdbg build-milvus-record-artifact` and `vdbg build-pgvector-record-artifact` build live read-only full-record artifacts from a passing mapping artifact.
- `vdbg compare-full-records` compares source/target full-record artifacts locally and writes a `0600` report.

Missing:

- `migrate-and-verify` does not automatically call the full-record builders or comparer.
- Markdown and diagnostic JSON reports do not include full-record artifact paths or equality status.

## Safety boundaries

- `--full-record-compare` must be opt-in.
- It must require `--record-mapping`, because live full-record builders consume the mapping artifact.
- It must not print PostgreSQL connection URLs.
- Full-record diagnostic artifacts must be written before returning a gate failure.
- Existing migrate-and-verify behavior must be unchanged when the flag is absent.

## Task 1: RED tests for flag parsing and orchestration

**Files:**

- Modify: `cmd/vdbg/migrate_and_verify_test.go`

**Tests:**

1. `TestParseMigrateAndVerifyOptionsParsesFullRecordCompare`
   - Args include `--record-mapping /tmp/map.json --full-record-compare`.
   - Assert `options.FullRecordCompare == true`.

2. `TestParseMigrateAndVerifyOptionsRequiresRecordMappingForFullRecordCompare`
   - Args include `--full-record-compare` without `--record-mapping`.
   - Expect error containing `record-mapping`.

3. `TestRunMigrateAndVerifyRunsFullRecordCompareWhenEnabled`
   - Fake steps record call order:

```text
migrate, build-source, build-target, compare, build-full-source, build-full-target, compare-full-records
```

   - Assert result contains paths:

```text
<artifact-dir>/<job-id>-source-full-records.json
<artifact-dir>/<job-id>-target-full-records.json
<artifact-dir>/<job-id>-full-record-compare.json
```

4. `TestRunMigrateAndVerifySkipsFullRecordCompareByDefault`
   - Existing default call order remains unchanged.

Run:

```bash
go test ./cmd/vdbg -run 'TestParseMigrateAndVerifyOptions.*FullRecord|TestRunMigrateAndVerify.*FullRecord' -v
```

Expected: FAIL for missing fields/methods.

## Task 2: GREEN implementation for orchestration

**Files:**

- Modify: `cmd/vdbg/migrate_and_verify.go`

Implementation outline:

- Add `FullRecordCompare bool` to `migrateAndVerifyOptions`.
- Add paths to `migrateAndVerifyResult`:
  - `SourceFullRecordPath string`
  - `TargetFullRecordPath string`
  - `FullRecordComparePath string`
- Extend `migrateAndVerifySteps`:

```go
BuildSourceFullRecordArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error
BuildTargetFullRecordArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error
CompareFullRecords(ctx context.Context, sourcePath, targetPath, outputPath string) error
```

- Parse `--full-record-compare`.
- Validate: if `FullRecordCompare && RecordMappingPath == ""`, return error.
- After fingerprint comparison and before final reports/thresholds, if enabled:
  - build source full-record artifact;
  - build target full-record artifact;
  - run full-record comparer;
  - if comparer returns fail, keep artifacts/report and return error after reports are written if practical.

Real steps should call existing command helpers:

```go
runBuildMilvusRecordArtifactCommand(ctx, args)
runBuildPGVectorRecordArtifactCommand(ctx, args)
runCompareFullRecordsCommand(args)
```

Use `[REDACTED]` only in docs/tests; real args pass the configured URL but no success output should print it.

Run targeted tests until PASS.

## Task 3: Reporting integration

**Files:**

- Modify: `internal/reporting/markdown.go`
- Modify: `internal/reporting/markdown_test.go`
- Modify: `internal/reporting/diagnostic_json.go`
- Modify: `internal/reporting/diagnostic_json_test.go`
- Modify: `cmd/vdbg/migrate_and_verify.go`

Add optional full-record fields to `reporting.MigrateAndVerifyReport`:

```go
FullRecordCompareEnabled bool
SourceFullRecordPath     string
TargetFullRecordPath     string
FullRecordComparePath    string
```

Markdown should include a `## Full-record equality` section only when enabled.

Diagnostic JSON should include an optional object:

```json
"full_record_equality": {
  "enabled": true,
  "source_artifact": "...",
  "target_artifact": "...",
  "compare_report": "..."
}
```

When disabled, JSON may include `{"enabled": false}` or omit paths. Prefer stable object with `enabled`.

Run:

```bash
go test ./internal/reporting -run 'TestRenderMigrateAndVerify.*FullRecord|TestRenderMigrateAndVerify' -v
go test ./cmd/vdbg -run 'TestRunMigrateAndVerify.*FullRecord|TestRunMigrateAndVerifyWithInjectedSteps' -v
```

## Task 4: Docs and smoke updates

**Files:**

- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `docs/migrate-and-verify-cli.md` if present, otherwise relevant existing migrate docs.
- Modify: `docs/local-migration-stack.md`
- Modify: `CHANGELOG.md`

Document:

```bash
go run ./cmd/vdbg migrate-and-verify \
  ... \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --full-record-compare
```

Call out:

- opt-in;
- requires mapping artifact;
- writes source/target full-record artifacts and full-record compare report;
- exits non-zero if full-record equality fails, after preserving diagnostics.

## Task 5: Quality gates, review, commit, push

Run:

```bash
go test ./cmd/vdbg -run 'TestParseMigrateAndVerify|TestRunMigrateAndVerify' -v
go test ./internal/reporting -v
go run ./cmd/vdbg compare-full-records \
  --source testdata/migration/source-full-record-artifact.json \
  --target testdata/migration/target-full-record-artifact.json \
  --output /tmp/vdb-guardian-full-record-compare.json
stat -c '%a %n' /tmp/vdb-guardian-full-record-compare.json
make fmt && make lint && make test && git diff --check && git diff --cached --check && make pre-commit
```

Secret scan staged/tracked + untracked files. Then independent review, fix must-fix findings, commit, push.
