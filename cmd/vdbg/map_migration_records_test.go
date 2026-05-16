package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
	"github.com/h3xwave/vdb-guardian/internal/migration"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

func TestRunMapMigrationRecordsWritesOutputWith0600Permissions(t *testing.T) {
	dir := t.TempDir()
	schemaPlanPath := filepath.Join(dir, "schema-plan.json")
	outputPath := filepath.Join(dir, "record-mapping.json")
	writeJSONFixture(t, schemaPlanPath, mapMigrationRecordsSchemaPlanFixture())

	err := runMapMigrationRecordsCommand([]string{"--schema-plan", schemaPlanPath, "--output", outputPath}, discardFlagOutput{})
	if err != nil {
		t.Fatalf("runMapMigrationRecordsCommand returned error: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
	var report migration.RecordMappingPlan
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != migration.RecordMappingStatusPass || report.Summary.CollectionCount != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestRunMapMigrationRecordsWritesStdoutWhenOutputOmitted(t *testing.T) {
	dir := t.TempDir()
	schemaPlanPath := filepath.Join(dir, "schema-plan.json")
	writeJSONFixture(t, schemaPlanPath, mapMigrationRecordsSchemaPlanFixture())
	stdout := &strings.Builder{}

	err := runMapMigrationRecordsCommand([]string{"--schema-plan", schemaPlanPath}, stdout)
	if err != nil {
		t.Fatalf("runMapMigrationRecordsCommand returned error: %v", err)
	}
	var report migration.RecordMappingPlan
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != migration.RecordMappingStatusPass {
		t.Fatalf("unexpected stdout report: %#v", report)
	}
}

func TestRunMapMigrationRecordsReturnsErrorAfterWritingBlockedReport(t *testing.T) {
	dir := t.TempDir()
	schemaPlanPath := filepath.Join(dir, "schema-plan.json")
	outputPath := filepath.Join(dir, "record-mapping.json")
	plan := mapMigrationRecordsSchemaPlanFixture()
	plan.Tables[0].Columns[1].TargetType = "jsonb"
	writeJSONFixture(t, schemaPlanPath, plan)

	err := runMapMigrationRecordsCommand([]string{"--schema-plan", schemaPlanPath, "--output", outputPath}, discardFlagOutput{})
	if err == nil || !strings.Contains(err.Error(), "record mapping failed") {
		t.Fatalf("expected record mapping failure, got %v", err)
	}
	var report migration.RecordMappingPlan
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("blocked report was not written: %v", readErr)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal blocked report: %v", err)
	}
	if report.Status != migration.RecordMappingStatusFail || report.Summary.BlockingIssueCount == 0 {
		t.Fatalf("unexpected blocked report: %#v", report)
	}
}

func TestRunMapMigrationRecordsRequiresSchemaPlan(t *testing.T) {
	err := runMapMigrationRecordsCommand(nil, discardFlagOutput{})
	if err == nil || !strings.Contains(err.Error(), "schema-plan is required") {
		t.Fatalf("expected schema-plan required error, got %v", err)
	}
}

func mapMigrationRecordsSchemaPlanFixture() planschema.PGVectorSchemaPlan {
	return planschema.PGVectorSchemaPlan{
		SchemaVersion: planschema.PGVectorSchemaPlanVersion,
		Target:        planschema.PGVectorPlanTarget{Type: "pgvector", Schema: "public"},
		Tables: []planschema.PGVectorTablePlan{{
			SourceCollection: "items",
			TargetSchema:     "public",
			TargetTable:      "items",
			Columns: []planschema.PGVectorColumnPlan{{
				SourceField:  "id",
				TargetColumn: "id",
				TargetType:   "text",
				PrimaryKey:   true,
				Nullable:     false,
				SupportLevel: inspection.SupportLevelSupported,
			}, {
				SourceField:  "embedding",
				TargetColumn: "embedding",
				TargetType:   "vector(8)",
				Nullable:     false,
				SupportLevel: inspection.SupportLevelSupported,
			}},
		}},
	}
}
