package schema

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

func TestApplyPGVectorSchemaPlanDryRunDoesNotExecuteSQL(t *testing.T) {
	plan := schemaApplyPlanFixture()
	executor := &fakeSchemaApplyExecutor{}

	report, err := ApplyPGVectorSchemaPlan(context.Background(), plan, executor, PGVectorSchemaApplyOptions{JobID: "apply-smoke", Mode: PGVectorSchemaApplyModeDryRun, SchemaPlanPath: "/tmp/schema-plan.json"})
	if err != nil {
		t.Fatalf("ApplyPGVectorSchemaPlan returned error: %v", err)
	}
	if len(executor.sql) != 0 {
		t.Fatalf("dry-run executed SQL: %#v", executor.sql)
	}
	if report.Mode != PGVectorSchemaApplyModeDryRun || report.JobID != "apply-smoke" {
		t.Fatalf("unexpected report identity: %#v", report)
	}
	if report.SchemaPlan != "/tmp/schema-plan.json" {
		t.Fatalf("expected schema plan path in report, got %q", report.SchemaPlan)
	}
	if report.Summary.TableCount != 1 || report.Summary.TableAppliedCount != 0 || report.Summary.IndexCount != 1 || report.Summary.IndexAppliedCount != 0 {
		t.Fatalf("unexpected dry-run summary: %#v", report.Summary)
	}
	if len(report.Tables) != 1 || report.Tables[0].Applied {
		t.Fatalf("expected table present but not applied: %#v", report.Tables)
	}
	if len(report.Tables[0].Indexes) != 1 || report.Tables[0].Indexes[0].Applied {
		t.Fatalf("expected index present but not applied: %#v", report.Tables[0].Indexes)
	}
}

func TestApplyPGVectorSchemaPlanExecuteRunsCreateTableAndIndexes(t *testing.T) {
	plan := schemaApplyPlanFixture()
	executor := &fakeSchemaApplyExecutor{}

	report, err := ApplyPGVectorSchemaPlan(context.Background(), plan, executor, PGVectorSchemaApplyOptions{JobID: "apply-smoke", Mode: PGVectorSchemaApplyModeExecute})
	if err != nil {
		t.Fatalf("ApplyPGVectorSchemaPlan returned error: %v", err)
	}
	expectedSQL := []string{plan.Tables[0].CreateTableSQL, plan.Tables[0].Indexes[0].CreateIndexSQL}
	if !reflect.DeepEqual(executor.sql, expectedSQL) {
		t.Fatalf("unexpected SQL execution order:\nwant %#v\n got %#v", expectedSQL, executor.sql)
	}
	if report.Summary.TableAppliedCount != 1 || report.Summary.IndexAppliedCount != 1 {
		t.Fatalf("unexpected execute summary: %#v", report.Summary)
	}
	if !report.Tables[0].Applied || !report.Tables[0].Indexes[0].Applied {
		t.Fatalf("expected table and index marked applied: %#v", report.Tables[0])
	}
}

func TestApplyPGVectorSchemaPlanCanSkipIndexes(t *testing.T) {
	plan := schemaApplyPlanFixture()
	executor := &fakeSchemaApplyExecutor{}

	report, err := ApplyPGVectorSchemaPlan(context.Background(), plan, executor, PGVectorSchemaApplyOptions{JobID: "apply-smoke", Mode: PGVectorSchemaApplyModeExecute, SkipIndexes: true})
	if err != nil {
		t.Fatalf("ApplyPGVectorSchemaPlan returned error: %v", err)
	}
	expectedSQL := []string{plan.Tables[0].CreateTableSQL}
	if !reflect.DeepEqual(executor.sql, expectedSQL) {
		t.Fatalf("unexpected SQL when skipping indexes:\nwant %#v\n got %#v", expectedSQL, executor.sql)
	}
	if report.Summary.IndexCount != 1 || report.Summary.IndexAppliedCount != 0 {
		t.Fatalf("unexpected skip-index summary: %#v", report.Summary)
	}
	if !report.Tables[0].Indexes[0].Skipped {
		t.Fatalf("expected index marked skipped: %#v", report.Tables[0].Indexes[0])
	}
}

func TestApplyPGVectorSchemaPlanRejectsUnsupportedFeaturesByDefault(t *testing.T) {
	plan := schemaApplyPlanFixture()
	plan.Tables[0].Columns[1].SupportLevel = inspection.SupportLevelUnsupported
	plan.Tables[0].Columns[1].Warning = "binary vector field unsupported"
	executor := &fakeSchemaApplyExecutor{}

	report, err := ApplyPGVectorSchemaPlan(context.Background(), plan, executor, PGVectorSchemaApplyOptions{JobID: "apply-smoke", Mode: PGVectorSchemaApplyModeExecute})
	if err == nil || !strings.Contains(err.Error(), "unsupported features") {
		t.Fatalf("expected unsupported feature error, got %v", err)
	}
	if len(executor.sql) != 0 {
		t.Fatalf("unsupported plan executed SQL: %#v", executor.sql)
	}
	if report.Status != PGVectorSchemaApplyStatusBlocked || report.Summary.UnsupportedFeatureCount != 1 {
		t.Fatalf("unexpected blocked report: %#v", report)
	}
}

func TestApplyPGVectorSchemaPlanAllowsUnsupportedWhenExplicit(t *testing.T) {
	plan := schemaApplyPlanFixture()
	plan.Tables[0].Columns[1].SupportLevel = inspection.SupportLevelUnsupported
	plan.Tables[0].Columns[1].Warning = "metadata-only partition strategy"
	executor := &fakeSchemaApplyExecutor{}

	report, err := ApplyPGVectorSchemaPlan(context.Background(), plan, executor, PGVectorSchemaApplyOptions{JobID: "apply-smoke", Mode: PGVectorSchemaApplyModeExecute, AllowUnsupported: true})
	if err != nil {
		t.Fatalf("ApplyPGVectorSchemaPlan returned error: %v", err)
	}
	if len(executor.sql) != 2 {
		t.Fatalf("expected SQL execution with allow-unsupported, got %#v", executor.sql)
	}
	if report.Status != PGVectorSchemaApplyStatusApplied || report.Summary.UnsupportedFeatureCount != 1 {
		t.Fatalf("unexpected allow-unsupported report: %#v", report)
	}
}

func TestApplyPGVectorSchemaPlanReturnsExecutionErrors(t *testing.T) {
	plan := schemaApplyPlanFixture()
	executor := &fakeSchemaApplyExecutor{errOnSQL: plan.Tables[0].Indexes[0].CreateIndexSQL, err: errors.New("index failed")}

	report, err := ApplyPGVectorSchemaPlan(context.Background(), plan, executor, PGVectorSchemaApplyOptions{JobID: "apply-smoke", Mode: PGVectorSchemaApplyModeExecute})
	if err == nil || !strings.Contains(err.Error(), "index failed") {
		t.Fatalf("expected executor error, got %v", err)
	}
	if report.Status != PGVectorSchemaApplyStatusFailed {
		t.Fatalf("expected failed report, got %#v", report)
	}
	if report.Summary.TableAppliedCount != 1 || report.Summary.IndexAppliedCount != 0 {
		t.Fatalf("unexpected partial failure summary: %#v", report.Summary)
	}
	if report.Tables[0].Error != "" {
		t.Fatalf("table should have applied successfully: %#v", report.Tables[0])
	}
	if !strings.Contains(report.Tables[0].Indexes[0].Error, "index failed") {
		t.Fatalf("expected index error in report: %#v", report.Tables[0].Indexes[0])
	}
}

func schemaApplyPlanFixture() PGVectorSchemaPlan {
	return PGVectorSchemaPlan{
		SchemaVersion: PGVectorSchemaPlanVersion,
		SourcePlan:    "/tmp/milvus-plan.json",
		Target: PGVectorPlanTarget{
			Type:   "pgvector",
			Schema: "public",
		},
		Tables: []PGVectorTablePlan{{
			SourceCollection: "items",
			TargetTable:      "items",
			CreateTableSQL:   "CREATE EXTENSION IF NOT EXISTS vector;\nCREATE TABLE IF NOT EXISTS public.items (id bigint PRIMARY KEY, embedding vector(8));",
			Columns: []PGVectorColumnPlan{{
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
			Indexes: []PGVectorIndexPlan{{
				SourceField:    "embedding",
				TargetIndex:    "items_embedding_hnsw_idx",
				CreateIndexSQL: "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops);",
				SupportLevel:   inspection.SupportLevelSupported,
			}},
		}},
		Summary: PGVectorSchemaSummary{TableCount: 1},
	}
}

type fakeSchemaApplyExecutor struct {
	sql      []string
	errOnSQL string
	err      error
}

func (f *fakeSchemaApplyExecutor) Exec(ctx context.Context, sql string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.sql = append(f.sql, sql)
	if f.errOnSQL == sql {
		return f.err
	}
	return nil
}
