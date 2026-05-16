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
	RecordMappingPath  string
	Mapping            *migration.CollectionRecordMapping
	CheckpointPath     string
	ResumeFromPath     string
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

func runMigrateWithFactory(ctx context.Context, args []string, factory func(connectors.MilvusConfig, connectors.PGVectorConfig, migration.VectorMigrationConfig, *migration.CollectionRecordMapping) (migrateRunner, error)) error {
	options, err := parseMigrateOptions(args)
	if err != nil {
		return err
	}
	preflightErr := runMigrateSchemaPreflight(options)
	if preflightErr != nil {
		return preflightErr
	}
	loadErr := prepareMigrateMappingAndResume(&options)
	if loadErr != nil {
		return loadErr
	}
	runner, err := factory(options.MilvusConfig, options.PGVectorConfig, options.MigrationConfig, options.Mapping)
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
		Checkpoint:        buildMigrateReportCheckpoint(options),
	})); err != nil {
		return err
	}
	return nil
}

func writeMigrateReport(path string, report migration.VectorMigrationReport) error {
	return migration.WriteVectorMigrationReport(path, report)
}

func buildMigrateReportCheckpoint(options migrateOptions) *migration.VectorMigrationReportCheckpoint {
	if options.CheckpointPath == "" && options.ResumeFromPath == "" {
		return nil
	}
	checkpoint := migration.VectorMigrationReportCheckpoint{Path: options.CheckpointPath, ResumeFrom: options.ResumeFromPath}
	if options.MigrationConfig.ResumeCheckpoint != nil {
		checkpoint.CompletedBatches = len(options.MigrationConfig.ResumeCheckpoint.CompletedBatches)
		checkpoint.FailedBatches = len(options.MigrationConfig.ResumeCheckpoint.FailedBatches)
		checkpoint.NextRecordOffset = options.MigrationConfig.ResumeCheckpoint.Resume.NextRecordOffset
	}
	return &checkpoint
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
	var recordMappingPath string
	var checkpointPath string
	var resumeFromPath string
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
	flagSet.StringVar(&recordMappingPath, "record-mapping", "", "optional map-migration-records JSON path for full-record migration")
	flagSet.StringVar(&checkpointPath, "checkpoint-path", "", "optional checkpoint JSON path for batch migration progress")
	flagSet.StringVar(&resumeFromPath, "resume-from", "", "optional checkpoint JSON path to resume from")
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
	if jobID == "" {
		jobID = "migration"
	}
	if requireSchemaMatch && schemaPlanPath == "" {
		return migrateOptions{}, errors.New("schema-plan is required when require-schema-match is set")
	}
	if requireSchemaMatch && liveSchemaPath == "" {
		return migrateOptions{}, errors.New("live-schema is required when require-schema-match is set")
	}
	if checkpointPath == "" && resumeFromPath != "" {
		checkpointPath = resumeFromPath
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
			SourceCollection:  sourceCollection,
			TargetTable:       targetTable,
			Dimension:         dimension,
			BatchSize:         batchSize,
			CheckpointPath:    checkpointPath,
			ResumeFromPath:    resumeFromPath,
			JobID:             jobID,
			SchemaPlanPath:    schemaPlanPath,
			RecordMappingPath: recordMappingPath,
		},
		RequireSchemaMatch: requireSchemaMatch,
		SchemaPlanPath:     schemaPlanPath,
		LiveSchemaPath:     liveSchemaPath,
		RecordMappingPath:  recordMappingPath,
		CheckpointPath:     checkpointPath,
		ResumeFromPath:     resumeFromPath,
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

func prepareMigrateMappingAndResume(options *migrateOptions) error {
	if options.SchemaPlanPath != "" {
		fingerprint, err := migration.FileSHA256Fingerprint(options.SchemaPlanPath)
		if err != nil {
			return fmt.Errorf("fingerprint schema plan: %w", err)
		}
		options.MigrationConfig.SchemaPlanFingerprint = fingerprint
	}
	mapping, err := loadMigrateRecordMapping(*options)
	if err != nil {
		return err
	}
	options.Mapping = mapping
	if mapping != nil {
		options.MigrationConfig.SourceCollection = mapping.SourceCollection
		options.MigrationConfig.TargetTable = mapping.TargetTable
		fingerprint, err := migration.FileSHA256Fingerprint(options.RecordMappingPath)
		if err != nil {
			return fmt.Errorf("fingerprint record mapping: %w", err)
		}
		options.MigrationConfig.RecordMappingFingerprint = fingerprint
	}
	return loadMigrateResumeCheckpoint(options)
}

func loadMigrateResumeCheckpoint(options *migrateOptions) error {
	if options.ResumeFromPath == "" {
		return nil
	}
	checkpoint, err := migration.ReadVectorMigrationCheckpoint(options.ResumeFromPath)
	if err != nil {
		return err
	}
	if err := migration.ValidateVectorMigrationResume(checkpoint, migration.VectorMigrationResumeExpectation{
		SourceCollection:         options.MigrationConfig.SourceCollection,
		TargetTable:              options.MigrationConfig.TargetTable,
		Dimension:                options.MigrationConfig.Dimension,
		BatchSize:                options.MigrationConfig.BatchSize,
		RecordMappingFingerprint: options.MigrationConfig.RecordMappingFingerprint,
		SchemaPlanFingerprint:    options.MigrationConfig.SchemaPlanFingerprint,
	}); err != nil {
		return fmt.Errorf("validate resume checkpoint: %w", err)
	}
	options.MigrationConfig.ResumeCheckpoint = &checkpoint
	return nil
}

func loadMigrateRecordMapping(options migrateOptions) (*migration.CollectionRecordMapping, error) {
	if options.RecordMappingPath == "" {
		return nil, nil
	}
	var plan migration.RecordMappingPlan
	if err := readMigrateJSONFile(options.RecordMappingPath, &plan); err != nil {
		return nil, fmt.Errorf("read record mapping: %w", err)
	}
	if plan.Status != migration.RecordMappingStatusPass {
		return nil, fmt.Errorf("record mapping status is %q", plan.Status)
	}
	if len(plan.Mappings) != 1 {
		return nil, fmt.Errorf("record mapping must contain exactly one collection, got %d", len(plan.Mappings))
	}
	collection := plan.Mappings[0]
	if collection.PrimaryKey == nil || collection.Vector == nil {
		return nil, fmt.Errorf("record mapping is missing primary key or vector mapping")
	}
	return &collection, nil
}

func readMigrateJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func newMigrateRunner(milvusConfig connectors.MilvusConfig, pgvectorConfig connectors.PGVectorConfig, migrationConfig migration.VectorMigrationConfig, recordMapping *migration.CollectionRecordMapping) (migrateRunner, error) {
	source, err := migration.NewMilvusVectorMigrationSource(milvusConfig, nil)
	if err != nil {
		return nil, err
	}
	target, err := migration.NewPGVectorMigrationTarget(pgvectorConfig, nil)
	if err != nil {
		return nil, err
	}
	if recordMapping != nil {
		source = source.WithRecordMapping(*recordMapping)
		target = target.WithRecordMapping(*recordMapping)
		migrationConfig.SourceCollection = recordMapping.SourceCollection
		migrationConfig.TargetTable = recordMapping.TargetTable
	}
	runner, err := migration.NewVectorMigrationRunner(migrationConfig, source, target)
	if err != nil {
		return nil, err
	}
	return runner, nil
}
