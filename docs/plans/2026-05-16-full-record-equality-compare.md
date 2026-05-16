# Full-Record Equality Compare Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add deterministic full-record artifact comparison so a Milvus→pgvector migration can prove scalar fields, dynamic metadata, partition metadata, and vector fingerprints are equal after mapping-driven execution.

**Architecture:** Keep Phase 10 artifact-only first: introduce an internal `migration` full-record artifact/report model and `CompareFullRecordArtifacts` function, then expose it through a new `vdbg compare-full-records` CLI that reads two local JSON artifacts and writes a `0600` machine-readable report. Real database artifact builders and `migrate-and-verify` auto-wiring stay as follow-up work unless the artifact-only contract is green and reviewed.

**Tech Stack:** Go standard library (`encoding/json`, `crypto/sha256`, `os.WriteFile 0600`), existing `cmd/vdbg` flag patterns, existing `internal/migration` mapping models, repository `make` quality gates.

---

## Current state

- Phase 9 implemented mapping-driven full-record migration execution through `vdbg migrate --record-mapping` and `vdbg migrate-and-verify --record-mapping`.
- Existing fingerprint artifact compare is search-behavior oriented through `vdbg compare-artifacts` and Python engine metrics.
- Existing schema/migration commands already use local JSON artifacts and `0600` output permissions.
- Repository rule `CLAUDE.md` requires plan-before-code, strict TDD, docs sync, secret hygiene, and quality gates.

## Phase 10 scope

In scope:

1. Define stable full-record artifact JSON schema.
2. Define stable full-record compare report JSON schema.
3. Implement deterministic artifact-only comparison.
4. Add `vdbg compare-full-records --source --target --output`.
5. Cover missing/extra rows, scalar mismatch, dynamic metadata mismatch, partition mismatch, vector hash mismatch, and summary counts.
6. Update English and Chinese docs/README/CHANGELOG.

Out of scope for this first Phase 10 slice:

- Live Milvus full-record artifact builder.
- Live pgvector full-record artifact builder.
- Automatic `migrate-and-verify --full-record-compare` orchestration.
- Checkpoint/resume/retry/idempotency.

These become Phase 10b / Phase 11 after the artifact contract is stable.

## Artifact schema proposal

`FullRecordArtifact`:

```json
{
  "schema_version": "v1",
  "system": "milvus",
  "collection": "items",
  "record_mapping_path": "/tmp/vdb-guardian-record-mapping.json",
  "records": [
    {
      "id": "sku-1",
      "vector_hash": "sha256:...",
      "vector_dimension": 8,
      "scalars": {"title": "First", "price": 9.5},
      "dynamic_metadata": {"brand": "acme", "tags": ["sale"]},
      "partition": "tenant_a"
    }
  ]
}
```

`FullRecordCompareReport`:

```json
{
  "schema_version": "v1",
  "status": "pass",
  "source": {"system": "milvus", "collection": "items", "record_count": 100},
  "target": {"system": "pgvector", "collection": "items", "record_count": 100},
  "summary": {
    "matched_records": 100,
    "missing_source_records": 0,
    "missing_target_records": 0,
    "mismatched_records": 0,
    "scalar_mismatches": 0,
    "dynamic_metadata_mismatches": 0,
    "partition_mismatches": 0,
    "vector_mismatches": 0
  },
  "missing_source_ids": [],
  "missing_target_ids": [],
  "mismatches": []
}
```

Status semantics:

- `pass`: no missing/extra rows and no mismatches.
- `fail`: any missing row or field/vector mismatch.

## Task 1: Add full-record artifact and report tests

**Objective:** Lock the JSON schema and comparison behavior before implementation.

**Files:**

- Create: `internal/migration/full_record_compare_test.go`
- Later modify: `internal/migration/full_record_compare.go`

**Step 1: Write failing tests**

Add tests for:

1. `TestCompareFullRecordArtifactsPassesForEqualRecords`
2. `TestCompareFullRecordArtifactsReportsMissingAndExtraRows`
3. `TestCompareFullRecordArtifactsReportsFieldMismatches`
4. `TestCompareFullRecordArtifactsRejectsDuplicateIDs`
5. `TestMarshalFullRecordCompareReportKeepsStableJSONShape`

Expected assertions:

- pass report has `status == "pass"` and sorted deterministic IDs.
- missing target source ID is reported as `missing_target_ids`.
- target-only ID is reported as `missing_source_ids`.
- mismatches include `field_path` values such as `scalars.title`, `dynamic_metadata.brand`, `partition`, `vector_hash`, `vector_dimension`.
- duplicate IDs return an error before comparison.
- JSON unmarshal confirms `schema_version`, `summary`, `mismatches` field names.

**Step 2: Run RED**

```bash
go test ./internal/migration -run 'TestCompareFullRecordArtifacts|TestMarshalFullRecordCompareReport' -v
```

Expected: FAIL because types/functions do not exist.

## Task 2: Implement internal full-record comparison

**Objective:** Add minimal production code to pass Task 1.

**Files:**

- Create: `internal/migration/full_record_compare.go`

**Implementation outline:**

- Exported constants:
  - `FullRecordArtifactVersion = "v1"`
  - `FullRecordCompareReportVersion = "v1"`
  - `FullRecordCompareStatusPass = "pass"`
  - `FullRecordCompareStatusFail = "fail"`
- Exported structs with Go doc comments:
  - `FullRecordArtifact`
  - `FullRecordArtifactRecord`
  - `FullRecordCompareReport`
  - `FullRecordCompareSummary`
  - `FullRecordMismatch`
- Function:
  - `CompareFullRecordArtifacts(source, target FullRecordArtifact) (FullRecordCompareReport, error)`
- Function:
  - `MarshalFullRecordCompareReport(report FullRecordCompareReport) ([]byte, error)`

Comparison rules:

- Validate schema version equals `v1`.
- Validate no duplicate IDs.
- Sort IDs and mismatch output for deterministic reports.
- Use `reflect.DeepEqual` or normalized JSON for scalar/dynamic values.
- Treat missing key and explicit `null` as different unless both JSON-normalize to the same value.
- Compare vector hash and vector dimension directly.

**Step 2: Run GREEN**

```bash
go test ./internal/migration -run 'TestCompareFullRecordArtifacts|TestMarshalFullRecordCompareReport' -v
```

Expected: PASS.

## Task 3: Add CLI parse/orchestration tests

**Objective:** Prove `vdbg compare-full-records` validates flags, writes `0600` report JSON, and does not require DB connections.

**Files:**

- Create: `cmd/vdbg/compare_full_records_test.go`
- Later create: `cmd/vdbg/compare_full_records.go`
- Modify: `cmd/vdbg/main.go`

**Step 1: Write failing tests**

Add tests for:

1. `TestParseCompareFullRecordsOptionsRequiresPaths`
2. `TestRunCompareFullRecordsWritesReport0600`
3. `TestRunCompareFullRecordsReturnsErrorOnMismatch`

Use temp files with minimal source/target artifacts. Verify:

- output JSON is valid and has `status`.
- output file mode is `0600`.
- command returns non-nil error on report `status: fail` after writing diagnostic output.

**Step 2: Run RED**

```bash
go test ./cmd/vdbg -run 'TestParseCompareFullRecords|TestRunCompareFullRecords' -v
```

Expected: FAIL because CLI functions do not exist.

## Task 4: Implement `vdbg compare-full-records`

**Objective:** Add the artifact-only CLI command.

**Files:**

- Create: `cmd/vdbg/compare_full_records.go`
- Modify: `cmd/vdbg/main.go`

**Implementation outline:**

Flags:

```bash
--source /tmp/source-records.json
--target /tmp/target-records.json
--output /tmp/full-record-compare-report.json
```

Behavior:

1. Parse and validate required paths.
2. Read source/target JSON.
3. Decode into `migration.FullRecordArtifact`.
4. Run `migration.CompareFullRecordArtifacts`.
5. Marshal report using `migration.MarshalFullRecordCompareReport`.
6. Create parent output directory.
7. Write output with `0600`.
8. Print concise summary:

```text
full-record comparison completed
status: pass
source_records: 100
target_records: 100
mismatched_records: 0
result: /tmp/full-record-compare-report.json
```

9. If status is `fail`, still write report and return error such as `full-record comparison failed`.
10. Never print DB connection strings because this command accepts only local file paths.

**Step 2: Run GREEN**

```bash
go test ./cmd/vdbg -run 'TestParseCompareFullRecords|TestRunCompareFullRecords' -v
```

Expected: PASS.

## Task 5: Add synthetic full-record example fixture

**Objective:** Provide small deterministic artifact samples for docs and future local smoke tests.

**Files:**

- Create: `testdata/migration/source-full-record-artifact.json`
- Create: `testdata/migration/target-full-record-artifact.json`

Content should include:

- renamed scalar already normalized to target-side logical field name;
- string/int/float/bool/null scalar values;
- nested dynamic metadata;
- `tenant_a` partition;
- vector hash and dimension.

Run:

```bash
go run ./cmd/vdbg compare-full-records \
  --source testdata/migration/source-full-record-artifact.json \
  --target testdata/migration/target-full-record-artifact.json \
  --output /tmp/vdb-guardian-full-record-compare.json
```

Expected: status pass and output file mode `0600`.

## Task 6: Documentation sync

**Objective:** Document the artifact-only comparison workflow in both English and Chinese docs.

**Files:**

- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `CHANGELOG.md`
- Create or modify: `docs/full-record-compare-cli.md`
- Create or modify: `docs/zh-CN/full-record-compare-cli.md`
- Modify: `docs/local-migration-stack.md`

Docs must include:

- command syntax;
- schema summary;
- pass/fail semantics;
- relation to Phase 9 `migrate --record-mapping`;
- explicit statement that live artifact builders are follow-up if not implemented in this slice;
- `[REDACTED]` for any connection examples.

## Task 7: Full verification and commit/push

**Objective:** Prove repo quality and publish branch.

Run:

```bash
make fmt && make lint && make test && git diff --check && make pre-commit
```

Secret scan:

```bash
git diff --cached | grep -Ei '(api[_-]?key|token|secret|password|BEGIN (RSA|OPENSSH|PRIVATE)|postgres(ql)?://[^[:space:]]+:[^[:space:]@]+@)' || true
git ls-files --others --exclude-standard -z | xargs -0 -r grep -nEi '(api_key|secret|password|token|credential|client_secret|private key|BEGIN .*PRIVATE|Authorization:|Bearer |postgres://|postgresql://|redis://|mysql://|sk-|ghp_|github_pat_|AKIA|AIza|ya29\.)' || true
```

Commit plan:

```bash
git add docs/plans/2026-05-16-full-record-equality-compare.md
git commit -m "docs: add full-record equality compare plan"

git add internal/migration/full_record_compare.go internal/migration/full_record_compare_test.go
git commit -m "feat(migration): compare full-record artifacts"

git add cmd/vdbg/compare_full_records.go cmd/vdbg/compare_full_records_test.go cmd/vdbg/main.go
git commit -m "feat(cli): add compare-full-records command"

git add README.md README.zh-CN.md CHANGELOG.md docs/full-record-compare-cli.md docs/zh-CN/full-record-compare-cli.md docs/local-migration-stack.md testdata/migration/*full-record-artifact.json
git commit -m "docs: document full-record comparison workflow"

git push origin feat/enterprise-scaffold
```

## Risks and mitigations

- **JSON type drift:** use deterministic normalization and tests for int/float/null behavior.
- **Large vector payloads:** compare hashes/dimensions, not raw vector arrays in the report.
- **No live artifact builders yet:** document clearly that this slice compares existing artifacts only.
- **False success from missing fields:** treat missing vs explicit null as mismatch unless JSON-normalized equal is intentionally supported by tests.
- **Secret leakage:** command only accepts local artifact paths; docs use `[REDACTED]`; run secret scan before push.

## Acceptance criteria

- `go test ./internal/migration -run 'TestCompareFullRecordArtifacts|TestMarshalFullRecordCompareReport' -v` passes.
- `go test ./cmd/vdbg -run 'TestParseCompareFullRecords|TestRunCompareFullRecords' -v` passes.
- `go run ./cmd/vdbg compare-full-records ...` works with testdata and writes `0600` output.
- Full repo gate passes.
- Secret scan reports no real credentials.
- Branch `feat/enterprise-scaffold` is pushed.
