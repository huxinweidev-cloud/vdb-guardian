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

func TestRunPlanPGVectorSchemaWritesPlanToFile(t *testing.T) {
	tmpDir := t.TempDir()
	inspectionPath := filepath.Join(tmpDir, "inspection.json")
	outputPath := filepath.Join(tmpDir, "schema-plan.json")
	writeInspectionPlanFixture(t, inspectionPath)

	var stdout bytes.Buffer
	err := runPlanPGVectorSchemaCommandWithWriters([]string{"--inspection-plan", inspectionPath, "--output", outputPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "schema plan completed") || !strings.Contains(stdout.String(), "tables: 1") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read schema plan: %v", err)
	}
	var plan planschema.PGVectorSchemaPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("schema plan is not JSON: %v\n%s", err, string(data))
	}
	if plan.SchemaVersion != planschema.PGVectorSchemaPlanVersion || plan.Summary.TableCount != 1 {
		t.Fatalf("unexpected schema plan: %#v", plan)
	}
}

func TestRunPlanPGVectorSchemaPrintsJSONToStdout(t *testing.T) {
	tmpDir := t.TempDir()
	inspectionPath := filepath.Join(tmpDir, "inspection.json")
	writeInspectionPlanFixture(t, inspectionPath)

	var stdout bytes.Buffer
	err := runPlanPGVectorSchemaCommandWithWriters([]string{"--inspection-plan", inspectionPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var plan planschema.PGVectorSchemaPlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("stdout is not schema plan JSON: %v\n%s", err, stdout.String())
	}
}

func TestParsePlanPGVectorSchemaRequiresInspectionPlan(t *testing.T) {
	_, err := parsePlanPGVectorSchemaOptions([]string{})
	if err == nil || !strings.Contains(err.Error(), "inspection-plan is required") {
		t.Fatalf("expected missing inspection-plan error, got %v", err)
	}
}

func writeInspectionPlanFixture(t *testing.T, path string) {
	t.Helper()
	plan := inspection.MilvusInspectionPlan{
		SchemaVersion: inspection.MilvusInspectionSchemaVersion,
		Collections: []inspection.MilvusCollectionPlan{
			{
				Name:       "items",
				PrimaryKey: "id",
				Fields: []inspection.MilvusFieldPlan{
					{Name: "id", SourceType: inspection.MilvusDataTypeVarChar, TargetType: "varchar(64)", PrimaryKey: true, SupportLevel: inspection.SupportLevelSupported},
					{Name: "embedding", SourceType: inspection.MilvusDataTypeFloatVector, TargetType: "vector(8)", Dimension: 8, SupportLevel: inspection.SupportLevelSupported},
				},
			},
		},
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
