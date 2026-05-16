package migration

import (
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

func TestBuildRecordMappingPlanMapsSchemaPlanColumns(t *testing.T) {
	plan := recordMappingSchemaPlanFixture()

	mapping, err := BuildRecordMappingPlan(plan, RecordMappingOptions{SchemaPlanPath: "/tmp/schema-plan.json"})
	if err != nil {
		t.Fatalf("BuildRecordMappingPlan returned error: %v", err)
	}
	if mapping.SchemaVersion != RecordMappingPlanVersion {
		t.Fatalf("unexpected schema version: %q", mapping.SchemaVersion)
	}
	if mapping.SchemaPlan != "/tmp/schema-plan.json" {
		t.Fatalf("schema plan path not preserved: %q", mapping.SchemaPlan)
	}
	if mapping.Status != RecordMappingStatusPass {
		t.Fatalf("expected pass status, got %#v", mapping)
	}
	if len(mapping.Mappings) != 1 {
		t.Fatalf("expected one collection mapping, got %#v", mapping.Mappings)
	}
	collection := mapping.Mappings[0]
	if collection.SourceCollection != "items" || collection.TargetSchema != "public" || collection.TargetTable != "items" {
		t.Fatalf("unexpected collection mapping: %#v", collection)
	}
	if collection.PrimaryKey == nil || collection.PrimaryKey.Kind != RecordMappingKindPrimaryKey || collection.PrimaryKey.SourceField != "id" || collection.PrimaryKey.TargetColumn != "id" {
		t.Fatalf("unexpected primary key mapping: %#v", collection.PrimaryKey)
	}
	if collection.Vector == nil || collection.Vector.Kind != RecordMappingKindVector || collection.Vector.SourceField != "embedding" || collection.Vector.TargetColumn != "embedding" || collection.Vector.TargetType != "vector(8)" {
		t.Fatalf("unexpected vector mapping: %#v", collection.Vector)
	}
	if len(collection.Scalars) != 1 || collection.Scalars[0].Kind != RecordMappingKindScalar || collection.Scalars[0].SourceField != "title" || collection.Scalars[0].TargetColumn != "title" {
		t.Fatalf("unexpected scalar mappings: %#v", collection.Scalars)
	}
	if collection.DynamicMetadata == nil || collection.DynamicMetadata.Kind != RecordMappingKindDynamicMetadata || collection.DynamicMetadata.TargetColumn != "milvus_dynamic" {
		t.Fatalf("unexpected dynamic metadata mapping: %#v", collection.DynamicMetadata)
	}
	if collection.PartitionMetadata == nil || collection.PartitionMetadata.Kind != RecordMappingKindPartitionMetadata || collection.PartitionMetadata.TargetColumn != "milvus_partition" {
		t.Fatalf("unexpected partition metadata mapping: %#v", collection.PartitionMetadata)
	}
	if mapping.Summary.CollectionCount != 1 || mapping.Summary.ScalarMappingCount != 1 || mapping.Summary.DynamicMetadataMappingCount != 1 || mapping.Summary.PartitionMetadataMappingCount != 1 || mapping.Summary.BlockingIssueCount != 0 {
		t.Fatalf("unexpected summary: %#v", mapping.Summary)
	}
}

func TestBuildRecordMappingPlanBlocksMissingPrimaryKey(t *testing.T) {
	plan := recordMappingSchemaPlanFixture()
	plan.Tables[0].Columns[0].PrimaryKey = false

	mapping, err := BuildRecordMappingPlan(plan, RecordMappingOptions{})
	if err != nil {
		t.Fatalf("BuildRecordMappingPlan returned error: %v", err)
	}
	assertRecordMappingFailedWithIssue(t, mapping, "primary key")
}

func TestBuildRecordMappingPlanBlocksMissingVector(t *testing.T) {
	plan := recordMappingSchemaPlanFixture()
	plan.Tables[0].Columns[1].TargetType = "jsonb"

	mapping, err := BuildRecordMappingPlan(plan, RecordMappingOptions{})
	if err != nil {
		t.Fatalf("BuildRecordMappingPlan returned error: %v", err)
	}
	assertRecordMappingFailedWithIssue(t, mapping, "vector")
}

func TestBuildRecordMappingPlanBlocksUnsupportedColumn(t *testing.T) {
	plan := recordMappingSchemaPlanFixture()
	plan.Tables[0].Columns[2].SupportLevel = inspection.SupportLevelUnsupported
	plan.Tables[0].Columns[2].Warning = "unsupported field"

	mapping, err := BuildRecordMappingPlan(plan, RecordMappingOptions{})
	if err != nil {
		t.Fatalf("BuildRecordMappingPlan returned error: %v", err)
	}
	assertRecordMappingFailedWithIssue(t, mapping, "unsupported")
	if len(mapping.Mappings[0].Scalars) != 1 {
		t.Fatalf("unsupported scalar should still be represented for diagnostics: %#v", mapping.Mappings[0].Scalars)
	}
}

func TestBuildRecordMappingPlanRejectsUnsupportedSchemaVersion(t *testing.T) {
	plan := recordMappingSchemaPlanFixture()
	plan.SchemaVersion = "v0"

	_, err := BuildRecordMappingPlan(plan, RecordMappingOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported pgvector schema plan version") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

func assertRecordMappingFailedWithIssue(t *testing.T, mapping RecordMappingPlan, contains string) {
	t.Helper()
	if mapping.Status != RecordMappingStatusFail {
		t.Fatalf("expected fail status, got %#v", mapping)
	}
	if mapping.Summary.BlockingIssueCount == 0 || len(mapping.Issues) == 0 {
		t.Fatalf("expected blocking issues, got %#v", mapping)
	}
	for _, issue := range mapping.Issues {
		if strings.Contains(strings.ToLower(issue.Message), strings.ToLower(contains)) && issue.SourceCollection == "items" {
			return
		}
	}
	t.Fatalf("expected issue containing %q, got %#v", contains, mapping.Issues)
}

func recordMappingSchemaPlanFixture() planschema.PGVectorSchemaPlan {
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
			}, {
				SourceField:  "title",
				TargetColumn: "title",
				TargetType:   "text",
				Nullable:     true,
				SupportLevel: inspection.SupportLevelSupported,
			}, {
				SourceField:  "_milvus_dynamic",
				TargetColumn: "milvus_dynamic",
				TargetType:   "jsonb",
				Nullable:     true,
				SupportLevel: inspection.SupportLevelDegraded,
				Warning:      "dynamic fields are preserved as metadata",
			}, {
				SourceField:  "_milvus_partition",
				TargetColumn: "milvus_partition",
				TargetType:   "text",
				Nullable:     true,
				SupportLevel: inspection.SupportLevelDegraded,
				Warning:      "partitions are preserved as metadata",
			}},
		}},
	}
}
