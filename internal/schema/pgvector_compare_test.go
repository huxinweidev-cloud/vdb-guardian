package schema

import (
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

func TestCompareSchemaPlansPassesForEquivalentPlans(t *testing.T) {
	inspectionPlan := schemaCompareInspectionFixture()
	schemaPlan, err := BuildPGVectorSchemaPlan(inspectionPlan, PGVectorSchemaPlannerOptions{TargetSchema: "public", SourcePlan: "milvus-plan.json"})
	if err != nil {
		t.Fatalf("BuildPGVectorSchemaPlan returned error: %v", err)
	}

	report, err := CompareSchemaPlans(inspectionPlan, schemaPlan, PlanCompareOptions{InspectionPlanPath: "milvus-plan.json", SchemaPlanPath: "schema-plan.json"})
	if err != nil {
		t.Fatalf("CompareSchemaPlans returned error: %v", err)
	}
	if report.SchemaVersion != PlanCompareReportVersion {
		t.Fatalf("unexpected schema version %q", report.SchemaVersion)
	}
	if report.Status != SchemaPlanCompareStatusPass {
		t.Fatalf("expected pass status, got %q with mismatches %#v", report.Status, report.Collections[0].Mismatches)
	}
	if report.Summary.CollectionsChecked != 1 || report.Summary.TablesChecked != 1 || report.Summary.FieldsChecked != 3 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	if report.Summary.MismatchCount != 0 {
		t.Fatalf("expected no mismatches: %#v", report.Summary)
	}
	if len(report.Collections) != 1 || report.Collections[0].Status != SchemaPlanCompareStatusPass {
		t.Fatalf("unexpected collection report: %#v", report.Collections)
	}
	assertHasCheck(t, report.Collections[0].Checks, "primary_key_preserved")
	assertHasCheck(t, report.Collections[0].Checks, "vector_dimension_preserved")
	assertHasCheck(t, report.Collections[0].Checks, "dynamic_field_mapped")
	assertHasCheck(t, report.Collections[0].Checks, "partition_metadata_mapped")
}

func TestCompareSchemaPlansFailsWhenFieldColumnIsMissing(t *testing.T) {
	inspectionPlan := schemaCompareInspectionFixture()
	schemaPlan, err := BuildPGVectorSchemaPlan(inspectionPlan, PGVectorSchemaPlannerOptions{TargetSchema: "public"})
	if err != nil {
		t.Fatalf("BuildPGVectorSchemaPlan returned error: %v", err)
	}
	schemaPlan.Tables[0].Columns = schemaPlan.Tables[0].Columns[:2]

	report, err := CompareSchemaPlans(inspectionPlan, schemaPlan, PlanCompareOptions{})
	if err != nil {
		t.Fatalf("CompareSchemaPlans returned error: %v", err)
	}
	if report.Status != SchemaPlanCompareStatusFail {
		t.Fatalf("expected fail status, got %q", report.Status)
	}
	if report.Summary.MismatchCount == 0 {
		t.Fatalf("expected mismatches: %#v", report.Summary)
	}
	assertHasMismatch(t, report.Collections[0].Mismatches, "field_column_present")
}

func TestCompareSchemaPlansFailsWhenVectorDimensionChanges(t *testing.T) {
	inspectionPlan := schemaCompareInspectionFixture()
	schemaPlan, err := BuildPGVectorSchemaPlan(inspectionPlan, PGVectorSchemaPlannerOptions{TargetSchema: "public"})
	if err != nil {
		t.Fatalf("BuildPGVectorSchemaPlan returned error: %v", err)
	}
	for i := range schemaPlan.Tables[0].Columns {
		if schemaPlan.Tables[0].Columns[i].SourceField == "embedding" {
			schemaPlan.Tables[0].Columns[i].TargetType = "vector(7)"
		}
	}

	report, err := CompareSchemaPlans(inspectionPlan, schemaPlan, PlanCompareOptions{})
	if err != nil {
		t.Fatalf("CompareSchemaPlans returned error: %v", err)
	}
	if report.Status != SchemaPlanCompareStatusFail {
		t.Fatalf("expected fail status, got %q", report.Status)
	}
	assertHasMismatch(t, report.Collections[0].Mismatches, "vector_dimension_preserved")
}

func TestCompareSchemaPlansRejectsUnsupportedPlanVersions(t *testing.T) {
	inspectionPlan := schemaCompareInspectionFixture()
	schemaPlan, err := BuildPGVectorSchemaPlan(inspectionPlan, PGVectorSchemaPlannerOptions{TargetSchema: "public"})
	if err != nil {
		t.Fatalf("BuildPGVectorSchemaPlan returned error: %v", err)
	}
	schemaPlan.SchemaVersion = "v-next"
	_, err = CompareSchemaPlans(inspectionPlan, schemaPlan, PlanCompareOptions{})
	if err == nil {
		t.Fatal("expected unsupported schema plan version error")
	}
}

func schemaCompareInspectionFixture() inspection.MilvusInspectionPlan {
	collection := inspection.MilvusCollectionPlan{
		Name:                "Items",
		RowCount:            2,
		DynamicFieldEnabled: true,
		PrimaryKey:          "id",
		Fields: []inspection.MilvusFieldPlan{
			inspection.MapMilvusFieldToPGVector(inspection.MilvusFieldPlan{Name: "id", SourceType: inspection.MilvusDataTypeVarChar, MaxLength: 64, PrimaryKey: true, Nullable: false}),
			inspection.MapMilvusFieldToPGVector(inspection.MilvusFieldPlan{Name: "embedding", SourceType: inspection.MilvusDataTypeFloatVector, Dimension: 8, Nullable: false}),
			inspection.MapMilvusFieldToPGVector(inspection.MilvusFieldPlan{Name: "profile", SourceType: inspection.MilvusDataTypeJSON, Nullable: true}),
		},
		Indexes: []inspection.MilvusIndexPlan{{
			Field:           "embedding",
			SourceIndexType: "HNSW",
			SourceMetric:    "COSINE",
			TargetIndexType: "hnsw",
			TargetOps:       "vector_cosine_ops",
			SupportLevel:    inspection.SupportLevelSupported,
		}},
		Partitions: []inspection.MilvusPartitionPlan{{Name: "hot", SupportLevel: inspection.SupportLevelDegraded, RecommendedStrategy: "metadata_column"}},
	}
	return inspection.MilvusInspectionPlan{
		SchemaVersion: inspection.MilvusInspectionSchemaVersion,
		Source:        inspection.MilvusInspectionSource{Type: "milvus"},
		Collections:   []inspection.MilvusCollectionPlan{collection},
		Summary:       inspection.BuildMilvusInspectionSummary([]inspection.MilvusCollectionPlan{collection}),
	}
}

func assertHasCheck(t *testing.T, checks []PlanCheck, name string) {
	t.Helper()
	for _, check := range checks {
		if check.Name == name && check.Status == SchemaPlanCompareStatusPass {
			return
		}
	}
	t.Fatalf("missing passing check %q in %#v", name, checks)
}

func assertHasMismatch(t *testing.T, mismatches []PlanMismatch, name string) {
	t.Helper()
	for _, mismatch := range mismatches {
		if mismatch.Name == name {
			return
		}
	}
	t.Fatalf("missing mismatch %q in %#v", name, mismatches)
}
