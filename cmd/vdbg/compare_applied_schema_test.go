package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

func TestRunCompareAppliedSchemaWritesReportWith0600Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	schemaPlanPath, liveSchemaPath := writeCompareAppliedSchemaFixtures(t, tmpDir, false)
	outputPath := filepath.Join(tmpDir, "compare-report.json")
	var stdout bytes.Buffer

	err := runCompareAppliedSchemaCommand([]string{
		"--schema-plan", schemaPlanPath,
		"--live-schema", liveSchemaPath,
		"--output", outputPath,
	}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, statErr := os.Stat(outputPath)
	if statErr != nil {
		t.Fatalf("stat output: %v", statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var report planschema.AppliedSchemaCompareReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != planschema.SchemaPlanCompareStatusPass || report.Summary.MismatchCount != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if strings.Contains(stdout.String(), "postgres://") {
		t.Fatalf("stdout leaked connection-like content: %s", stdout.String())
	}
}

func TestRunCompareAppliedSchemaReturnsErrorButWritesReportOnMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	schemaPlanPath, liveSchemaPath := writeCompareAppliedSchemaFixtures(t, tmpDir, true)
	outputPath := filepath.Join(tmpDir, "compare-report.json")
	var stdout bytes.Buffer

	err := runCompareAppliedSchemaCommand([]string{
		"--schema-plan", schemaPlanPath,
		"--live-schema", liveSchemaPath,
		"--output", outputPath,
	}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "applied schema comparison failed") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var report planschema.AppliedSchemaCompareReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != planschema.SchemaPlanCompareStatusFail || report.Summary.MismatchCount == 0 {
		t.Fatalf("expected failed report, got %#v", report)
	}
}

func TestRunCompareAppliedSchemaRequiresSchemaPlan(t *testing.T) {
	var stdout bytes.Buffer
	err := runCompareAppliedSchemaCommand([]string{"--live-schema", "live.json"}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "--schema-plan is required") {
		t.Fatalf("expected schema plan required error, got %v", err)
	}
}

func TestRunCompareAppliedSchemaRequiresLiveSchema(t *testing.T) {
	var stdout bytes.Buffer
	err := runCompareAppliedSchemaCommand([]string{"--schema-plan", "schema.json"}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "--live-schema is required") {
		t.Fatalf("expected live schema required error, got %v", err)
	}
}

func TestRunCompareAppliedSchemaWritesJSONToStdoutWhenNoOutput(t *testing.T) {
	tmpDir := t.TempDir()
	schemaPlanPath, liveSchemaPath := writeCompareAppliedSchemaFixtures(t, tmpDir, false)
	var stdout bytes.Buffer

	err := runCompareAppliedSchemaCommand([]string{
		"--schema-plan", schemaPlanPath,
		"--live-schema", liveSchemaPath,
	}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var report planschema.AppliedSchemaCompareReport
	if unmarshalErr := json.Unmarshal(stdout.Bytes(), &report); unmarshalErr != nil {
		t.Fatalf("stdout is not report JSON: %v\n%s", unmarshalErr, stdout.String())
	}
	if report.Status != planschema.SchemaPlanCompareStatusPass {
		t.Fatalf("unexpected stdout report: %#v", report)
	}
}

func writeCompareAppliedSchemaFixtures(t *testing.T, dir string, mismatch bool) (string, string) {
	t.Helper()
	plan := appliedCompareCLISchemaPlanFixture()
	live := appliedCompareCLILiveSchemaFixture()
	if mismatch {
		live.Tables[0].Columns[1].FormattedType = "vector(768)"
		live.Tables[0].Columns[1].VectorDimension = 768
	}
	schemaPlanPath := filepath.Join(dir, "schema-plan.json")
	liveSchemaPath := filepath.Join(dir, "live-schema.json")
	writeJSONFixture(t, schemaPlanPath, plan)
	writeJSONFixture(t, liveSchemaPath, live)
	return schemaPlanPath, liveSchemaPath
}

func appliedCompareCLISchemaPlanFixture() planschema.PGVectorSchemaPlan {
	return planschema.PGVectorSchemaPlan{
		SchemaVersion: planschema.PGVectorSchemaPlanVersion,
		Target:        planschema.PGVectorPlanTarget{Type: "pgvector", Schema: "public"},
		Tables: []planschema.PGVectorTablePlan{{
			SourceCollection: "items",
			TargetSchema:     "public",
			TargetTable:      "items",
			Columns: []planschema.PGVectorColumnPlan{
				{SourceField: "id", TargetColumn: "id", TargetType: "bigint", PrimaryKey: true, Nullable: false, SupportLevel: inspection.SupportLevelSupported},
				{SourceField: "embedding", TargetColumn: "embedding", TargetType: "vector(1536)", Nullable: false, SupportLevel: inspection.SupportLevelSupported},
			},
			Indexes: []planschema.PGVectorIndexPlan{{
				SourceField:     "embedding",
				TargetIndex:     "items_embedding_hnsw_idx",
				TargetIndexType: "hnsw",
				CreateIndexSQL:  "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops)",
				SupportLevel:    inspection.SupportLevelSupported,
			}},
		}},
	}
}

func appliedCompareCLILiveSchemaFixture() planschema.PGVectorLiveSchemaInspection {
	return planschema.PGVectorLiveSchemaInspection{
		SchemaVersion: planschema.PGVectorLiveSchemaInspectionVersion,
		Target:        planschema.PGVectorLiveSchemaTarget{Type: "pgvector", Schema: "public"},
		Extension:     planschema.PGVectorExtensionInspection{Name: "vector", Installed: true, Version: "0.8.0"},
		Tables: []planschema.PGVectorLiveTableInspection{{
			TargetTable: "items",
			Columns: []planschema.PGVectorLiveColumnInspection{
				{Name: "id", Type: "bigint", FormattedType: "bigint", Nullable: false, PrimaryKey: true},
				{Name: "embedding", Type: "vector", FormattedType: "vector(1536)", Nullable: false, VectorDimension: 1536},
			},
			Indexes: []planschema.PGVectorLiveIndexInspection{{
				Name:       "items_embedding_hnsw_idx",
				Method:     "hnsw",
				Definition: "CREATE INDEX items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops)",
			}},
		}},
	}
}
