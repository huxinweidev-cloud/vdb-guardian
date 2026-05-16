package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/h3xwave/vdb-guardian/internal/migration"
)

func TestParseMigrateOptions(t *testing.T) {
	options, err := parseMigrateOptions([]string{
		"--milvus-address", "localhost:19530",
		"--source-collection", "source_items",
		"--milvus-id-field", "vector_id",
		"--milvus-vector-field", "embedding",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "target_items",
		"--pgvector-id-column", "vector_id",
		"--pgvector-vector-column", "embedding",
		"--dimension", "8",
		"--batch-size", "25",
	})
	if err != nil {
		t.Fatalf("parseMigrateOptions returned error: %v", err)
	}
	if options.MilvusConfig.Address != "localhost:19530" {
		t.Fatalf("unexpected milvus address: %s", options.MilvusConfig.Address)
	}
	if options.MigrationConfig.SourceCollection != "source_items" {
		t.Fatalf("unexpected source collection: %s", options.MigrationConfig.SourceCollection)
	}
	if options.MilvusConfig.IDField != "vector_id" || options.MilvusConfig.VectorField != "embedding" {
		t.Fatalf("unexpected milvus fields: %+v", options.MilvusConfig)
	}
	if options.PGVectorConfig.ConnectionURL != "postgres://[REDACTED]" {
		t.Fatalf("unexpected connection url: %s", options.PGVectorConfig.ConnectionURL)
	}
	if options.MigrationConfig.TargetTable != "target_items" {
		t.Fatalf("unexpected target table: %s", options.MigrationConfig.TargetTable)
	}
	if options.PGVectorConfig.IDColumn != "vector_id" || options.PGVectorConfig.VectorColumn != "embedding" {
		t.Fatalf("unexpected pgvector columns: %+v", options.PGVectorConfig)
	}
	if options.MigrationConfig.Dimension != 8 || options.MigrationConfig.BatchSize != 25 {
		t.Fatalf("unexpected migration config: %+v", options.MigrationConfig)
	}
}

func TestParseMigrateOptionsWithSchemaPreflightAndOutput(t *testing.T) {
	options, err := parseMigrateOptions([]string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
		"--require-schema-match",
		"--schema-plan", "/tmp/schema-plan.json",
		"--live-schema", "/tmp/live-schema.json",
		"--output", "/tmp/migration-report.json",
		"--job-id", "migration-smoke",
	})
	if err != nil {
		t.Fatalf("parseMigrateOptions returned error: %v", err)
	}
	if !options.RequireSchemaMatch {
		t.Fatal("expected schema match preflight to be required")
	}
	if options.SchemaPlanPath != "/tmp/schema-plan.json" || options.LiveSchemaPath != "/tmp/live-schema.json" {
		t.Fatalf("unexpected schema paths: %+v", options)
	}
	if options.OutputPath != "/tmp/migration-report.json" || options.JobID != "migration-smoke" {
		t.Fatalf("unexpected report options: %+v", options)
	}
}

func TestParseMigrateOptionsWithCheckpointAndResume(t *testing.T) {
	options, err := parseMigrateOptions([]string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
		"--checkpoint-path", "/tmp/checkpoint.json",
		"--resume-from", "/tmp/checkpoint.json",
	})
	if err != nil {
		t.Fatalf("parseMigrateOptions returned error: %v", err)
	}
	if options.CheckpointPath != "/tmp/checkpoint.json" || options.ResumeFromPath != "/tmp/checkpoint.json" {
		t.Fatalf("unexpected checkpoint options: %+v", options)
	}
	if options.MigrationConfig.CheckpointPath != "/tmp/checkpoint.json" || options.MigrationConfig.ResumeFromPath != "/tmp/checkpoint.json" {
		t.Fatalf("unexpected migration checkpoint config: %+v", options.MigrationConfig)
	}
}

func TestRunMigrateLoadsAndValidatesResumeCheckpoint(t *testing.T) {
	dir := t.TempDir()
	checkpointPath := filepath.Join(dir, "checkpoint.json")
	checkpoint := migration.BuildVectorMigrationCheckpoint(migration.VectorMigrationCheckpointInput{Status: migration.VectorMigrationCheckpointStatusFailed, SourceCollection: "items", TargetTable: "items", Dimension: 8, BatchSize: 100, RecordsRead: 100, RecordsWritten: 100, CompletedBatches: []migration.VectorMigrationCheckpointBatch{{Index: 0, Start: 0, End: 100, RecordsWritten: 100}}, Resume: migration.VectorMigrationCheckpointResume{NextBatchIndex: 1, NextRecordOffset: 100}})
	if err := migration.WriteVectorMigrationCheckpoint(checkpointPath, checkpoint); err != nil {
		t.Fatalf("write checkpoint fixture: %v", err)
	}
	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--source-collection", "items",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--dimension", "8",
		"--resume-from", checkpointPath,
	}, fake.newRunner)
	if err != nil {
		t.Fatalf("runMigrateWithFactory returned error: %v", err)
	}
	if fake.config.ResumeCheckpoint == nil || fake.config.ResumeCheckpoint.Resume.NextRecordOffset != 100 {
		t.Fatalf("resume checkpoint not passed to runner: %+v", fake.config)
	}
	if fake.config.CheckpointPath != checkpointPath {
		t.Fatalf("checkpoint path should default to resume file, got %q", fake.config.CheckpointPath)
	}
}

func TestRunMigrateRejectsMissingResumeCheckpointBeforeFactory(t *testing.T) {
	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
		"--resume-from", filepath.Join(t.TempDir(), "missing.json"),
	}, fake.newRunner)
	if err == nil || !strings.Contains(err.Error(), "read vector migration checkpoint") {
		t.Fatalf("expected missing checkpoint error, got %v", err)
	}
	if fake.migrated || fake.config.Dimension != 0 {
		t.Fatalf("factory should not run on missing resume checkpoint: %+v", fake)
	}
}

func TestParseMigrateOptionsWithRecordMapping(t *testing.T) {
	options, err := parseMigrateOptions([]string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
		"--record-mapping", "/tmp/record-mapping.json",
	})
	if err != nil {
		t.Fatalf("parseMigrateOptions returned error: %v", err)
	}
	if options.RecordMappingPath != "/tmp/record-mapping.json" {
		t.Fatalf("unexpected record mapping path: %s", options.RecordMappingPath)
	}
}

func TestRunMigrateLoadsRecordMapping(t *testing.T) {
	dir := t.TempDir()
	mappingPath := filepath.Join(dir, "record-mapping.json")
	writeJSONFixture(t, mappingPath, migrateRecordMappingFixture())
	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "3",
		"--record-mapping", mappingPath,
	}, fake.newRunner)
	if err != nil {
		t.Fatalf("runMigrateWithFactory returned error: %v", err)
	}
	if fake.mapping == nil || fake.mapping.SourceCollection != "products" || fake.mapping.TargetTable != "products" {
		t.Fatalf("mapping was not passed to runner: %#v", fake.mapping)
	}
	if fake.config.SourceCollection != "products" || fake.config.TargetTable != "products" {
		t.Fatalf("migration config should use mapping collection/table: %+v", fake.config)
	}
}

func TestRunMigrateRejectsBlockingRecordMapping(t *testing.T) {
	dir := t.TempDir()
	mappingPath := filepath.Join(dir, "record-mapping.json")
	mappingPlan := migrateRecordMappingFixture()
	mappingPlan.Status = migration.RecordMappingStatusFail
	writeJSONFixture(t, mappingPath, mappingPlan)
	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "3",
		"--record-mapping", mappingPath,
	}, fake.newRunner)
	if err == nil || !strings.Contains(err.Error(), "record mapping status") {
		t.Fatalf("expected record mapping status error, got %v", err)
	}
	if fake.migrated {
		t.Fatal("migration should not run when record mapping is blocking")
	}
}

func TestParseMigrateOptionsRejectsIncompleteSchemaPreflight(t *testing.T) {
	_, err := parseMigrateOptions([]string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
		"--require-schema-match",
		"--schema-plan", "/tmp/schema-plan.json",
	})
	if err == nil || !strings.Contains(err.Error(), "live-schema") {
		t.Fatalf("expected live-schema error, got %v", err)
	}
}

func TestRunMigrateBlocksWhenSchemaPreflightFails(t *testing.T) {
	dir := t.TempDir()
	schemaPlanPath := filepath.Join(dir, "schema-plan.json")
	liveSchemaPath := filepath.Join(dir, "live-schema.json")
	writeJSONFixture(t, schemaPlanPath, appliedCompareCLISchemaPlanFixture())
	live := appliedCompareCLILiveSchemaFixture()
	live.Tables[0].Columns[1].VectorDimension = 4
	live.Tables[0].Columns[1].FormattedType = "vector(4)"
	writeJSONFixture(t, liveSchemaPath, live)

	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--source-collection", "items",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--dimension", "8",
		"--require-schema-match",
		"--schema-plan", schemaPlanPath,
		"--live-schema", liveSchemaPath,
	}, fake.newRunner)
	if err == nil || !strings.Contains(err.Error(), "schema preflight failed") {
		t.Fatalf("expected schema preflight failure, got %v", err)
	}
	if fake.migrated {
		t.Fatal("migration should not run when schema preflight fails")
	}
}

func TestRunMigrateWritesReportOutputWith0600Permissions(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "migration-report.json")
	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--source-collection", "items",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--dimension", "8",
		"--output", outputPath,
		"--job-id", "migration-smoke",
	}, fake.newRunner)
	if err != nil {
		t.Fatalf("runMigrateWithFactory returned error: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", got)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var report migration.VectorMigrationReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.JobID != "migration-smoke" || report.Summary.RecordsWritten != 100 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if strings.Contains(string(data), "postgres://") {
		t.Fatalf("report leaked connection URL: %s", data)
	}
}

func TestParseMigrateOptionsRejectsMissingRequiredFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing milvus address", args: []string{"--pgvector-connection-url", "postgres://[REDACTED]", "--dimension", "8"}, want: "milvus-address"},
		{name: "missing connection url", args: []string{"--milvus-address", "localhost:19530", "--dimension", "8"}, want: "pgvector-connection-url"},
		{name: "missing dimension", args: []string{"--milvus-address", "localhost:19530", "--pgvector-connection-url", "postgres://[REDACTED]"}, want: "dimension"},
		{name: "bad dimension", args: []string{"--milvus-address", "localhost:19530", "--pgvector-connection-url", "postgres://[REDACTED]", "--dimension", "0"}, want: "dimension"},
		{name: "bad batch", args: []string{"--milvus-address", "localhost:19530", "--pgvector-connection-url", "postgres://[REDACTED]", "--dimension", "8", "--batch-size", "0"}, want: "batch-size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMigrateOptions(tt.args)
			if err == nil {
				t.Fatal("expected invalid options to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestRunMigrateWithInjectedRunner(t *testing.T) {
	fake := &fakeMigrateRunner{}
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--source-collection", "items",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--dimension", "8",
	}, fake.newRunner)
	if err != nil {
		t.Fatalf("runMigrateWithFactory returned error: %v", err)
	}
	if fake.milvus.Address != "localhost:19530" {
		t.Fatalf("unexpected milvus config: %+v", fake.milvus)
	}
	if fake.pgvector.ConnectionURL != "postgres://[REDACTED]" {
		t.Fatalf("unexpected pgvector config: %+v", fake.pgvector)
	}
	if fake.config.SourceCollection != "items" || fake.config.TargetTable != "items" || fake.config.Dimension != 8 {
		t.Fatalf("unexpected migration config: %+v", fake.config)
	}
	if !fake.migrated {
		t.Fatal("expected runner to be executed")
	}
}

func TestRunMigratePropagatesFactoryAndRunnerErrors(t *testing.T) {
	err := runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
	}, func(connectors.MilvusConfig, connectors.PGVectorConfig, migration.VectorMigrationConfig, *migration.CollectionRecordMapping) (migrateRunner, error) {
		return nil, errors.New("factory failed")
	})
	if err == nil || !strings.Contains(err.Error(), "factory failed") {
		t.Fatalf("expected factory error, got %v", err)
	}

	fake := &fakeMigrateRunner{err: errors.New("migrate failed")}
	err = runMigrateWithFactory(context.Background(), []string{
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--dimension", "8",
	}, fake.newRunner)
	if err == nil || !strings.Contains(err.Error(), "migrate failed") {
		t.Fatalf("expected runner error, got %v", err)
	}
}

type fakeMigrateRunner struct {
	milvus   connectors.MilvusConfig
	pgvector connectors.PGVectorConfig
	config   migration.VectorMigrationConfig
	mapping  *migration.CollectionRecordMapping
	migrated bool
	err      error
}

func (f *fakeMigrateRunner) newRunner(milvus connectors.MilvusConfig, pgvector connectors.PGVectorConfig, config migration.VectorMigrationConfig, mapping *migration.CollectionRecordMapping) (migrateRunner, error) {
	f.milvus = milvus
	f.pgvector = pgvector
	f.mapping = mapping
	if mapping != nil {
		config.SourceCollection = mapping.SourceCollection
		config.TargetTable = mapping.TargetTable
	}
	f.config = config
	return f, nil
}

func migrateRecordMappingFixture() migration.RecordMappingPlan {
	return migration.RecordMappingPlan{
		SchemaVersion: migration.RecordMappingPlanVersion,
		Status:        migration.RecordMappingStatusPass,
		Mappings: []migration.CollectionRecordMapping{{
			SourceCollection: "products",
			TargetSchema:     "public",
			TargetTable:      "products",
			PrimaryKey:       &migration.RecordFieldMapping{Kind: migration.RecordMappingKindPrimaryKey, SourceField: "sku", TargetColumn: "sku", TargetType: "varchar(64)", SupportLevel: "supported"},
			Vector:           &migration.RecordFieldMapping{Kind: migration.RecordMappingKindVector, SourceField: "embedding", TargetColumn: "embedding", TargetType: "vector(3)", SupportLevel: "supported"},
			Scalars: []migration.RecordFieldMapping{
				{Kind: migration.RecordMappingKindScalar, SourceField: "title", TargetColumn: "title", TargetType: "text", SupportLevel: "supported"},
			},
			DynamicMetadata:   &migration.RecordFieldMapping{Kind: migration.RecordMappingKindDynamicMetadata, SourceField: "_milvus_dynamic", TargetColumn: "milvus_dynamic", TargetType: "jsonb", SupportLevel: "degraded"},
			PartitionMetadata: &migration.RecordFieldMapping{Kind: migration.RecordMappingKindPartitionMetadata, SourceField: "_milvus_partition", TargetColumn: "milvus_partition", TargetType: "text", SupportLevel: "degraded"},
		}},
	}
}

func (f *fakeMigrateRunner) Migrate(ctx context.Context) (migration.VectorMigrationResult, error) {
	if err := ctx.Err(); err != nil {
		return migration.VectorMigrationResult{}, err
	}
	if f.err != nil {
		return migration.VectorMigrationResult{}, f.err
	}
	f.migrated = true
	return migration.VectorMigrationResult{
		SourceCollection: f.config.SourceCollection,
		TargetTable:      f.config.TargetTable,
		Dimension:        f.config.Dimension,
		RecordsRead:      100,
		RecordsWritten:   100,
	}, nil
}
