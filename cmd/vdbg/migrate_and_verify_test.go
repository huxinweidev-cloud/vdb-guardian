package main

import (
	"context"
	"errors"
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

func TestRunMigrateAndVerifyWithInjectedSteps(t *testing.T) {
	fake := &fakeMigrateAndVerifySteps{}
	result, err := runMigrateAndVerifyWithSteps(context.Background(), []string{
		"--fixture", "fixture.json",
		"--milvus-address", "localhost:19530",
		"--source-collection", "items",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--artifact-dir", "/tmp/vdb-guardian-run",
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
	if result.SourceFingerprintPath != filepath.Join("/tmp/vdb-guardian-run", "mv-smoke-source-fingerprint.json") {
		t.Fatalf("unexpected source fingerprint path: %s", result.SourceFingerprintPath)
	}
	if result.TargetFingerprintPath != filepath.Join("/tmp/vdb-guardian-run", "mv-smoke-target-fingerprint.json") {
		t.Fatalf("unexpected target fingerprint path: %s", result.TargetFingerprintPath)
	}
	if result.Verification.Output.ConsistencyScore != 1.0 {
		t.Fatalf("unexpected consistency score: %+v", result.Verification.Output)
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
	migrated    bool
	sourceBuilt bool
	targetBuilt bool
	compared    bool
	migrateErr  error
}

func (f *fakeMigrateAndVerifySteps) Migrate(ctx context.Context, options migrateOptions) (migration.VectorMigrationResult, error) {
	if err := ctx.Err(); err != nil {
		return migration.VectorMigrationResult{}, err
	}
	if f.migrateErr != nil {
		return migration.VectorMigrationResult{}, f.migrateErr
	}
	f.migrated = true
	return migration.VectorMigrationResult{
		SourceCollection: options.MigrationConfig.SourceCollection,
		TargetTable:      options.MigrationConfig.TargetTable,
		Dimension:        options.MigrationConfig.Dimension,
		RecordsRead:      100,
		RecordsWritten:   100,
	}, nil
}

func (f *fakeMigrateAndVerifySteps) BuildSourceArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.sourceBuilt = strings.HasSuffix(outputPath, "-source-fingerprint.json")
	return nil
}

func (f *fakeMigrateAndVerifySteps) BuildTargetArtifact(ctx context.Context, options migrateAndVerifyOptions, outputPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.targetBuilt = strings.HasSuffix(outputPath, "-target-fingerprint.json")
	return nil
}

func (f *fakeMigrateAndVerifySteps) Compare(ctx context.Context, options compareArtifactsOptions, compareEngine engine.Engine) (jobs.VerificationResult, error) {
	if err := ctx.Err(); err != nil {
		return jobs.VerificationResult{}, err
	}
	f.compared = true
	return jobs.VerificationResult{
		JobID:      options.JobID,
		ResultPath: options.ArtifactDir + "/" + options.JobID + "-result.json",
		Output: engine.CompareOutput{
			ConsistencyScore: 1.0,
			Metrics: engine.MetricSummary{
				FingerprintDistance: 0.0,
				MatchedQueryCount:   10,
			},
		},
	}, nil
}
