package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/fixtures"
	"github.com/h3xwave/vdb-guardian/internal/migration"
)

func TestParseSeedPGVectorOptions(t *testing.T) {
	options, err := parseSeedPGVectorOptions([]string{
		"--fixture", "fixture.json",
		"--connection-url", "postgres://[REDACTED]",
		"--table", "vectors",
		"--id-column", "vector_id",
		"--vector-column", "embedding",
	})
	if err != nil {
		t.Fatalf("parseSeedPGVectorOptions returned error: %v", err)
	}
	if options.FixturePath != "fixture.json" {
		t.Fatalf("unexpected fixture path: %s", options.FixturePath)
	}
	if options.ConnectionURL != "postgres://[REDACTED]" {
		t.Fatalf("unexpected connection URL: %s", options.ConnectionURL)
	}
	if options.SeederConfig.Table != "vectors" {
		t.Fatalf("unexpected table: %s", options.SeederConfig.Table)
	}
	if options.SeederConfig.IDColumn != "vector_id" {
		t.Fatalf("unexpected id column: %s", options.SeederConfig.IDColumn)
	}
	if options.SeederConfig.VectorColumn != "embedding" {
		t.Fatalf("unexpected vector column: %s", options.SeederConfig.VectorColumn)
	}
}

func TestParseSeedPGVectorOptionsRejectsMissingRequiredFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing fixture", args: []string{"--connection-url", "postgres://[REDACTED]"}, want: "fixture"},
		{name: "missing connection url", args: []string{"--fixture", "fixture.json"}, want: "connection-url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSeedPGVectorOptions(tt.args)
			if err == nil {
				t.Fatalf("expected missing option to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestLoadSyntheticDatasetFile(t *testing.T) {
	path := writeSeedPGVectorFixture(t, syntheticSeedPGVectorDataset())
	dataset, err := loadSyntheticDatasetFile(path)
	if err != nil {
		t.Fatalf("loadSyntheticDatasetFile returned error: %v", err)
	}
	if dataset.Dimension != 3 || len(dataset.Records) != 2 {
		t.Fatalf("unexpected dataset: %+v", dataset)
	}
}

func TestRunSeedPGVectorWithInjectedSeeder(t *testing.T) {
	fixturePath := writeSeedPGVectorFixture(t, syntheticSeedPGVectorDataset())
	fake := &fakeSeedPGVectorRunner{}
	err := runSeedPGVectorWithFactory(context.Background(), []string{
		"--fixture", fixturePath,
		"--connection-url", "postgres://[REDACTED]",
		"--table", "items",
	}, fake.newSeeder)
	if err != nil {
		t.Fatalf("runSeedPGVectorWithFactory returned error: %v", err)
	}
	if fake.connectionURL != "postgres://[REDACTED]" {
		t.Fatalf("unexpected connection URL: %s", fake.connectionURL)
	}
	if fake.config.Dimension != 3 {
		t.Fatalf("expected dimension from dataset, got %d", fake.config.Dimension)
	}
	if fake.seeded.Dimension != 3 || len(fake.seeded.Records) != 2 {
		t.Fatalf("unexpected seeded dataset: %+v", fake.seeded)
	}
}

func TestRunSeedPGVectorRejectsInvalidFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	err := runSeedPGVectorWithFactory(context.Background(), []string{
		"--fixture", path,
		"--connection-url", "postgres://[REDACTED]",
	}, func(string, migration.PGVectorSeederConfig) (pgvectorSeedRunner, error) {
		t.Fatalf("factory should not be called for invalid fixture")
		return nil, nil
	})
	if err == nil {
		t.Fatalf("expected invalid fixture to fail")
	}
}

func writeSeedPGVectorFixture(t *testing.T, dataset fixtures.SyntheticDataset) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.json")
	content, err := json.Marshal(dataset)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func syntheticSeedPGVectorDataset() fixtures.SyntheticDataset {
	return fixtures.SyntheticDataset{
		Seed:        42,
		Dimension:   3,
		RecordCount: 2,
		QueryCount:  1,
		Metric:      fixtures.MetricCosine,
		Records: []fixtures.SyntheticVector{
			{ID: "vec-000001", Vector: []float64{0.1, 0.2, 0.3}},
			{ID: "vec-000002", Vector: []float64{0.4, 0.5, 0.6}},
		},
		Queries: []fixtures.SyntheticVector{
			{ID: "query-000001", Vector: []float64{0.7, 0.8, 0.9}},
		},
	}
}

type fakeSeedPGVectorRunner struct {
	connectionURL string
	config        migration.PGVectorSeederConfig
	seeded        fixtures.SyntheticDataset
}

func (f *fakeSeedPGVectorRunner) newSeeder(connectionURL string, config migration.PGVectorSeederConfig) (pgvectorSeedRunner, error) {
	f.connectionURL = connectionURL
	f.config = config
	return f, nil
}

func (f *fakeSeedPGVectorRunner) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (migration.PGVectorSeedResult, error) {
	if err := ctx.Err(); err != nil {
		return migration.PGVectorSeedResult{}, err
	}
	f.seeded = dataset
	return migration.PGVectorSeedResult{
		Table:         f.config.Table,
		Dimension:     dataset.Dimension,
		RecordsTotal:  len(dataset.Records),
		RecordsSeeded: len(dataset.Records),
	}, nil
}
