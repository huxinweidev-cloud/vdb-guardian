package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/h3xwave/vdb-guardian/internal/migration"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

type migrateOptions struct {
	MilvusConfig       connectors.MilvusConfig
	PGVectorConfig     connectors.PGVectorConfig
	MigrationConfig    migration.VectorMigrationConfig
	RequireSchemaMatch bool
	SchemaPlanPath     string
	LiveSchemaPath     string
	OutputPath         string
	JobID              string
}

type migrateRunner interface {
	Migrate(ctx context.Context) (migration.VectorMigrationResult, error)
}

// runMigrateCommand migrates vector records from a Milvus collection into a pgvector table.
//
// The command performs real database reads and writes. It assumes both databases
// are already running and reachable; it never starts Docker or provisions services.
//
// runMigrateCommand 将向量记录从 Milvus 集合迁移到 pgvector 数据表。
//
// 该命令会执行真实数据库读写。它假定两个数据库都已经启动并且可访问；它不会启动 Docker，
// 也不会自动创建或配置本地服务。
func runMigrateCommand(ctx context.Context, args []string) error {
	return runMigrateWithFactory(ctx, args, newMigrateRunner)
}

func runMigrateWithFactory(ctx context.Context, args []string, factory func(connectors.MilvusConfig, connectors.PGVectorConfig, migration.VectorMigrationConfig) (migrateRunner, error)) error {
	options, err := parseMigrateOptions(args)
	if err != nil {
		return err
	}
	preflightErr := runMigrateSchemaPreflight(options)
	if preflightErr != nil {
		return preflightErr
	}
	runner, err := factory(options.MilvusConfig, options.PGVectorConfig, options.MigrationConfig)
	if err != nil {
		return err
	}
	result, err := runner.Migrate(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("migration completed\n")
	fmt.Printf("source_collection: %s\n", result.SourceCollection)
	fmt.Printf("target_table: %s\n", result.TargetTable)
	fmt.Printf("dimension: %d\n", result.Dimension)
	fmt.Printf("records_read: %d\n", result.RecordsRead)
	fmt.Printf("records_written: %d\n", result.RecordsWritten)
	if err := writeMigrateReport(options.OutputPath, migration.BuildVectorMigrationReport(result, migration.VectorMigrationReportOptions{
		JobID:             options.JobID,
		SchemaPreflight:   options.RequireSchemaMatch,
		SchemaComparePath: options.SchemaPlanPath,
	})); err != nil {
		return err
	}
	return nil
}

func writeMigrateReport(path string, report migration.VectorMigrationReport) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func parseMigrateOptions(args []string) (migrateOptions, error) {
	flagSet := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var milvusAddress string
	var sourceCollection string
	var milvusIDField string
	var milvusVectorField string
	var pgvectorConnectionURL string
	var targetTable string
	var pgvectorIDColumn string
	var pgvectorVectorColumn string
	var dimension int
	var schemaPlanPath string
	var liveSchemaPath string
	var outputPath string
	var jobID string
	var requireSchemaMatch bool
	var batchSize int
	flagSet.StringVar(&milvusAddress, "milvus-address", "", "Milvus gRPC address to read source records from")
	flagSet.StringVar(&sourceCollection, "source-collection", "items", "Milvus source collection")
	flagSet.StringVar(&milvusIDField, "milvus-id-field", "id", "Milvus text primary key field name")
	flagSet.StringVar(&milvusVectorField, "milvus-vector-field", "embedding", "Milvus float vector field name")
	flagSet.StringVar(&pgvectorConnectionURL, "pgvector-connection-url", "", "PostgreSQL connection URL for pgvector target")
	flagSet.StringVar(&targetTable, "target-table", "items", "pgvector target table")
	flagSet.StringVar(&pgvectorIDColumn, "pgvector-id-column", "id", "pgvector ID column")
	flagSet.StringVar(&pgvectorVectorColumn, "pgvector-vector-column", "embedding", "pgvector vector column")
	flagSet.IntVar(&dimension, "dimension", 0, "vector dimension to validate during migration")
	flagSet.IntVar(&batchSize, "batch-size", 100, "migration batch size")
	flagSet.BoolVar(&requireSchemaMatch, "require-schema-match", false, "require planned-vs-live schema match before migration")
	flagSet.StringVar(&schemaPlanPath, "schema-plan", "", "path to pgvector schema plan JSON")
	flagSet.StringVar(&liveSchemaPath, "live-schema", "", "path to live pgvector schema inspection JSON")
	flagSet.StringVar(&outputPath, "output", "", "optional migration result report JSON output path")
	flagSet.StringVar(&jobID, "job-id", "", "optional job id for the migration report")
	if err := flagSet.Parse(args); err != nil {
		return migrateOptions{}, err
	}
	if milvusAddress == "" {
		return migrateOptions{}, errors.New("milvus-address is required")
	}
	if pgvectorConnectionURL == "" {
		return migrateOptions{}, errors.New("pgvector-connection-url is required")
	}
	if dimension <= 0 {
		return migrateOptions{}, errors.New("dimension must be positive")
	}
	if batchSize <= 0 {
		return migrateOptions{}, errors.New("batch-size must be positive")
	}
	if requireSchemaMatch && schemaPlanPath == "" {
		return migrateOptions{}, errors.New("schema-plan is required when require-schema-match is set")
	}
	if requireSchemaMatch && liveSchemaPath == "" {
		return migrateOptions{}, errors.New("live-schema is required when require-schema-match is set")
	}
	return migrateOptions{
		MilvusConfig: connectors.MilvusConfig{
			Address:           milvusAddress,
			DefaultCollection: sourceCollection,
			IDField:           milvusIDField,
			VectorField:       milvusVectorField,
		},
		PGVectorConfig: connectors.PGVectorConfig{
			ConnectionURL: pgvectorConnectionURL,
			DefaultTable:  targetTable,
			IDColumn:      pgvectorIDColumn,
			VectorColumn:  pgvectorVectorColumn,
		},
		MigrationConfig: migration.VectorMigrationConfig{
			SourceCollection: sourceCollection,
			TargetTable:      targetTable,
			Dimension:        dimension,
			BatchSize:        batchSize,
		},
		RequireSchemaMatch: requireSchemaMatch,
		SchemaPlanPath:     schemaPlanPath,
		LiveSchemaPath:     liveSchemaPath,
		OutputPath:         outputPath,
		JobID:              jobID,
	}, nil
}

func runMigrateSchemaPreflight(options migrateOptions) error {
	if !options.RequireSchemaMatch {
		return nil
	}
	var schemaPlan planschema.PGVectorSchemaPlan
	if err := readMigrateJSONFile(options.SchemaPlanPath, &schemaPlan); err != nil {
		return fmt.Errorf("read schema plan: %w", err)
	}
	var liveSchema planschema.PGVectorLiveSchemaInspection
	if err := readMigrateJSONFile(options.LiveSchemaPath, &liveSchema); err != nil {
		return fmt.Errorf("read live schema: %w", err)
	}
	report, err := planschema.CompareAppliedPGVectorSchema(schemaPlan, liveSchema, planschema.AppliedSchemaCompareOptions{})
	if err != nil {
		return fmt.Errorf("schema preflight failed: %w", err)
	}
	if report.Status == planschema.SchemaPlanCompareStatusFail {
		return fmt.Errorf("schema preflight failed: planned schema does not match live schema")
	}
	return nil
}

func readMigrateJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func newMigrateRunner(milvusConfig connectors.MilvusConfig, pgvectorConfig connectors.PGVectorConfig, migrationConfig migration.VectorMigrationConfig) (migrateRunner, error) {
	source, err := migration.NewMilvusVectorMigrationSource(milvusConfig, nil)
	if err != nil {
		return nil, err
	}
	target, err := migration.NewPGVectorMigrationTarget(pgvectorConfig, nil)
	if err != nil {
		return nil, err
	}
	runner, err := migration.NewVectorMigrationRunner(migrationConfig, source, target)
	if err != nil {
		return nil, err
	}
	return runner, nil
}
