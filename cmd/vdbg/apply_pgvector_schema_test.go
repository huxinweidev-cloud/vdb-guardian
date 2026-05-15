package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

func TestRunApplyPGVectorSchemaDryRunWritesReportWithoutConnectionURL(t *testing.T) {
	tmp := t.TempDir()
	schemaPlanPath := filepath.Join(tmp, "schema-plan.json")
	writeApplySchemaPlanFixture(t, schemaPlanPath, false)
	artifactDir := filepath.Join(tmp, "artifacts")
	var stdout bytes.Buffer
	factory := func(_ string) (planschema.PGVectorSchemaExecutor, func() error, error) {
		t.Fatal("dry-run must not create executor")
		return nil, nil, nil
	}

	err := runApplyPGVectorSchemaCommandWithFactory(context.Background(), []string{
		"--schema-plan", schemaPlanPath,
		"--artifact-dir", artifactDir,
		"--job-id", "apply-smoke",
	}, &stdout, factory)
	if err != nil {
		t.Fatalf("runApplyPGVectorSchemaCommandWithFactory returned error: %v", err)
	}
	reportPath := filepath.Join(artifactDir, "apply-smoke-pgvector-schema-apply-report.json")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected report file: %v", err)
	}
	assertMode0600(t, reportPath)
	if strings.Contains(stdout.String(), "postgres://") || strings.Contains(string(data), "postgres://") {
		t.Fatalf("connection URL leaked in stdout/report")
	}
	var report planschema.PGVectorSchemaApplyReport
	if err = json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != planschema.PGVectorSchemaApplyStatusPlanned || report.Mode != planschema.PGVectorSchemaApplyModeDryRun {
		t.Fatalf("unexpected dry-run report: %#v", report)
	}
	if !strings.Contains(stdout.String(), reportPath) {
		t.Fatalf("stdout should include report path, got %q", stdout.String())
	}
}

func TestRunApplyPGVectorSchemaExecuteRequiresConnectionURL(t *testing.T) {
	tmp := t.TempDir()
	schemaPlanPath := filepath.Join(tmp, "schema-plan.json")
	writeApplySchemaPlanFixture(t, schemaPlanPath, false)
	var stdout bytes.Buffer

	err := runApplyPGVectorSchemaCommandWithFactory(context.Background(), []string{
		"--schema-plan", schemaPlanPath,
		"--artifact-dir", filepath.Join(tmp, "artifacts"),
		"--execute",
	}, &stdout, nil)
	if err == nil || !strings.Contains(err.Error(), "pgvector connection URL") {
		t.Fatalf("expected connection URL error, got %v", err)
	}
}

func TestRunApplyPGVectorSchemaExecuteUsesInjectedExecutor(t *testing.T) {
	tmp := t.TempDir()
	schemaPlanPath := filepath.Join(tmp, "schema-plan.json")
	plan := writeApplySchemaPlanFixture(t, schemaPlanPath, false)
	executor := &fakeApplyCLIExecutor{}
	factory := func(connectionURL string) (planschema.PGVectorSchemaExecutor, func() error, error) {
		if connectionURL != "postgres://user@example/db" {
			t.Fatalf("unexpected connection URL passed to factory: %q", connectionURL)
		}
		return executor, func() error { executor.closed = true; return nil }, nil
	}
	var stdout bytes.Buffer

	err := runApplyPGVectorSchemaCommandWithFactory(context.Background(), []string{
		"--schema-plan", schemaPlanPath,
		"--artifact-dir", filepath.Join(tmp, "artifacts"),
		"--job-id", "apply-smoke",
		"--pgvector-connection-url", "postgres://user@example/db",
		"--execute",
	}, &stdout, factory)
	if err != nil {
		t.Fatalf("runApplyPGVectorSchemaCommandWithFactory returned error: %v", err)
	}
	expected := []string{plan.Tables[0].CreateTableSQL, plan.Tables[0].Indexes[0].CreateIndexSQL}
	if strings.Join(executor.sql, "\n---\n") != strings.Join(expected, "\n---\n") {
		t.Fatalf("unexpected executed SQL:\nwant %#v\n got %#v", expected, executor.sql)
	}
	if !executor.closed {
		t.Fatal("expected executor close function to be called")
	}
	if strings.Contains(stdout.String(), "user@example") {
		t.Fatalf("stdout leaked connection URL: %q", stdout.String())
	}
}

func TestRunApplyPGVectorSchemaSkipIndexes(t *testing.T) {
	tmp := t.TempDir()
	schemaPlanPath := filepath.Join(tmp, "schema-plan.json")
	plan := writeApplySchemaPlanFixture(t, schemaPlanPath, false)
	executor := &fakeApplyCLIExecutor{}
	factory := func(_ string) (planschema.PGVectorSchemaExecutor, func() error, error) {
		return executor, nil, nil
	}
	var stdout bytes.Buffer

	err := runApplyPGVectorSchemaCommandWithFactory(context.Background(), []string{
		"--schema-plan", schemaPlanPath,
		"--artifact-dir", filepath.Join(tmp, "artifacts"),
		"--pgvector-connection-url", "postgres://user@example/db",
		"--execute",
		"--skip-indexes",
	}, &stdout, factory)
	if err != nil {
		t.Fatalf("runApplyPGVectorSchemaCommandWithFactory returned error: %v", err)
	}
	if len(executor.sql) != 1 || executor.sql[0] != plan.Tables[0].CreateTableSQL {
		t.Fatalf("unexpected SQL with skip-indexes: %#v", executor.sql)
	}
}

func TestRunApplyPGVectorSchemaWritesBlockedReportBeforeReturningError(t *testing.T) {
	tmp := t.TempDir()
	schemaPlanPath := filepath.Join(tmp, "schema-plan.json")
	writeApplySchemaPlanFixture(t, schemaPlanPath, true)
	artifactDir := filepath.Join(tmp, "artifacts")
	var stdout bytes.Buffer

	err := runApplyPGVectorSchemaCommandWithFactory(context.Background(), []string{
		"--schema-plan", schemaPlanPath,
		"--artifact-dir", artifactDir,
		"--job-id", "blocked",
		"--pgvector-connection-url", "postgres://user@example/db",
		"--execute",
	}, &stdout, func(_ string) (planschema.PGVectorSchemaExecutor, func() error, error) {
		return &fakeApplyCLIExecutor{}, nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported features") {
		t.Fatalf("expected unsupported feature error, got %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(artifactDir, "blocked-pgvector-schema-apply-report.json"))
	if readErr != nil {
		t.Fatalf("expected blocked report to be written: %v", readErr)
	}
	var report planschema.PGVectorSchemaApplyReport
	if err = json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != planschema.PGVectorSchemaApplyStatusBlocked {
		t.Fatalf("unexpected blocked report: %#v", report)
	}
}

func writeApplySchemaPlanFixture(t *testing.T, path string, unsupported bool) planschema.PGVectorSchemaPlan {
	t.Helper()
	plan := planschema.PGVectorSchemaPlan{
		SchemaVersion: planschema.PGVectorSchemaPlanVersion,
		Target: planschema.PGVectorPlanTarget{
			Type:   "pgvector",
			Schema: "public",
		},
		Tables: []planschema.PGVectorTablePlan{{
			SourceCollection: "items",
			TargetSchema:     "public",
			TargetTable:      "items",
			CreateTableSQL:   "CREATE EXTENSION IF NOT EXISTS vector;\nCREATE TABLE IF NOT EXISTS public.items (id bigint PRIMARY KEY, embedding vector(8));",
			Columns: []planschema.PGVectorColumnPlan{{
				SourceField:  "id",
				TargetColumn: "id",
				TargetType:   "bigint",
				PrimaryKey:   true,
				SupportLevel: inspection.SupportLevelSupported,
			}, {
				SourceField:  "embedding",
				TargetColumn: "embedding",
				TargetType:   "vector(8)",
				SupportLevel: inspection.SupportLevelSupported,
			}},
			Indexes: []planschema.PGVectorIndexPlan{{
				SourceField:    "embedding",
				TargetIndex:    "items_embedding_hnsw_idx",
				CreateIndexSQL: "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops);",
				SupportLevel:   inspection.SupportLevelSupported,
			}},
		}},
	}
	if unsupported {
		plan.Tables[0].Columns[1].SupportLevel = inspection.SupportLevelUnsupported
		plan.Tables[0].Columns[1].Warning = "unsupported vector feature"
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal schema plan: %v", err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write schema plan: %v", err)
	}
	return plan
}

func assertMode0600(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected %s mode 0600, got %o", path, info.Mode().Perm())
	}
}

type fakeApplyCLIExecutor struct {
	sql    []string
	closed bool
}

func (f *fakeApplyCLIExecutor) Exec(ctx context.Context, sql string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.sql = append(f.sql, sql)
	return nil
}
