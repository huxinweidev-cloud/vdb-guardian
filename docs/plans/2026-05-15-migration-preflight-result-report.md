# Migration Preflight and Result Report Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add Phase 7 safeguards to `vdbg migrate`: optional schema preflight using existing planned-vs-live artifacts, plus a machine-readable migration result report artifact.

**Architecture:** Keep migration itself single-purpose and CLI-first. Reuse the already implemented `CompareAppliedPGVectorSchema` gate by loading `--schema-plan` and `--live-schema` JSON only when `--require-schema-match` is set, then run the existing migration runner and emit a JSON report to stdout or `--output`. This does not add checkpointing, metadata migration, partition migration, or new database queries.

**Tech Stack:** Go CLI under `cmd/vdbg`, migration core under `internal/migration`, schema artifact comparison under `internal/schema`, JSON artifacts with `0600` permissions, tests using fake runners only.

---

## Context

The schema safety chain is now implemented:

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
  -> inspect-pgvector-schema
  -> compare-applied-schema
```

The next safest development step is not full checkpoint/resume yet. The current `vdbg migrate` can already run standalone real record transfer, but it only prints text summary and does not enforce that the schema drift gate passed. This plan adds a small production-safety increment:

```text
compare-applied-schema artifacts
  -> migrate --require-schema-match
  -> migration-result JSON report
```

This makes standalone migration safer while keeping all stages independently executable.

## Explicit non-goals

Do **not** implement in this phase:

- checkpoint/resume;
- batch-level retry;
- production bulk import;
- metadata/dynamic-field payload migration;
- Milvus partition payload migration;
- cleanup/rollback policy;
- new PostgreSQL or Milvus queries for preflight.

Those remain future data migration hardening phases.

---

## Task 1: Add migration result report model

**Objective:** Add a stable JSON report type for standalone `vdbg migrate` runs.

**Files:**
- Modify: `internal/migration/vector_migration.go`
- Test: `internal/migration/vector_migration_test.go`

**Step 1: Write failing test**

Add a test that asserts a report can be built from a successful migration result:

```go
func TestBuildVectorMigrationReport(t *testing.T) {
	result := VectorMigrationResult{
		SourceCollection: "items",
		TargetTable:      "items",
		Dimension:        8,
		RecordsRead:      100,
		RecordsWritten:   100,
	}
	report := BuildVectorMigrationReport(result, VectorMigrationReportOptions{
		JobID:             "migration-smoke",
		SchemaPreflight:   true,
		SchemaComparePath: "/tmp/schema-compare.json",
	})

	if report.SchemaVersion != VectorMigrationReportVersion {
		t.Fatalf("unexpected schema version: %s", report.SchemaVersion)
	}
	if report.Status != "completed" {
		t.Fatalf("unexpected status: %s", report.Status)
	}
	if report.Summary.RecordsRead != 100 || report.Summary.RecordsWritten != 100 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	if !report.Preflight.SchemaMatchRequired || report.Preflight.SchemaCompareStatus != "pass" {
		t.Fatalf("unexpected preflight: %+v", report.Preflight)
	}
}
```

**Step 2: Run test to verify failure**

```bash
go test ./internal/migration -run 'TestBuildVectorMigrationReport' -v
```

Expected: FAIL because the report type and builder do not exist.

**Step 3: Implement minimal report structs**

Add to `internal/migration/vector_migration.go` or a new small file `internal/migration/vector_migration_report.go`:

```go
const VectorMigrationReportVersion = "v1"

const (
	VectorMigrationReportStatusCompleted = "completed"
	VectorMigrationReportStatusFailed    = "failed"
)

type VectorMigrationReportOptions struct {
	JobID             string
	SchemaPreflight   bool
	SchemaComparePath string
}

type VectorMigrationReport struct {
	SchemaVersion string                         `json:"schema_version"`
	JobID         string                         `json:"job_id,omitempty"`
	Status        string                         `json:"status"`
	Source        VectorMigrationReportEndpoint  `json:"source"`
	Target        VectorMigrationReportEndpoint  `json:"target"`
	Preflight     VectorMigrationReportPreflight `json:"preflight"`
	Summary       VectorMigrationReportSummary   `json:"summary"`
}

type VectorMigrationReportEndpoint struct {
	Type       string `json:"type"`
	Collection string `json:"collection,omitempty"`
	Table      string `json:"table,omitempty"`
}

type VectorMigrationReportPreflight struct {
	SchemaMatchRequired bool   `json:"schema_match_required"`
	SchemaComparePath   string `json:"schema_compare_path,omitempty"`
	SchemaCompareStatus string `json:"schema_compare_status"`
}

type VectorMigrationReportSummary struct {
	Dimension      int `json:"dimension"`
	RecordsRead    int `json:"records_read"`
	RecordsWritten int `json:"records_written"`
}

func BuildVectorMigrationReport(result VectorMigrationResult, options VectorMigrationReportOptions) VectorMigrationReport {
	status := "skipped"
	if options.SchemaPreflight {
		status = "pass"
	}
	return VectorMigrationReport{
		SchemaVersion: VectorMigrationReportVersion,
		JobID:         options.JobID,
		Status:        VectorMigrationReportStatusCompleted,
		Source: VectorMigrationReportEndpoint{
			Type:       "milvus",
			Collection: result.SourceCollection,
		},
		Target: VectorMigrationReportEndpoint{
			Type:  "pgvector",
			Table: result.TargetTable,
		},
		Preflight: VectorMigrationReportPreflight{
			SchemaMatchRequired: options.SchemaPreflight,
			SchemaComparePath:   options.SchemaComparePath,
			SchemaCompareStatus: status,
		},
		Summary: VectorMigrationReportSummary{
			Dimension:      result.Dimension,
			RecordsRead:    result.RecordsRead,
			RecordsWritten: result.RecordsWritten,
		},
	}
}
```

**Step 4: Run test to verify pass**

```bash
go test ./internal/migration -run 'TestBuildVectorMigrationReport' -v
```

Expected: PASS.

---

## Task 2: Add schema preflight helper for migrate CLI

**Objective:** Reuse the existing planned-vs-live schema comparison before migration when explicitly requested.

**Files:**
- Modify: `cmd/vdbg/migrate.go`
- Test: `cmd/vdbg/migrate_test.go`

**Step 1: Write failing tests**

Add tests for option parsing:

```go
func TestParseMigrateOptionsWithSchemaPreflightAndOutput(t *testing.T) {
	options, err := parseMigrateOptions([]string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
		"--require-schema-match",
		"--schema-plan", "/tmp/schema-plan.json",
		"--live-schema", "/tmp/live-schema.json",
		"--output", "/tmp/migration-report.json",
		"--job-id", "migration-smoke",
	})
	if err != nil {
		t.Fatalf("parseMigrateOptions returned error: %v", err)
	}
	if !options.RequireSchemaMatch {
		t.Fatal("expected schema match preflight to be required")
	}
	if options.SchemaPlanPath != "/tmp/schema-plan.json" || options.LiveSchemaPath != "/tmp/live-schema.json" {
		t.Fatalf("unexpected schema paths: %+v", options)
	}
	if options.OutputPath != "/tmp/migration-report.json" || options.JobID != "migration-smoke" {
		t.Fatalf("unexpected report options: %+v", options)
	}
}

func TestParseMigrateOptionsRejectsIncompleteSchemaPreflight(t *testing.T) {
	_, err := parseMigrateOptions([]string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
		"--require-schema-match",
		"--schema-plan", "/tmp/schema-plan.json",
	})
	if err == nil || !strings.Contains(err.Error(), "live-schema") {
		t.Fatalf("expected live-schema error, got %v", err)
	}
}
```

**Step 2: Run test to verify failure**

```bash
go test ./cmd/vdbg -run 'TestParseMigrateOptionsWithSchemaPreflight|TestParseMigrateOptionsRejectsIncompleteSchemaPreflight' -v
```

Expected: FAIL because fields/flags are missing.

**Step 3: Implement fields and parsing**

Extend `migrateOptions`:

```go
RequireSchemaMatch bool
SchemaPlanPath     string
LiveSchemaPath     string
OutputPath         string
JobID              string
```

Add flags:

```go
flagSet.BoolVar(&requireSchemaMatch, "require-schema-match", false, "require planned-vs-live schema match before migration")
flagSet.StringVar(&schemaPlanPath, "schema-plan", "", "path to pgvector schema plan JSON")
flagSet.StringVar(&liveSchemaPath, "live-schema", "", "path to live pgvector schema inspection JSON")
flagSet.StringVar(&outputPath, "output", "", "optional migration result report JSON output path")
flagSet.StringVar(&jobID, "job-id", "", "optional job id for the migration report")
```

Validation:

```go
if requireSchemaMatch && schemaPlanPath == "" {
	return migrateOptions{}, errors.New("schema-plan is required when require-schema-match is set")
}
if requireSchemaMatch && liveSchemaPath == "" {
	return migrateOptions{}, errors.New("live-schema is required when require-schema-match is set")
}
```

**Step 4: Run tests**

```bash
go test ./cmd/vdbg -run 'TestParseMigrateOptions' -v
```

Expected: PASS.

---

## Task 3: Execute preflight and block migration on schema drift

**Objective:** Ensure `vdbg migrate --require-schema-match` refuses to run the migration if planned-vs-live schema comparison fails.

**Files:**
- Modify: `cmd/vdbg/migrate.go`
- Test: `cmd/vdbg/migrate_test.go`

**Step 1: Write failing test**

Create temporary schema plan/live schema artifacts with a drift, call `runMigrateWithFactory`, and assert the fake runner was not called.

```go
func TestRunMigrateBlocksWhenSchemaPreflightFails(t *testing.T) {
	dir := t.TempDir()
	schemaPlanPath := filepath.Join(dir, "schema-plan.json")
	liveSchemaPath := filepath.Join(dir, "live-schema.json")
	writeJSONFixture(t, schemaPlanPath, appliedCompareCLISchemaPlanFixture())
	live := appliedCompareCLILiveSchemaFixture()
	live.Tables[0].Columns[1].VectorDimension = 4
	writeJSONFixture(t, liveSchemaPath, live)

	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--source-collection", "items",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--dimension", "8",
		"--require-schema-match",
		"--schema-plan", schemaPlanPath,
		"--live-schema", liveSchemaPath,
	}, fake.newRunner)
	if err == nil || !strings.Contains(err.Error(), "schema preflight failed") {
		t.Fatalf("expected schema preflight failure, got %v", err)
	}
	if fake.migrated {
		t.Fatal("migration should not run when schema preflight fails")
	}
}
```

**Step 2: Run test to verify failure**

```bash
go test ./cmd/vdbg -run 'TestRunMigrateBlocksWhenSchemaPreflightFails' -v
```

Expected: FAIL because preflight is not implemented.

**Step 3: Implement helper**

Add:

```go
func runMigrateSchemaPreflight(options migrateOptions) error {
	if !options.RequireSchemaMatch {
		return nil
	}
	var schemaPlan schema.PGVectorSchemaPlan
	if err := readJSONFile(options.SchemaPlanPath, &schemaPlan); err != nil {
		return err
	}
	var liveSchema schema.PGVectorLiveSchemaInspection
	if err := readJSONFile(options.LiveSchemaPath, &liveSchema); err != nil {
		return err
	}
	report, err := schema.CompareAppliedPGVectorSchema(schemaPlan, liveSchema)
	if err != nil {
		return fmt.Errorf("schema preflight failed: %w", err)
	}
	if report.Status == schema.AppliedSchemaCompareStatusFail {
		return fmt.Errorf("schema preflight failed: planned schema does not match live schema")
	}
	return nil
}
```

Call it in `runMigrateWithFactory` **before** creating/running the migration runner.

**Step 4: Run test**

```bash
go test ./cmd/vdbg -run 'TestRunMigrateBlocksWhenSchemaPreflightFails|TestRunMigrateWithInjectedRunner' -v
```

Expected: PASS.

---

## Task 4: Write migration result report to stdout or output file

**Objective:** `vdbg migrate` should still print the old summary, but also support machine-readable JSON report when `--output` is provided.

**Files:**
- Modify: `cmd/vdbg/migrate.go`
- Test: `cmd/vdbg/migrate_test.go`

**Step 1: Write failing test**

```go
func TestRunMigrateWritesReportOutputWith0600Permissions(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "migration-report.json")
	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--source-collection", "items",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--dimension", "8",
		"--output", outputPath,
		"--job-id", "migration-smoke",
	}, fake.newRunner)
	if err != nil {
		t.Fatalf("runMigrateWithFactory returned error: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", got)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var report migration.VectorMigrationReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.JobID != "migration-smoke" || report.Summary.RecordsWritten != 100 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if strings.Contains(string(data), "postgres://") {
		t.Fatalf("report leaked connection URL: %s", data)
	}
}
```

**Step 2: Run test to verify failure**

```bash
go test ./cmd/vdbg -run 'TestRunMigrateWritesReportOutputWith0600Permissions' -v
```

Expected: FAIL because report output is not implemented.

**Step 3: Implement report writing**

After successful migration:

```go
report := migration.BuildVectorMigrationReport(result, migration.VectorMigrationReportOptions{
	JobID:             options.JobID,
	SchemaPreflight:   options.RequireSchemaMatch,
	SchemaComparePath: options.SchemaPlanPath,
})
if err := writeMigrateReport(options.OutputPath, report); err != nil {
	return err
}
```

Add helper similar to other CLI artifact writers:

```go
func writeMigrateReport(path string, report migration.VectorMigrationReport) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
```

**Step 4: Run tests**

```bash
go test ./cmd/vdbg -run 'TestRunMigrate' -v
```

Expected: PASS.

---

## Task 5: Update docs and examples

**Objective:** Document standalone migration preflight and JSON result artifact.

**Files:**
- Modify: `docs/migrate-cli.md`
- Modify: `docs/zh-CN/migrate-cli.md`
- Modify: `docs/local-migration-stack.md`
- Modify: `docs/zh-CN/local-migration-stack.md`
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `CHANGELOG.md`

**Step 1: Update English docs**

In `docs/migrate-cli.md`, add example:

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --source-collection items \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --dimension 1536 \
  --require-schema-match \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --output /tmp/vdb-guardian-migration-report.json \
  --job-id migration-smoke
```

Document:

- `--require-schema-match` blocks migration when planned-vs-live schema drift exists;
- `--schema-plan` path;
- `--live-schema` path;
- `--output` writes JSON report with `0600` permissions;
- `--job-id` annotates report;
- standalone migration still works without preflight for controlled/dev use.

**Step 2: Update Chinese docs**

Mirror the same content in `docs/zh-CN/migrate-cli.md`.

**Step 3: Update README and CHANGELOG**

Mention that `vdbg migrate` can now optionally require schema match and emit a machine-readable result report.

**Step 4: Verify docs mention flags**

```bash
grep -R "require-schema-match\|migration-report\|schema-plan\|live-schema" -n README.md README.zh-CN.md CHANGELOG.md docs/migrate-cli.md docs/zh-CN/migrate-cli.md
```

Expected: relevant mentions appear in English and Chinese docs.

---

## Task 6: Full quality gate, secret scan, commit, push

**Objective:** Verify and commit the completed Phase 7 increment.

**Files:** all modified files.

**Step 1: Run targeted tests**

```bash
go test ./internal/migration ./cmd/vdbg -run 'TestBuildVectorMigrationReport|TestParseMigrateOptions|TestRunMigrate' -v
```

Expected: PASS.

**Step 2: Run full gate**

```bash
make fmt && make lint && make test && git diff --check && make pre-commit
```

Expected: PASS.

**Step 3: Secret scan**

```bash
git diff | grep -Ei '(api[_-]?key|token|secret|password|BEGIN (RSA|OPENSSH|PRIVATE)|postgres(ql)?://[^[:space:]]+:[^[:space:]@]+@)' || true
```

Expected: no unredacted secrets or real connection strings.

**Step 4: Commit and push**

```bash
git add internal/migration/vector_migration*.go internal/migration/*test.go cmd/vdbg/migrate.go cmd/vdbg/migrate_test.go README.md README.zh-CN.md CHANGELOG.md docs/migrate-cli.md docs/zh-CN/migrate-cli.md docs/local-migration-stack.md docs/zh-CN/local-migration-stack.md
git commit -m "feat(migration): add schema preflight and result report"
git push origin feat/enterprise-scaffold
```

Expected: commit created and pushed.

---

## Acceptance criteria

- `vdbg migrate` remains standalone and works without schema preflight.
- `vdbg migrate --require-schema-match` requires both `--schema-plan` and `--live-schema`.
- Schema preflight uses existing planned-vs-live artifact comparison.
- Migration does not run if schema preflight fails.
- Successful migration can emit JSON report via `--output`.
- Report file permissions are `0600`.
- Report does not include PostgreSQL connection URL.
- Existing text summary remains available for humans.
- English and Chinese docs are synchronized.
- Full quality gate passes.

## Follow-up phases

After this increment, the next coherent data migration phases are:

1. batch-level migration report with per-batch counts;
2. checkpoint/resume artifact;
3. retry and failed-batch journal;
4. metadata/dynamic field payload migration;
5. Milvus partition metadata migration;
6. streaming full-record compare.
