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

func TestParseSeedMilvusOptions(t *testing.T) {
	options, err := parseSeedMilvusOptions([]string{
		"--fixture", "fixture.json",
		"--address", "localhost:19530",
		"--collection", "vectors",
		"--id-field", "vector_id",
		"--vector-field", "embedding",
		"--metric", "l2",
	})
	if err != nil {
		t.Fatalf("parseSeedMilvusOptions returned error: %v", err)
	}
	if options.FixturePath != "fixture.json" {
		t.Fatalf("unexpected fixture path: %s", options.FixturePath)
	}
	if options.Address != "localhost:19530" {
		t.Fatalf("unexpected address: %s", options.Address)
	}
	if options.SeederConfig.Collection != "vectors" {
		t.Fatalf("unexpected collection: %s", options.SeederConfig.Collection)
	}
	if options.SeederConfig.IDField != "vector_id" {
		t.Fatalf("unexpected id field: %s", options.SeederConfig.IDField)
	}
	if options.SeederConfig.VectorField != "embedding" {
		t.Fatalf("unexpected vector field: %s", options.SeederConfig.VectorField)
	}
	if options.SeederConfig.Metric != fixtures.MetricL2 {
		t.Fatalf("unexpected metric: %s", options.SeederConfig.Metric)
	}
}

func TestParseSeedMilvusOptionsRejectsMissingRequiredFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing fixture", args: []string{"--address", "localhost:19530"}, want: "fixture"},
		{name: "missing address", args: []string{"--fixture", "fixture.json"}, want: "address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSeedMilvusOptions(tt.args)
			if err == nil {
				t.Fatal("expected missing option to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestRunSeedMilvusWithInjectedSeeder(t *testing.T) {
	fixturePath := writeSeedMilvusFixture(t, syntheticSeedMilvusDataset())
	fake := &fakeSeedMilvusRunner{}
	err := runSeedMilvusWithFactory(context.Background(), []string{
		"--fixture", fixturePath,
		"--address", "localhost:19530",
		"--collection", "items",
	}, fake.newSeeder)
	if err != nil {
		t.Fatalf("runSeedMilvusWithFactory returned error: %v", err)
	}
	if fake.address != "localhost:19530" {
		t.Fatalf("unexpected address: %s", fake.address)
	}
	if fake.config.Dimension != 3 {
		t.Fatalf("expected dimension from dataset, got %d", fake.config.Dimension)
	}
	if fake.seeded.Dimension != 3 || len(fake.seeded.Records) != 2 {
		t.Fatalf("unexpected seeded dataset: %+v", fake.seeded)
	}
}

func TestRunSeedMilvusRejectsInvalidFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	err := runSeedMilvusWithFactory(context.Background(), []string{
		"--fixture", path,
		"--address", "localhost:19530",
	}, func(string, migration.MilvusSeederConfig) (milvusSeedRunner, error) {
		t.Fatal("factory should not be called for invalid fixture")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected invalid fixture to fail")
	}
}

func writeSeedMilvusFixture(t *testing.T, dataset fixtures.SyntheticDataset) string {
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

func syntheticSeedMilvusDataset() fixtures.SyntheticDataset {
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

type fakeSeedMilvusRunner struct {
	address string
	config  migration.MilvusSeederConfig
	seeded  fixtures.SyntheticDataset
}

func (f *fakeSeedMilvusRunner) newSeeder(address string, config migration.MilvusSeederConfig) (milvusSeedRunner, error) {
	f.address = address
	f.config = config
	return f, nil
}

func (f *fakeSeedMilvusRunner) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (migration.MilvusSeedResult, error) {
	if err := ctx.Err(); err != nil {
		return migration.MilvusSeedResult{}, err
	}
	f.seeded = dataset
	return migration.MilvusSeedResult{
		Collection:    f.config.Collection,
		Dimension:     dataset.Dimension,
		RecordsTotal:  len(dataset.Records),
		RecordsSeeded: len(dataset.Records),
	}, nil
}
