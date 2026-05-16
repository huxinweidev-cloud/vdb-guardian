package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/h3xwave/vdb-guardian/internal/engine"
	"github.com/h3xwave/vdb-guardian/internal/jobs"
	"github.com/h3xwave/vdb-guardian/internal/migration"
	"github.com/h3xwave/vdb-guardian/internal/reporting"
)

type migrateAndVerifyOptions struct {
	FixturePath            string
	Migrate                migrateOptions
	ArtifactDir            string
	JobID                  string
	TopK                   int
	ExpandK                int
	StableK                int
	BoundaryK              int
	Metric                 string
	ResetTarget            bool
	StrictCount            bool
	MinConsistencyScore    float64
	MaxFingerprintDistance float64
	FullRecordCompare      bool
}

type migrateAndVerifyResult struct {
	Migration                migration.VectorMigrationResult
	SourceFingerprintPath    string
	TargetFingerprintPath    string
	Verification             jobs.VerificationResult
	MarkdownReportPath       string
	DiagnosticJSONReportPath string
	SourceFullRecordPath     string
	TargetFullRecordPath     string
	FullRecordComparePath    string
	FullRecordCompareErr     error
}

type migrateAndVerifySteps interface {
	ResetTarget(ctx context.Context, options migrateAndVerifyOptions) error
	Migrate(ctx context.Context, options migrateOptions) (migration.VectorMigrationResult, error)
	CountTarget(ctx context.Context, options migrateAndVerifyOptions) (int64, error)
	BuildSourceArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error
	BuildTargetArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error
	Compare(ctx context.Context, options compareArtifactsOptions, compareEngine engine.Engine) (jobs.VerificationResult, error)
	BuildSourceFullRecordArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error
	BuildTargetFullRecordArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error
	CompareFullRecords(ctx context.Context, sourcePath, targetPath, outputPath string) error
}

type realMigrateAndVerifySteps struct{}

// runMigrateAndVerifyCommand migrates records, builds source/target fingerprint artifacts, and compares them.
//
// The command composes the existing real Milvus reader, pgvector writer, artifact builders, and Python comparison
// engine. It assumes databases are already running and never starts Docker or provisions services.
//
// runMigrateAndVerifyCommand 负责执行真实迁移、构建源/目标指纹产物，并调用 Python 引擎完成一致性比对。
//
// 该命令只编排已有能力，假定数据库已经启动且可访问；它不会启动 Docker，也不会自动创建服务。
func runMigrateAndVerifyCommand(ctx context.Context, args []string) error {
	result, err := runMigrateAndVerifyWithSteps(ctx, args, realMigrateAndVerifySteps{})
	if err != nil {
		return err
	}
	fmt.Printf("migrate-and-verify completed\n")
	fmt.Printf("source_collection: %s\n", result.Migration.SourceCollection)
	fmt.Printf("target_table: %s\n", result.Migration.TargetTable)
	fmt.Printf("dimension: %d\n", result.Migration.Dimension)
	fmt.Printf("records_read: %d\n", result.Migration.RecordsRead)
	fmt.Printf("records_written: %d\n", result.Migration.RecordsWritten)
	fmt.Printf("consistency_score: %.6f\n", result.Verification.Output.ConsistencyScore)
	fmt.Printf("fingerprint_distance: %.6f\n", result.Verification.Output.Metrics.FingerprintDistance)
	fmt.Printf("matched_queries: %d\n", result.Verification.Output.Metrics.MatchedQueryCount)
	fmt.Printf("source_fingerprint: %s\n", result.SourceFingerprintPath)
	fmt.Printf("target_fingerprint: %s\n", result.TargetFingerprintPath)
	fmt.Printf("result: %s\n", result.Verification.ResultPath)
	fmt.Printf("report: %s\n", result.MarkdownReportPath)
	fmt.Printf("diagnostic_report: %s\n", result.DiagnosticJSONReportPath)
	if result.FullRecordComparePath != "" {
		fmt.Printf("source_full_records: %s\n", result.SourceFullRecordPath)
		fmt.Printf("target_full_records: %s\n", result.TargetFullRecordPath)
		fmt.Printf("full_record_compare: %s\n", result.FullRecordComparePath)
	}
	return nil
}

func runMigrateAndVerifyWithSteps(ctx context.Context, args []string, steps migrateAndVerifySteps) (migrateAndVerifyResult, error) {
	options, err := parseMigrateAndVerifyOptions(args)
	if err != nil {
		return migrateAndVerifyResult{}, err
	}
	if options.ResetTarget {
		err = steps.ResetTarget(ctx, options)
		if err != nil {
			return migrateAndVerifyResult{}, err
		}
	}
	migrationResult, err := steps.Migrate(ctx, options.Migrate)
	if err != nil {
		return migrateAndVerifyResult{}, err
	}
	strictCountErr := validateMigrateAndVerifyStrictCount(ctx, options, steps, migrationResult)
	if strictCountErr != nil {
		return migrateAndVerifyResult{}, strictCountErr
	}
	sourcePath := filepath.Join(options.ArtifactDir, options.JobID+"-source-fingerprint.json")
	targetPath := filepath.Join(options.ArtifactDir, options.JobID+"-target-fingerprint.json")
	err = steps.BuildSourceArtifact(ctx, options, sourcePath)
	if err != nil {
		return migrateAndVerifyResult{}, err
	}
	err = steps.BuildTargetArtifact(ctx, options, targetPath)
	if err != nil {
		return migrateAndVerifyResult{}, err
	}
	verification, err := steps.Compare(ctx, compareArtifactsOptions{
		SourceFingerprintPath: sourcePath,
		TargetFingerprintPath: targetPath,
		ArtifactDir:           options.ArtifactDir,
		JobID:                 options.JobID,
	}, nil)
	if err != nil {
		return migrateAndVerifyResult{}, err
	}
	fullRecordPaths, fullRecordErr := runMigrateAndVerifyFullRecordCompare(ctx, options, steps)
	reportPath := filepath.Join(options.ArtifactDir, options.JobID+"-report.md")
	if err := writeMigrateAndVerifyMarkdownReport(reportPath, reporting.MigrateAndVerifyReport{
		JobID:                    options.JobID,
		State:                    verification.State,
		Migration:                migrationResult,
		Output:                   verification.Output,
		SourceFingerprintPath:    sourcePath,
		TargetFingerprintPath:    targetPath,
		ResultPath:               verification.ResultPath,
		ResetTarget:              options.ResetTarget,
		StrictCount:              options.StrictCount,
		FullRecordCompareEnabled: options.FullRecordCompare,
		SourceFullRecordPath:     fullRecordPaths.Source,
		TargetFullRecordPath:     fullRecordPaths.Target,
		FullRecordComparePath:    fullRecordPaths.Compare,
		CheckpointPath:           options.Migrate.CheckpointPath,
		ResumeFromPath:           options.Migrate.ResumeFromPath,
	}); err != nil {
		return migrateAndVerifyResult{}, err
	}
	diagnosticReportPath := filepath.Join(options.ArtifactDir, options.JobID+"-diagnostic-report.json")
	if err := writeMigrateAndVerifyDiagnosticJSONReport(diagnosticReportPath, reporting.MigrateAndVerifyDiagnosticReport{
		MigrateAndVerifyReport: reporting.MigrateAndVerifyReport{
			JobID:                    options.JobID,
			State:                    verification.State,
			Migration:                migrationResult,
			Output:                   verification.Output,
			SourceFingerprintPath:    sourcePath,
			TargetFingerprintPath:    targetPath,
			ResultPath:               verification.ResultPath,
			ResetTarget:              options.ResetTarget,
			StrictCount:              options.StrictCount,
			FullRecordCompareEnabled: options.FullRecordCompare,
			SourceFullRecordPath:     fullRecordPaths.Source,
			TargetFullRecordPath:     fullRecordPaths.Target,
			FullRecordComparePath:    fullRecordPaths.Compare,
			CheckpointPath:           options.Migrate.CheckpointPath,
			ResumeFromPath:           options.Migrate.ResumeFromPath,
		},
		MinConsistencyScore:    options.MinConsistencyScore,
		MaxFingerprintDistance: options.MaxFingerprintDistance,
	}); err != nil {
		return migrateAndVerifyResult{}, err
	}
	if err := validateMigrateAndVerifyThresholds(options, verification, reportPath); err != nil {
		return migrateAndVerifyResult{}, err
	}
	result := migrateAndVerifyResult{
		Migration:                migrationResult,
		SourceFingerprintPath:    sourcePath,
		TargetFingerprintPath:    targetPath,
		Verification:             verification,
		MarkdownReportPath:       reportPath,
		DiagnosticJSONReportPath: diagnosticReportPath,
		SourceFullRecordPath:     fullRecordPaths.Source,
		TargetFullRecordPath:     fullRecordPaths.Target,
		FullRecordComparePath:    fullRecordPaths.Compare,
		FullRecordCompareErr:     fullRecordErr,
	}
	if fullRecordErr != nil {
		return result, fullRecordErr
	}
	return result, nil
}

func validateMigrateAndVerifyStrictCount(ctx context.Context, options migrateAndVerifyOptions, steps migrateAndVerifySteps, migrationResult migration.VectorMigrationResult) error {
	if !options.StrictCount {
		return nil
	}
	if options.Migrate.ResumeFromPath != "" {
		return errors.New("strict-count cannot be combined with resume-from")
	}
	targetCount, err := steps.CountTarget(ctx, options)
	if err != nil {
		return err
	}
	if targetCount != int64(migrationResult.RecordsWritten) {
		return fmt.Errorf("strict count mismatch: records_written=%d target_count=%d", migrationResult.RecordsWritten, targetCount)
	}
	return nil
}

type migrateAndVerifyFullRecordPaths struct {
	Source  string
	Target  string
	Compare string
}

func runMigrateAndVerifyFullRecordCompare(ctx context.Context, options migrateAndVerifyOptions, steps migrateAndVerifySteps) (migrateAndVerifyFullRecordPaths, error) {
	if !options.FullRecordCompare {
		return migrateAndVerifyFullRecordPaths{}, nil
	}
	paths := migrateAndVerifyFullRecordPaths{
		Source:  filepath.Join(options.ArtifactDir, options.JobID+"-source-full-records.json"),
		Target:  filepath.Join(options.ArtifactDir, options.JobID+"-target-full-records.json"),
		Compare: filepath.Join(options.ArtifactDir, options.JobID+"-full-record-compare.json"),
	}
	if err := steps.BuildSourceFullRecordArtifact(ctx, options, paths.Source); err != nil {
		return migrateAndVerifyFullRecordPaths{}, err
	}
	if err := steps.BuildTargetFullRecordArtifact(ctx, options, paths.Target); err != nil {
		return migrateAndVerifyFullRecordPaths{}, err
	}
	if err := steps.CompareFullRecords(ctx, paths.Source, paths.Target, paths.Compare); err != nil {
		return paths, err
	}
	return paths, nil
}

func writeMigrateAndVerifyMarkdownReport(path string, report reporting.MigrateAndVerifyReport) error {
	markdown, err := reporting.RenderMigrateAndVerifyMarkdown(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create migrate-and-verify report dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(markdown), 0o600); err != nil {
		return fmt.Errorf("write migrate-and-verify report %q: %w", path, err)
	}
	return nil
}

func writeMigrateAndVerifyDiagnosticJSONReport(path string, report reporting.MigrateAndVerifyDiagnosticReport) error {
	data, err := reporting.RenderMigrateAndVerifyDiagnosticJSON(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create migrate-and-verify diagnostic report dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write migrate-and-verify diagnostic report %q: %w", path, err)
	}
	return nil
}

func validateMigrateAndVerifyThresholds(options migrateAndVerifyOptions, verification jobs.VerificationResult, reportPath string) error {
	if verification.Output.ConsistencyScore < options.MinConsistencyScore {
		return fmt.Errorf("consistency score %.6f is below minimum %.6f; report: %s", verification.Output.ConsistencyScore, options.MinConsistencyScore, reportPath)
	}
	if verification.Output.Metrics.FingerprintDistance > options.MaxFingerprintDistance {
		return fmt.Errorf("fingerprint distance %.6f is above maximum %.6f; report: %s", verification.Output.Metrics.FingerprintDistance, options.MaxFingerprintDistance, reportPath)
	}
	return nil
}

func parseMigrateAndVerifyOptions(args []string) (migrateAndVerifyOptions, error) {
	flagSet := flag.NewFlagSet("migrate-and-verify", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var fixturePath string
	var milvusAddress string
	var sourceCollection string
	var milvusIDField string
	var milvusVectorField string
	var pgvectorConnectionURL string
	var targetTable string
	var pgvectorIDColumn string
	var pgvectorVectorColumn string
	var pgvectorWriteMode string
	var recordMappingPath string
	var checkpointPath string
	var resumeFromPath string
	var artifactDir string
	var jobID string
	var dimension int
	var batchSize int
	var topK int
	var expandK int
	var stableK int
	var boundaryK int
	var metric string
	var resetTarget bool
	var strictCount bool
	var fullRecordCompare bool
	var minConsistencyScore float64
	var maxFingerprintDistance float64
	flagSet.StringVar(&fixturePath, "fixture", "", "path to a synthetic fixture JSON file containing verification queries")
	flagSet.StringVar(&milvusAddress, "milvus-address", "", "Milvus gRPC address to read source records from")
	flagSet.StringVar(&sourceCollection, "source-collection", "items", "Milvus source collection")
	flagSet.StringVar(&milvusIDField, "milvus-id-field", "id", "Milvus text primary key field name")
	flagSet.StringVar(&milvusVectorField, "milvus-vector-field", "embedding", "Milvus float vector field name")
	flagSet.StringVar(&pgvectorConnectionURL, "pgvector-connection-url", "", "PostgreSQL connection URL for pgvector target")
	flagSet.StringVar(&targetTable, "target-table", "items", "pgvector target table")
	flagSet.StringVar(&pgvectorIDColumn, "pgvector-id-column", "id", "pgvector ID column")
	flagSet.StringVar(&pgvectorVectorColumn, "pgvector-vector-column", "embedding", "pgvector vector column")
	flagSet.StringVar(&pgvectorWriteMode, "pgvector-write-mode", "batch-upsert", "pgvector write mode: batch-upsert, copy, or auto")
	flagSet.StringVar(&recordMappingPath, "record-mapping", "", "optional map-migration-records JSON path for full-record migration")
	flagSet.StringVar(&checkpointPath, "checkpoint-path", "", "optional checkpoint JSON path for batch migration progress")
	flagSet.StringVar(&resumeFromPath, "resume-from", "", "optional checkpoint JSON path to resume from")
	flagSet.StringVar(&artifactDir, "artifact-dir", "", "directory to write source, target, and comparison artifacts")
	flagSet.StringVar(&jobID, "job-id", "migrate-and-verify", "job id used for artifact filenames")
	flagSet.IntVar(&dimension, "dimension", 0, "vector dimension to validate during migration")
	flagSet.IntVar(&batchSize, "batch-size", 100, "migration batch size")
	flagSet.IntVar(&topK, "top-k", 3, "business-visible topK result count")
	flagSet.IntVar(&expandK, "expand-k", 5, "expanded result count for boundary artifact building")
	flagSet.IntVar(&stableK, "stable-k", 2, "leading hit count for stable_neighbors")
	flagSet.IntVar(&boundaryK, "boundary-k", 1, "rank-window width around the topK cutoff")
	flagSet.StringVar(&metric, "metric", connectors.MilvusMetricCosine, "metric for both Milvus and pgvector artifact searches: cosine or l2")
	flagSet.BoolVar(&resetTarget, "reset-target", false, "truncate the pgvector target table before migration to avoid stale-row verification")
	flagSet.BoolVar(&strictCount, "strict-count", false, "fail when pgvector target row count does not equal records written after migration")
	flagSet.BoolVar(&fullRecordCompare, "full-record-compare", false, "build live full-record artifacts and compare source/target record equality after migration")
	flagSet.Float64Var(&minConsistencyScore, "min-consistency-score", 0, "fail when consistency_score is below this threshold")
	flagSet.Float64Var(&maxFingerprintDistance, "max-fingerprint-distance", 1, "fail when fingerprint_distance is above this threshold")
	if err := flagSet.Parse(args); err != nil {
		return migrateAndVerifyOptions{}, err
	}
	if strictCount && resumeFromPath != "" {
		return migrateAndVerifyOptions{}, errors.New("strict-count cannot be combined with resume-from")
	}
	if err := validateMigrateAndVerifyFields(fixturePath, milvusAddress, pgvectorConnectionURL, artifactDir, jobID, dimension, batchSize, topK, expandK, stableK, boundaryK, metric); err != nil {
		return migrateAndVerifyOptions{}, err
	}
	if err := validatePGVectorWriteModeFlag(pgvectorWriteMode); err != nil {
		return migrateAndVerifyOptions{}, err
	}
	if err := validateMigrateAndVerifyThresholdFields(minConsistencyScore, maxFingerprintDistance); err != nil {
		return migrateAndVerifyOptions{}, err
	}
	if fullRecordCompare && recordMappingPath == "" {
		return migrateAndVerifyOptions{}, errors.New("record-mapping is required when full-record-compare is enabled")
	}
	if checkpointPath == "" && resumeFromPath != "" {
		checkpointPath = resumeFromPath
	}
	if resetTarget && resumeFromPath != "" {
		return migrateAndVerifyOptions{}, errors.New("reset-target cannot be combined with resume-from")
	}
	return migrateAndVerifyOptions{
		FixturePath: fixturePath,
		Migrate: migrateOptions{
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
				WriteMode:     pgvectorWriteMode,
			},
			RecordMappingPath: recordMappingPath,
			CheckpointPath:    checkpointPath,
			ResumeFromPath:    resumeFromPath,
			MigrationConfig: migration.VectorMigrationConfig{
				SourceCollection:   sourceCollection,
				TargetTable:        targetTable,
				Dimension:          dimension,
				BatchSize:          batchSize,
				WriteModeRequested: pgvectorWriteMode,
				CheckpointPath:     checkpointPath,
				ResumeFromPath:     resumeFromPath,
				JobID:              jobID,
				RecordMappingPath:  recordMappingPath,
			},
		},
		ArtifactDir:            artifactDir,
		JobID:                  jobID,
		TopK:                   topK,
		ExpandK:                expandK,
		StableK:                stableK,
		BoundaryK:              boundaryK,
		Metric:                 metric,
		ResetTarget:            resetTarget,
		StrictCount:            strictCount,
		MinConsistencyScore:    minConsistencyScore,
		MaxFingerprintDistance: maxFingerprintDistance,
		FullRecordCompare:      fullRecordCompare,
	}, nil
}

func (realMigrateAndVerifySteps) ResetTarget(ctx context.Context, options migrateAndVerifyOptions) error {
	target, err := migration.NewPGVectorMigrationTarget(options.Migrate.PGVectorConfig, nil)
	if err != nil {
		return err
	}
	return target.ResetRecords(ctx, options.Migrate.MigrationConfig.TargetTable)
}

func (realMigrateAndVerifySteps) Migrate(ctx context.Context, options migrateOptions) (migration.VectorMigrationResult, error) {
	if err := prepareMigrateMappingAndResume(&options); err != nil {
		return migration.VectorMigrationResult{}, err
	}
	runner, err := newMigrateRunner(options.MilvusConfig, options.PGVectorConfig, options.MigrationConfig, options.Mapping)
	if err != nil {
		return migration.VectorMigrationResult{}, err
	}
	return runner.Migrate(ctx)
}

func (realMigrateAndVerifySteps) CountTarget(ctx context.Context, options migrateAndVerifyOptions) (int64, error) {
	connector, err := connectors.NewPGVectorConnector(options.Migrate.PGVectorConfig, nil)
	if err != nil {
		return 0, err
	}
	defer connector.Close()
	return connector.Count(ctx, options.Migrate.MigrationConfig.TargetTable)
}

func (realMigrateAndVerifySteps) BuildSourceArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error {
	return runMilvusArtifactWithFactory(ctx, []string{
		"--fixture", options.FixturePath,
		"--address", options.Migrate.MilvusConfig.Address,
		"--output", outputPath,
		"--collection", options.Migrate.MigrationConfig.SourceCollection,
		"--id-field", options.Migrate.MilvusConfig.IDField,
		"--vector-field", options.Migrate.MilvusConfig.VectorField,
		"--top-k", fmt.Sprintf("%d", options.TopK),
		"--expand-k", fmt.Sprintf("%d", options.ExpandK),
		"--stable-k", fmt.Sprintf("%d", options.StableK),
		"--boundary-k", fmt.Sprintf("%d", options.BoundaryK),
		"--metric", options.Metric,
	}, newMilvusSearchConnector)
}

func (realMigrateAndVerifySteps) BuildTargetArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error {
	return runPGVectorArtifactWithFactory(ctx, []string{
		"--fixture", options.FixturePath,
		"--connection-url", options.Migrate.PGVectorConfig.ConnectionURL,
		"--output", outputPath,
		"--table", options.Migrate.MigrationConfig.TargetTable,
		"--top-k", fmt.Sprintf("%d", options.TopK),
		"--expand-k", fmt.Sprintf("%d", options.ExpandK),
		"--stable-k", fmt.Sprintf("%d", options.StableK),
		"--boundary-k", fmt.Sprintf("%d", options.BoundaryK),
		"--metric", options.Metric,
	}, newPGVectorSearchConnector)
}

func (realMigrateAndVerifySteps) Compare(ctx context.Context, options compareArtifactsOptions, compareEngine engine.Engine) (jobs.VerificationResult, error) {
	if compareEngine == nil {
		pythonPath, pythonWorkDir, err := discoverPythonEngine()
		if err != nil {
			return jobs.VerificationResult{}, err
		}
		compareEngine = engine.NewPythonRunner(pythonPath, pythonWorkDir)
	}
	return runCompareArtifacts(ctx, options, compareEngine)
}

func (realMigrateAndVerifySteps) BuildSourceFullRecordArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error {
	return runBuildMilvusRecordArtifactCommand(ctx, []string{
		"--milvus-address", options.Migrate.MilvusConfig.Address,
		"--record-mapping", options.Migrate.RecordMappingPath,
		"--output", outputPath,
	})
}

func (realMigrateAndVerifySteps) BuildTargetFullRecordArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error {
	return runBuildPGVectorRecordArtifactCommand(ctx, []string{
		"--pgvector-connection-url", options.Migrate.PGVectorConfig.ConnectionURL,
		"--record-mapping", options.Migrate.RecordMappingPath,
		"--output", outputPath,
	})
}

func (realMigrateAndVerifySteps) CompareFullRecords(ctx context.Context, sourcePath, targetPath, outputPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return runCompareFullRecordsCommand([]string{
		"--source", sourcePath,
		"--target", targetPath,
		"--output", outputPath,
	})
}

func validateMigrateAndVerifyThresholdFields(minConsistencyScore, maxFingerprintDistance float64) error {
	if minConsistencyScore < 0 || minConsistencyScore > 1 {
		return errors.New("min-consistency-score must be between 0 and 1")
	}
	if maxFingerprintDistance < 0 || maxFingerprintDistance > 1 {
		return errors.New("max-fingerprint-distance must be between 0 and 1")
	}
	return nil
}

func validateMigrateAndVerifyFields(fixturePath, milvusAddress, pgvectorConnectionURL, artifactDir, jobID string, dimension, batchSize, topK, expandK, stableK, boundaryK int, metric string) error {
	if fixturePath == "" {
		return errors.New("fixture path is required")
	}
	if milvusAddress == "" {
		return errors.New("milvus-address is required")
	}
	if pgvectorConnectionURL == "" {
		return errors.New("pgvector-connection-url is required")
	}
	if artifactDir == "" {
		return errors.New("artifact-dir is required")
	}
	if jobID == "" {
		return errors.New("job-id is required")
	}
	if dimension <= 0 {
		return errors.New("dimension must be positive")
	}
	if batchSize <= 0 {
		return errors.New("batch-size must be positive")
	}
	if topK <= 0 {
		return errors.New("top-k must be positive")
	}
	if stableK <= 0 || stableK > topK {
		return errors.New("stable-k must be positive and less than or equal to top-k")
	}
	if boundaryK <= 0 {
		return errors.New("boundary-k must be positive")
	}
	if expandK < topK+boundaryK {
		return errors.New("expand-k must be greater than or equal to top-k plus boundary-k")
	}
	if metric != connectors.MilvusMetricCosine && metric != connectors.MilvusMetricL2 {
		return fmt.Errorf("unsupported metric %q", metric)
	}
	return nil
}
