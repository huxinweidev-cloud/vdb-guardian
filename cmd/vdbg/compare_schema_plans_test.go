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

func TestRunCompareSchemaPlansWritesReport(t *testing.T) {
	tempDir := t.TempDir()
	inspectionPlan := compareSchemaPlansInspectionFixture()
	inspectionPath := filepath.Join(tempDir, "milvus-plan.json")
	writeJSONFixture(t, inspectionPath, inspectionPlan)
	schemaPlan, err := planschema.BuildPGVectorSchemaPlan(inspectionPlan, planschema.PGVectorSchemaPlannerOptions{TargetSchema: "public", SourcePlan: inspectionPath})
	if err != nil {
		t.Fatalf("BuildPGVectorSchemaPlan returned error: %v", err)
	}
	schemaPath := filepath.Join(tempDir, "schema-plan.json")
	writeJSONFixture(t, schemaPath, schemaPlan)
	outputPath := filepath.Join(tempDir, "schema-compare-report.json")
	var stdout bytes.Buffer

	err = runCompareSchemaPlansCommand([]string{"--inspection-plan", inspectionPath, "--schema-plan", schemaPath, "--output", outputPath}, &stdout)
	if err != nil {
		t.Fatalf("runCompareSchemaPlansCommand returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "schema comparison completed") || !strings.Contains(stdout.String(), "status: pass") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("expected report to be written: %v", err)
	}
	var report planschema.PlanCompareReport
	if err = json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != planschema.SchemaPlanCompareStatusPass || report.Summary.MismatchCount != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected output mode 0600, got %o", info.Mode().Perm())
	}
}

func TestRunCompareSchemaPlansReturnsErrorForMismatches(t *testing.T) {
	tempDir := t.TempDir()
	inspectionPlan := compareSchemaPlansInspectionFixture()
	inspectionPath := filepath.Join(tempDir, "milvus-plan.json")
	writeJSONFixture(t, inspectionPath, inspectionPlan)
	schemaPlan, err := planschema.BuildPGVectorSchemaPlan(inspectionPlan, planschema.PGVectorSchemaPlannerOptions{TargetSchema: "public"})
	if err != nil {
		t.Fatalf("BuildPGVectorSchemaPlan returned error: %v", err)
	}
	schemaPlan.Tables[0].Columns = schemaPlan.Tables[0].Columns[:1]
	schemaPath := filepath.Join(tempDir, "schema-plan.json")
	writeJSONFixture(t, schemaPath, schemaPlan)
	outputPath := filepath.Join(tempDir, "schema-compare-report.json")
	var stdout bytes.Buffer

	err = runCompareSchemaPlansCommand([]string{"--inspection-plan", inspectionPath, "--schema-plan", schemaPath, "--output", outputPath}, &stdout)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "schema comparison failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected failed comparison report to be written: %v", statErr)
	}
}

func TestParseCompareSchemaPlansOptionsRequiresInputs(t *testing.T) {
	_, err := parseCompareSchemaPlansOptions([]string{"--schema-plan", "schema.json"})
	if err == nil || !strings.Contains(err.Error(), "inspection-plan is required") {
		t.Fatalf("expected inspection-plan error, got %v", err)
	}
	_, err = parseCompareSchemaPlansOptions([]string{"--inspection-plan", "inspection.json"})
	if err == nil || !strings.Contains(err.Error(), "schema-plan is required") {
		t.Fatalf("expected schema-plan error, got %v", err)
	}
}

func compareSchemaPlansInspectionFixture() inspection.MilvusInspectionPlan {
	collection := inspection.MilvusCollectionPlan{
		Name:                "items",
		DynamicFieldEnabled: true,
		PrimaryKey:          "id",
		Fields: []inspection.MilvusFieldPlan{
			inspection.MapMilvusFieldToPGVector(inspection.MilvusFieldPlan{Name: "id", SourceType: inspection.MilvusDataTypeVarChar, MaxLength: 64, PrimaryKey: true}),
			inspection.MapMilvusFieldToPGVector(inspection.MilvusFieldPlan{Name: "embedding", SourceType: inspection.MilvusDataTypeFloatVector, Dimension: 8}),
		},
		Partitions: []inspection.MilvusPartitionPlan{{Name: "default", SupportLevel: inspection.SupportLevelDegraded}},
	}
	return inspection.MilvusInspectionPlan{
		SchemaVersion: inspection.MilvusInspectionSchemaVersion,
		Source:        inspection.MilvusInspectionSource{Type: "milvus"},
		Collections:   []inspection.MilvusCollectionPlan{collection},
		Summary:       inspection.BuildMilvusInspectionSummary([]inspection.MilvusCollectionPlan{collection}),
	}
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}
