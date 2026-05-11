package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/fixtures"
)

func TestParseGenerateSyntheticFixtureOptions(t *testing.T) {
	options, err := parseGenerateSyntheticFixtureOptions([]string{
		"--output", "dataset.json",
		"--seed", "42",
		"--dimension", "16",
		"--records", "100",
		"--queries", "10",
		"--metric", fixtures.MetricCosine,
	})
	if err != nil {
		t.Fatalf("parseGenerateSyntheticFixtureOptions returned error: %v", err)
	}

	if options.OutputPath != "dataset.json" {
		t.Fatalf("unexpected output path: %s", options.OutputPath)
	}
	if options.SyntheticOptions.Seed != 42 || options.SyntheticOptions.Dimension != 16 {
		t.Fatalf("unexpected seed/dimension: %+v", options.SyntheticOptions)
	}
	if options.SyntheticOptions.RecordCount != 100 || options.SyntheticOptions.QueryCount != 10 {
		t.Fatalf("unexpected counts: %+v", options.SyntheticOptions)
	}
	if options.SyntheticOptions.Metric != fixtures.MetricCosine {
		t.Fatalf("unexpected metric: %s", options.SyntheticOptions.Metric)
	}
}

func TestParseGenerateSyntheticFixtureOptionsRejectsMissingOutput(t *testing.T) {
	_, err := parseGenerateSyntheticFixtureOptions([]string{"--dimension", "8"})
	if err == nil {
		t.Fatalf("expected missing output to fail")
	}
	if !strings.Contains(err.Error(), "output") {
		t.Fatalf("expected output error, got %v", err)
	}
}

func TestRunGenerateSyntheticFixtureWritesDataset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.json")
	err := runGenerateSyntheticFixture([]string{
		"--output", path,
		"--seed", "99",
		"--dimension", "6",
		"--records", "4",
		"--queries", "2",
		"--metric", fixtures.MetricL2,
	})
	if err != nil {
		t.Fatalf("runGenerateSyntheticFixture returned error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read synthetic fixture: %v", err)
	}
	var dataset fixtures.SyntheticDataset
	if err := json.Unmarshal(content, &dataset); err != nil {
		t.Fatalf("decode synthetic fixture: %v", err)
	}
	if dataset.Seed != 99 || dataset.Dimension != 6 || dataset.Metric != fixtures.MetricL2 {
		t.Fatalf("unexpected dataset metadata: %+v", dataset)
	}
	if len(dataset.Records) != 4 || len(dataset.Queries) != 2 {
		t.Fatalf("unexpected dataset counts: records=%d queries=%d", len(dataset.Records), len(dataset.Queries))
	}
}
