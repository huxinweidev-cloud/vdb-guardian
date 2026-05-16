package schema

import (
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

func TestCompareAppliedPGVectorSchemaPassesForMatchingLiveSchema(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{
		SchemaPlanPath: "/tmp/schema-plan.json",
		LiveSchemaPath: "/tmp/live-schema.json",
	})
	if err != nil {
		t.Fatalf("CompareAppliedPGVectorSchema returned error: %v", err)
	}
	if report.Status != SchemaPlanCompareStatusPass {
		t.Fatalf("expected pass, got %#v", report)
	}
	if report.Summary.MismatchCount != 0 || report.Summary.WarningCount != 0 || report.Summary.TablesChecked != 1 || report.Summary.ColumnsChecked != 2 || report.Summary.IndexesChecked != 1 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	if report.SchemaPlan != "/tmp/schema-plan.json" || report.LiveSchema != "/tmp/live-schema.json" {
		t.Fatalf("expected provenance paths, got %#v", report)
	}
	assertAppliedCheck(t, report.Tables[0].Checks, "table_present")
	assertAppliedCheck(t, report.Tables[0].Checks, "column_type_matches")
	assertAppliedCheck(t, report.Tables[0].Checks, "primary_key_preserved")
	assertAppliedCheck(t, report.Tables[0].Checks, "vector_dimension_preserved")
	assertAppliedCheck(t, report.Tables[0].Checks, "index_present")
}

func TestCompareAppliedPGVectorSchemaFailsWhenTableMissing(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Tables = nil

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	assertAppliedMismatch(t, report, "table_present")
}

func TestCompareAppliedPGVectorSchemaFailsWhenColumnMissing(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Tables[0].Columns = live.Tables[0].Columns[:1]

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	assertAppliedMismatch(t, report, "column_present")
}

func TestCompareAppliedPGVectorSchemaNormalizesEquivalentVarcharAlias(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	plan.Tables[0].Columns[0].TargetType = "varchar(256)"
	live := appliedCompareLiveSchemaFixture()
	live.Tables[0].Columns[0].Type = "character varying(256)"
	live.Tables[0].Columns[0].FormattedType = "character varying(256)"

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	if report.Status != SchemaPlanCompareStatusPass {
		t.Fatalf("expected equivalent varchar alias to pass, got %#v", report)
	}
	assertAppliedCheck(t, report.Tables[0].Checks, "column_type_matches")
}

func TestCompareAppliedPGVectorSchemaFailsWhenColumnTypeDiffers(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Tables[0].Columns[1].FormattedType = "vector(768)"
	live.Tables[0].Columns[1].VectorDimension = 768

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	assertAppliedMismatch(t, report, "column_type_matches")
	assertAppliedMismatch(t, report, "vector_dimension_preserved")
}

func TestCompareAppliedPGVectorSchemaFailsWhenNullableDiffers(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Tables[0].Columns[1].Nullable = true

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	assertAppliedMismatch(t, report, "nullable_matches")
}

func TestCompareAppliedPGVectorSchemaFailsWhenPrimaryKeyMissing(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Tables[0].Columns[0].PrimaryKey = false

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	assertAppliedMismatch(t, report, "primary_key_preserved")
}

func TestCompareAppliedPGVectorSchemaFailsWhenVectorExtensionMissingForVectorPlan(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Extension.Installed = false
	live.Extension.Version = ""

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	assertAppliedMismatch(t, report, "pgvector_extension_installed")
}

func TestCompareAppliedPGVectorSchemaFailsWhenPlannedIndexMissing(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Tables[0].Indexes = nil

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	assertAppliedMismatch(t, report, "index_present")
}

func TestCompareAppliedPGVectorSchemaFailsWhenIndexMethodDiffers(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Tables[0].Indexes[0].Method = "ivfflat"

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	assertAppliedMismatch(t, report, "index_method_matches")
}

func TestCompareAppliedPGVectorSchemaIgnoresFlatExactScanIndexes(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	plan.Tables[0].Indexes[0].TargetIndexType = "flat"
	plan.Tables[0].Indexes[0].CreateIndexSQL = ""
	live := appliedCompareLiveSchemaFixture()
	live.Tables[0].Indexes = nil

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	if report.Status != SchemaPlanCompareStatusPass {
		t.Fatalf("expected pass for flat exact scan, got %#v", report)
	}
}

func TestCompareAppliedPGVectorSchemaWarnsForExtraLiveObjects(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	live := appliedCompareLiveSchemaFixture()
	live.Tables = append(live.Tables, PGVectorLiveTableInspection{TargetTable: "audit_log"})
	live.Tables[0].Columns = append(live.Tables[0].Columns, PGVectorLiveColumnInspection{Name: "created_at", Type: "timestamp", FormattedType: "timestamp with time zone"})
	live.Tables[0].Indexes = append(live.Tables[0].Indexes, PGVectorLiveIndexInspection{Name: "items_extra_idx", Method: "btree", Definition: "CREATE INDEX items_extra_idx ON public.items USING btree (id)"})

	report, err := CompareAppliedPGVectorSchema(plan, live, AppliedSchemaCompareOptions{})
	assertNoCompareError(t, err)
	if report.Status != SchemaPlanCompareStatusWarn || report.Summary.WarningCount != 3 {
		t.Fatalf("expected warn with three warnings, got %#v", report)
	}
}

func TestCompareAppliedPGVectorSchemaRejectsUnsupportedPlanVersion(t *testing.T) {
	plan := appliedCompareSchemaPlanFixture()
	plan.SchemaVersion = "v999"
	_, err := CompareAppliedPGVectorSchema(plan, appliedCompareLiveSchemaFixture(), AppliedSchemaCompareOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported pgvector schema plan version") {
		t.Fatalf("expected unsupported plan version error, got %v", err)
	}
}

func TestCompareAppliedPGVectorSchemaRejectsUnsupportedLiveVersion(t *testing.T) {
	live := appliedCompareLiveSchemaFixture()
	live.SchemaVersion = "v999"
	_, err := CompareAppliedPGVectorSchema(appliedCompareSchemaPlanFixture(), live, AppliedSchemaCompareOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported pgvector live schema inspection version") {
		t.Fatalf("expected unsupported live version error, got %v", err)
	}
}

func appliedCompareSchemaPlanFixture() PGVectorSchemaPlan {
	return PGVectorSchemaPlan{
		SchemaVersion: PGVectorSchemaPlanVersion,
		Target: PGVectorPlanTarget{
			Type:   "pgvector",
			Schema: "public",
		},
		Tables: []PGVectorTablePlan{
			{
				SourceCollection: "items",
				TargetTable:      "items",
				Columns: []PGVectorColumnPlan{
					{SourceField: "id", TargetColumn: "id", TargetType: "bigint", PrimaryKey: true, Nullable: false, SupportLevel: inspection.SupportLevelSupported},
					{SourceField: "embedding", TargetColumn: "embedding", TargetType: "vector(1536)", Nullable: false, SupportLevel: inspection.SupportLevelSupported},
				},
				Indexes: []PGVectorIndexPlan{
					{SourceField: "embedding", TargetIndex: "items_embedding_hnsw_idx", TargetIndexType: "hnsw", CreateIndexSQL: "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops)", SupportLevel: inspection.SupportLevelSupported},
				},
			},
		},
	}
}

func appliedCompareLiveSchemaFixture() PGVectorLiveSchemaInspection {
	return PGVectorLiveSchemaInspection{
		SchemaVersion: PGVectorLiveSchemaInspectionVersion,
		Target: PGVectorLiveSchemaTarget{
			Type:   "pgvector",
			Schema: "public",
		},
		Extension: PGVectorExtensionInspection{Name: "vector", Installed: true, Version: "0.8.0"},
		Tables: []PGVectorLiveTableInspection{
			{
				TargetTable: "items",
				Columns: []PGVectorLiveColumnInspection{
					{Name: "id", Type: "bigint", FormattedType: "bigint", Nullable: false, PrimaryKey: true},
					{Name: "embedding", Type: "vector", FormattedType: "vector(1536)", Nullable: false, VectorDimension: 1536},
				},
				Indexes: []PGVectorLiveIndexInspection{
					{Name: "items_embedding_hnsw_idx", Method: "hnsw", Definition: "CREATE INDEX items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops)"},
				},
			},
		},
	}
}

func assertAppliedCheck(t *testing.T, checks []AppliedSchemaCheck, name string) {
	t.Helper()
	for _, check := range checks {
		if check.Name == name && check.Status == SchemaPlanCompareStatusPass {
			return
		}
	}
	t.Fatalf("missing applied schema check %q in %#v", name, checks)
}

func assertAppliedMismatch(t *testing.T, report AppliedSchemaCompareReport, name string) {
	t.Helper()
	if report.Status != SchemaPlanCompareStatusFail {
		t.Fatalf("expected fail status, got %#v", report)
	}
	for _, table := range report.Tables {
		for _, mismatch := range table.Mismatches {
			if mismatch.Name == name {
				return
			}
		}
	}
	t.Fatalf("missing applied schema mismatch %q in %#v", name, report)
}

func assertNoCompareError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected compare error: %v", err)
	}
}
