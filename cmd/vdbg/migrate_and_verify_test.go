package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/engine"
	"github.com/h3xwave/vdb-guardian/internal/jobs"
	"github.com/h3xwave/vdb-guardian/internal/migration"
)

func TestParseMigrateAndVerifyOptions(t *testing.T) {
	options, err := parseMigrateAndVerifyOptions([]string{
		"--fixture", "testdata/migration/synthetic-small.json",
		"--milvus-address", "localhost:19530",
		"--source-collection", "source_items",
		"--milvus-id-field", "vector_id",
		"--milvus-vector-field", "embedding",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "target_items",
		"--pgvector-id-column", "vector_id",
		"--pgvector-vector-column", "embedding",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--job-id", "mv-smoke",
		"--dimension", "8",
		"--batch-size", "25",
		"--top-k", "3",
		"--expand-k", "5",
		"--stable-k", "2",
		"--boundary-k", "1",
		"--metric", "l2",
	})
	if err != nil {
		t.Fatalf("parseMigrateAndVerifyOptions returned error: %v", err)
	}
	if options.FixturePath != "testdata/migration/synthetic-small.json" {
		t.Fatalf("unexpected fixture path: %s", options.FixturePath)
	}
	if options.Migrate.MilvusConfig.Address != "localhost:19530" {
		t.Fatalf("unexpected milvus address: %s", options.Migrate.MilvusConfig.Address)
	}
	if options.Migrate.MigrationConfig.SourceCollection != "source_items" || options.Migrate.MigrationConfig.TargetTable != "target_items" {
		t.Fatalf("unexpected migration config: %+v", options.Migrate.MigrationConfig)
	}
	if options.Migrate.PGVectorConfig.ConnectionURL != "postgres://[REDACTED]" {
		t.Fatalf("unexpected connection URL: %s", options.Migrate.PGVectorConfig.ConnectionURL)
	}
	if options.ArtifactDir != "/tmp/vdb-guardian-run" || options.JobID != "mv-smoke" {
		t.Fatalf("unexpected artifact options: %+v", options)
	}
	if options.TopK != 3 || options.ExpandK != 5 || options.StableK != 2 || options.BoundaryK != 1 {
		t.Fatalf("unexpected fingerprint options: %+v", options)
	}
	if options.Metric != "l2" {
		t.Fatalf("unexpected metric: %s", options.Metric)
	}
}

func TestParseMigrateAndVerifyOptionsParsesResetTarget(t *testing.T) {
	options, err := parseMigrateAndVerifyOptions([]string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--dimension", "8",
		"--reset-target",
	})
	if err != nil {
		t.Fatalf("parseMigrateAndVerifyOptions returned error: %v", err)
	}
	if !options.ResetTarget {
		t.Fatal("expected reset-target flag to enable target reset")
	}
}

func TestParseMigrateAndVerifyOptionsParsesRecordMapping(t *testing.T) {
	options, err := parseMigrateAndVerifyOptions([]string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", t.TempDir(),
		"--dimension", "8",
		"--record-mapping", "/tmp/record-mapping.json",
	})
	if err != nil {
		t.Fatalf("parseMigrateAndVerifyOptions returned error: %v", err)
	}
	if options.Migrate.RecordMappingPath != "/tmp/record-mapping.json" {
		t.Fatalf("record mapping path = %q", options.Migrate.RecordMappingPath)
	}
}

func TestParseMigrateAndVerifyOptionsParsesStrictCount(t *testing.T) {
	options, err := parseMigrateAndVerifyOptions([]string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--dimension", "8",
		"--strict-count",
	})
	if err != nil {
		t.Fatalf("parseMigrateAndVerifyOptions returned error: %v", err)
	}
	if !options.StrictCount {
		t.Fatal("expected strict-count flag to enable count validation")
	}
}

func TestParseMigrateAndVerifyOptionsParsesThresholds(t *testing.T) {
	options, err := parseMigrateAndVerifyOptions([]string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--dimension", "8",
		"--min-consistency-score", "0.999",
		"--max-fingerprint-distance", "0.001",
	})
	if err != nil {
		t.Fatalf("parseMigrateAndVerifyOptions returned error: %v", err)
	}
	if options.MinConsistencyScore != 0.999 || options.MaxFingerprintDistance != 0.001 {
		t.Fatalf("unexpected thresholds: %+v", options)
	}
}

func TestParseMigrateAndVerifyOptionsRejectsMissingRequiredFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing fixture", args: []string{"--milvus-address", "localhost:19530", "--pgvector-connection-url", "postgres://[REDACTED]", "--artifact-dir", "/tmp/out", "--dimension", "8"}, want: "fixture"},
		{name: "missing milvus address", args: []string{"--fixture", "fixture.json", "--pgvector-connection-url", "postgres://[REDACTED]", "--artifact-dir", "/tmp/out", "--dimension", "8"}, want: "milvus-address"},
		{name: "missing connection url", args: []string{"--fixture", "fixture.json", "--milvus-address", "localhost:19530", "--artifact-dir", "/tmp/out", "--dimension", "8"}, want: "pgvector-connection-url"},
		{name: "missing artifact dir", args: []string{"--fixture", "fixture.json", "--milvus-address", "localhost:19530", "--pgvector-connection-url", "postgres://[REDACTED]", "--dimension", "8"}, want: "artifact-dir"},
		{name: "bad dimension", args: []string{"--fixture", "fixture.json", "--milvus-address", "localhost:19530", "--pgvector-connection-url", "postgres://[REDACTED]", "--artifact-dir", "/tmp/out", "--dimension", "0"}, want: "dimension"},
		{name: "bad expand", args: []string{"--fixture", "fixture.json", "--milvus-address", "localhost:19530", "--pgvector-connection-url", "postgres://[REDACTED]", "--artifact-dir", "/tmp/out", "--dimension", "8", "--top-k", "3", "--expand-k", "3", "--boundary-k", "1"}, want: "expand-k"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMigrateAndVerifyOptions(tt.args)
			if err == nil {
				t.Fatal("expected invalid options to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestParseMigrateAndVerifyOptionsRejectsInvalidThresholds(t *testing.T) {
	baseArgs := []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--dimension", "8",
	}
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "min consistency below zero",
			args: append(append([]string{}, baseArgs...), "--min-consistency-score", "-0.1"),
		},
		{
			name: "max fingerprint distance above one",
			args: append(append([]string{}, baseArgs...), "--max-fingerprint-distance", "1.1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseMigrateAndVerifyOptions(tt.args); err == nil {
				t.Fatal("expected invalid threshold to be rejected")
			}
		})
	}
}

func TestRunMigrateAndVerifyWithInjectedSteps(t *testing.T) {
	artifactDir := t.TempDir()
	fake := &fakeMigrateAndVerifySteps{}
	result, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--source-collection", "items",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--artifact-dir", artifactDir,
		"--job-id", "mv-smoke",
		"--dimension", "8",
	}, fake)
	if err != nil {
		t.Fatalf("runMigrateAndVerifyWithSteps returned error: %v", err)
	}
	if !fake.migrated || !fake.sourceBuilt || !fake.targetBuilt || !fake.compared {
		t.Fatalf("expected all steps to run, got %+v", fake)
	}
	if result.Migration.RecordsRead != 100 || result.Migration.RecordsWritten != 100 {
		t.Fatalf("unexpected migration result: %+v", result.Migration)
	}
	if result.SourceFingerprintPath != filepath.Join(artifactDir, "mv-smoke-source-fingerprint.json") {
		t.Fatalf("unexpected source fingerprint path: %s", result.SourceFingerprintPath)
	}
	if result.TargetFingerprintPath != filepath.Join(artifactDir, "mv-smoke-target-fingerprint.json") {
		t.Fatalf("unexpected target fingerprint path: %s", result.TargetFingerprintPath)
	}
	if result.MarkdownReportPath != filepath.Join(artifactDir, "mv-smoke-report.md") {
		t.Fatalf("unexpected markdown report path: %s", result.MarkdownReportPath)
	}
	if result.DiagnosticJSONReportPath != filepath.Join(artifactDir, "mv-smoke-diagnostic-report.json") {
		t.Fatalf("unexpected diagnostic JSON report path: %s", result.DiagnosticJSONReportPath)
	}
	reportData, err := os.ReadFile(result.MarkdownReportPath)
	if err != nil {
		t.Fatalf("expected markdown report to be written: %v", err)
	}
	if !strings.Contains(string(reportData), "# vdb-guardian migrate-and-verify report") {
		t.Fatalf("unexpected markdown report contents:\n%s", string(reportData))
	}
	diagnosticReportData, err := os.ReadFile(result.DiagnosticJSONReportPath)
	if err != nil {
		t.Fatalf("expected diagnostic JSON report to be written: %v", err)
	}
	if !strings.Contains(string(diagnosticReportData), "\"schema_version\": \"v1\"") || !strings.Contains(string(diagnosticReportData), "\"job_id\": \"mv-smoke\"") {
		t.Fatalf("unexpected diagnostic JSON report contents:\n%s", string(diagnosticReportData))
	}
	if result.Verification.Output.ConsistencyScore != 1.0 {
		t.Fatalf("unexpected consistency score: %+v", result.Verification.Output)
	}
}

func TestRunMigrateAndVerifyResetsTargetBeforeMigration(t *testing.T) {
	fake := &fakeMigrateAndVerifySteps{}
	_, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--dimension", "8",
		"--reset-target",
	}, fake)
	if err != nil {
		t.Fatalf("runMigrateAndVerifyWithSteps returned error: %v", err)
	}
	want := []string{"reset-target", "migrate", "build-source", "build-target", "compare"}
	if strings.Join(fake.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected call order: got %v want %v", fake.calls, want)
	}
}

func TestRunMigrateAndVerifyDoesNotResetTargetByDefault(t *testing.T) {
	fake := &fakeMigrateAndVerifySteps{}
	_, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--dimension", "8",
	}, fake)
	if err != nil {
		t.Fatalf("runMigrateAndVerifyWithSteps returned error: %v", err)
	}
	if fake.resetTarget {
		t.Fatal("expected reset-target step to be skipped by default")
	}
}

func TestRunMigrateAndVerifyValidatesTargetCountWhenStrictCountEnabled(t *testing.T) {
	fake := &fakeMigrateAndVerifySteps{targetCount: 100}
	_, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", t.TempDir(),
		"--dimension", "8",
		"--strict-count",
	}, fake)
	if err != nil {
		t.Fatalf("runMigrateAndVerifyWithSteps returned error: %v", err)
	}
	want := []string{"migrate", "count-target", "build-source", "build-target", "compare"}
	if strings.Join(fake.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected call order: got %v want %v", fake.calls, want)
	}
}

func TestRunMigrateAndVerifyFailsWhenStrictCountMismatches(t *testing.T) {
	fake := &fakeMigrateAndVerifySteps{targetCount: 99}
	_, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", t.TempDir(),
		"--dimension", "8",
		"--strict-count",
	}, fake)
	if err == nil || !strings.Contains(err.Error(), "strict count") {
		t.Fatalf("expected strict count mismatch error, got %v", err)
	}
	if fake.sourceBuilt || fake.targetBuilt || fake.compared {
		t.Fatalf("expected artifact and compare steps to be skipped, got %+v", fake)
	}
}

func TestRunMigrateAndVerifyFailsWhenConsistencyScoreBelowThreshold(t *testing.T) {
	artifactDir := t.TempDir()
	fake := &fakeMigrateAndVerifySteps{consistencyScore: 0.95}
	_, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", artifactDir,
		"--dimension", "8",
		"--min-consistency-score", "0.99",
	}, fake)
	if err == nil || !strings.Contains(err.Error(), "consistency score") {
		t.Fatalf("expected consistency score threshold error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(artifactDir, "migrate-and-verify-report.md")); statErr != nil {
		t.Fatalf("expected report to be written before threshold failure: %v", statErr)
	}
}

func TestRunMigrateAndVerifyFailsWhenFingerprintDistanceAboveThreshold(t *testing.T) {
	fake := &fakeMigrateAndVerifySteps{consistencyScore: 1.0, fingerprintDistance: 0.02}
	_, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", t.TempDir(),
		"--dimension", "8",
		"--max-fingerprint-distance", "0.01",
	}, fake)
	if err == nil || !strings.Contains(err.Error(), "fingerprint distance") {
		t.Fatalf("expected fingerprint distance threshold error, got %v", err)
	}
}

func TestRunMigrateAndVerifyStopsWhenResetTargetFails(t *testing.T) {
	fake := &fakeMigrateAndVerifySteps{resetErr: errors.New("reset failed")}
	_, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--dimension", "8",
		"--reset-target",
	}, fake)
	if err == nil || !strings.Contains(err.Error(), "reset failed") {
		t.Fatalf("expected reset error, got %v", err)
	}
	if fake.migrated || fake.sourceBuilt || fake.targetBuilt || fake.compared {
		t.Fatalf("expected later steps to be skipped, got %+v", fake)
	}
}

func TestRunMigrateAndVerifyStopsOnStepError(t *testing.T) {
	fake := &fakeMigrateAndVerifySteps{migrateErr: errors.New("migrate failed")}
	_, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--artifact-dir", "/tmp/vdb-guardian-run",
		"--dimension", "8",
	}, fake)
	if err == nil || !strings.Contains(err.Error(), "migrate failed") {
		t.Fatalf("expected migrate error, got %v", err)
	}
	if fake.sourceBuilt || fake.targetBuilt || fake.compared {
		t.Fatalf("expected later steps to be skipped, got %+v", fake)
	}
}

type fakeMigrateAndVerifySteps struct {
	calls               []string
	resetTarget         bool
	migrated            bool
	sourceBuilt         bool
	targetBuilt         bool
	compared            bool
	targetCount         int64
	resetErr            error
	migrateErr          error
	consistencyScore    float64
	fingerprintDistance float64
}

func (f *fakeMigrateAndVerifySteps) ResetTarget(ctx context.Context, options migrateAndVerifyOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.resetErr != nil {
		return f.resetErr
	}
	f.calls = append(f.calls, "reset-target")
	f.resetTarget = true
	return nil
}

func (f *fakeMigrateAndVerifySteps) Migrate(ctx context.Context, options migrateOptions) (migration.VectorMigrationResult, error) {
	if err := ctx.Err(); err != nil {
		return migration.VectorMigrationResult{}, err
	}
	if f.migrateErr != nil {
		return migration.VectorMigrationResult{}, f.migrateErr
	}
	f.calls = append(f.calls, "migrate")
	f.migrated = true
	return migration.VectorMigrationResult{
		SourceCollection: options.MigrationConfig.SourceCollection,
		TargetTable:      options.MigrationConfig.TargetTable,
		Dimension:        options.MigrationConfig.Dimension,
		RecordsRead:      100,
		RecordsWritten:   100,
	}, nil
}

func (f *fakeMigrateAndVerifySteps) CountTarget(ctx context.Context, options migrateAndVerifyOptions) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.calls = append(f.calls, "count-target")
	return f.targetCount, nil
}

func (f *fakeMigrateAndVerifySteps) BuildSourceArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.calls = append(f.calls, "build-source")
	f.sourceBuilt = strings.HasSuffix(outputPath, "-source-fingerprint.json")
	return nil
}

func (f *fakeMigrateAndVerifySteps) BuildTargetArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.calls = append(f.calls, "build-target")
	f.targetBuilt = strings.HasSuffix(outputPath, "-target-fingerprint.json")
	return nil
}

func (f *fakeMigrateAndVerifySteps) Compare(ctx context.Context, options compareArtifactsOptions, compareEngine engine.Engine) (jobs.VerificationResult, error) {
	if err := ctx.Err(); err != nil {
		return jobs.VerificationResult{}, err
	}
	f.calls = append(f.calls, "compare")
	f.compared = true
	consistencyScore := f.consistencyScore
	if consistencyScore == 0 {
		consistencyScore = 1.0
	}
	return jobs.VerificationResult{
		JobID:      options.JobID,
		State:      jobs.StateSucceeded,
		ResultPath: options.ArtifactDir + "/" + options.JobID + "-result.json",
		Output: engine.CompareOutput{
			ConsistencyScore: consistencyScore,
			Metrics: engine.MetricSummary{
				FingerprintDistance: f.fingerprintDistance,
				MatchedQueryCount:   10,
			},
		},
	}, nil
}
