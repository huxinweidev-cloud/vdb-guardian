package schema

import (
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

func TestBuildPGVectorSchemaPlanFromInspectionPlan(t *testing.T) {
	inspectionPlan := inspection.MilvusInspectionPlan{
		SchemaVersion: inspection.MilvusInspectionSchemaVersion,
		Collections: []inspection.MilvusCollectionPlan{
			{
				Name:                "Items.Collection",
				DynamicFieldEnabled: true,
				PrimaryKey:          "ID",
				Fields: []inspection.MilvusFieldPlan{
					{Name: "ID", SourceType: inspection.MilvusDataTypeVarChar, TargetType: "varchar(64)", MaxLength: 64, PrimaryKey: true, SupportLevel: inspection.SupportLevelSupported},
					{Name: "Embedding", SourceType: inspection.MilvusDataTypeFloatVector, TargetType: "vector(8)", Dimension: 8, SupportLevel: inspection.SupportLevelSupported},
				},
				Indexes: []inspection.MilvusIndexPlan{
					{Field: "Embedding", SourceIndexType: "HNSW", SourceMetric: "COSINE", TargetIndexType: "hnsw", TargetOps: "vector_cosine_ops", SupportLevel: inspection.SupportLevelDegraded},
				},
				Partitions: []inspection.MilvusPartitionPlan{{Name: "p2026", SupportLevel: inspection.SupportLevelDegraded, RecommendedStrategy: "metadata_column"}},
			},
		},
	}

	plan, err := BuildPGVectorSchemaPlan(inspectionPlan, PGVectorSchemaPlannerOptions{TargetSchema: "public"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.SchemaVersion != PGVectorSchemaPlanVersion {
		t.Fatalf("schema version mismatch: %q", plan.SchemaVersion)
	}
	if plan.Target.Schema != "public" {
		t.Fatalf("target schema mismatch: %q", plan.Target.Schema)
	}
	if len(plan.Tables) != 1 {
		t.Fatalf("table count: got %d", len(plan.Tables))
	}
	table := plan.Tables[0]
	if table.SourceCollection != "Items.Collection" || table.TargetTable != "items_collection" {
		t.Fatalf("table mapping mismatch: %#v", table)
	}
	assertColumn(t, table.Columns, "ID", "id", "varchar(64)", true)
	assertColumn(t, table.Columns, "Embedding", "embedding", "vector(8)", false)
	assertColumn(t, table.Columns, "_milvus_dynamic", "_milvus_dynamic", "jsonb", false)
	assertColumn(t, table.Columns, "_milvus_partition", "_milvus_partition", "text", false)
	if !strings.Contains(table.CreateTableSQL, "CREATE TABLE IF NOT EXISTS public.items_collection") {
		t.Fatalf("missing create table SQL: %s", table.CreateTableSQL)
	}
	if len(table.Indexes) != 1 || !strings.Contains(table.Indexes[0].CreateIndexSQL, "USING hnsw") {
		t.Fatalf("missing hnsw index SQL: %#v", table.Indexes)
	}
	if plan.Summary.TableCount != 1 || plan.Summary.WarningCount != 0 || plan.Summary.UnsupportedFeatureCount != 0 {
		t.Fatalf("summary mismatch: %#v", plan.Summary)
	}
}

func TestBuildPGVectorSchemaPlanRejectsUnsupportedInspectionVersion(t *testing.T) {
	_, err := BuildPGVectorSchemaPlan(inspection.MilvusInspectionPlan{SchemaVersion: "future"}, PGVectorSchemaPlannerOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported inspection schema version") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

func TestRenderCreateTableSQL(t *testing.T) {
	table := PGVectorTablePlan{
		TargetSchema: "public",
		TargetTable:  "items",
		Columns: []PGVectorColumnPlan{
			{TargetColumn: "id", TargetType: "varchar(64)", PrimaryKey: true},
			{TargetColumn: "embedding", TargetType: "vector(8)", Nullable: false},
		},
	}
	sql, err := RenderCreateTableSQL(table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "CREATE EXTENSION IF NOT EXISTS vector;\nCREATE TABLE IF NOT EXISTS public.items (\n  id varchar(64) PRIMARY KEY,\n  embedding vector(8) NOT NULL\n);"
	if sql != want {
		t.Fatalf("SQL mismatch:\ngot:\n%s\nwant:\n%s", sql, want)
	}
}

func TestRenderPGVectorIndexSQL(t *testing.T) {
	index := PGVectorIndexPlan{TargetSchema: "public", TargetTable: "items", TargetColumn: "embedding", TargetIndex: "items_embedding_hnsw_idx", TargetIndexType: "hnsw", TargetOps: "vector_cosine_ops"}
	sql, err := RenderCreateIndexSQL(index)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops);"
	if sql != want {
		t.Fatalf("SQL mismatch: got %q want %q", sql, want)
	}
}

func assertColumn(t *testing.T, columns []PGVectorColumnPlan, source, target, targetType string, primaryKey bool) {
	t.Helper()
	for _, column := range columns {
		if column.SourceField == source && column.TargetColumn == target {
			if column.TargetType != targetType || column.PrimaryKey != primaryKey {
				t.Fatalf("column mismatch for %q: %#v", source, column)
			}
			return
		}
	}
	t.Fatalf("missing column source=%q target=%q in %#v", source, target, columns)
}
