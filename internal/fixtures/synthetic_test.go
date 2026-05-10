package fixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSyntheticDatasetIsDeterministic(t *testing.T) {
	options := SyntheticOptions{
		Seed:        42,
		Dimension:   4,
		RecordCount: 5,
		QueryCount:  2,
		Metric:      MetricCosine,
	}

	first, err := GenerateSyntheticDataset(options)
	if err != nil {
		t.Fatalf("GenerateSyntheticDataset returned error: %v", err)
	}
	second, err := GenerateSyntheticDataset(options)
	if err != nil {
		t.Fatalf("GenerateSyntheticDataset returned error on second call: %v", err)
	}

	if len(first.Records) != 5 {
		t.Fatalf("expected 5 records, got %d", len(first.Records))
	}
	if len(first.Queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(first.Queries))
	}
	if first.Records[0].ID != "vec-000001" {
		t.Fatalf("unexpected first record id: %s", first.Records[0].ID)
	}
	if first.Queries[0].ID != "query-000001" {
		t.Fatalf("unexpected first query id: %s", first.Queries[0].ID)
	}
	if len(first.Records[0].Vector) != 4 {
		t.Fatalf("expected record dimension 4, got %d", len(first.Records[0].Vector))
	}
	if len(first.Queries[0].Vector) != 4 {
		t.Fatalf("expected query dimension 4, got %d", len(first.Queries[0].Vector))
	}
	if first.Records[0].Vector[0] != second.Records[0].Vector[0] {
		t.Fatalf("expected deterministic first vector value, got %f and %f", first.Records[0].Vector[0], second.Records[0].Vector[0])
	}
	if first.Queries[1].Vector[3] != second.Queries[1].Vector[3] {
		t.Fatalf("expected deterministic query value, got %f and %f", first.Queries[1].Vector[3], second.Queries[1].Vector[3])
	}
}

func TestGenerateSyntheticDatasetNormalizesCosineVectors(t *testing.T) {
	dataset, err := GenerateSyntheticDataset(SyntheticOptions{
		Seed:        7,
		Dimension:   8,
		RecordCount: 3,
		QueryCount:  2,
		Metric:      MetricCosine,
	})
	if err != nil {
		t.Fatalf("GenerateSyntheticDataset returned error: %v", err)
	}

	for _, record := range dataset.Records {
		assertApproxUnitVector(t, record.Vector)
	}
	for _, query := range dataset.Queries {
		assertApproxUnitVector(t, query.Vector)
	}
}

func TestGenerateSyntheticDatasetRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name    string
		options SyntheticOptions
	}{
		{
			name:    "zero dimension",
			options: SyntheticOptions{Seed: 1, Dimension: 0, RecordCount: 1, QueryCount: 1, Metric: MetricCosine},
		},
		{
			name:    "dimension above pgvector vector limit",
			options: SyntheticOptions{Seed: 1, Dimension: 2001, RecordCount: 1, QueryCount: 1, Metric: MetricCosine},
		},
		{
			name:    "zero records",
			options: SyntheticOptions{Seed: 1, Dimension: 4, RecordCount: 0, QueryCount: 1, Metric: MetricCosine},
		},
		{
			name:    "zero queries",
			options: SyntheticOptions{Seed: 1, Dimension: 4, RecordCount: 1, QueryCount: 0, Metric: MetricCosine},
		},
		{
			name:    "unsupported metric",
			options: SyntheticOptions{Seed: 1, Dimension: 4, RecordCount: 1, QueryCount: 1, Metric: "dot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GenerateSyntheticDataset(tt.options)
			if err == nil {
				t.Fatalf("expected invalid options to fail")
			}
		})
	}
}

func TestWriteSyntheticDatasetWritesReadableJSON(t *testing.T) {
	dataset, err := GenerateSyntheticDataset(SyntheticOptions{
		Seed:        9,
		Dimension:   3,
		RecordCount: 2,
		QueryCount:  1,
		Metric:      MetricL2,
	})
	if err != nil {
		t.Fatalf("GenerateSyntheticDataset returned error: %v", err)
	}

	path := filepath.Join(t.TempDir(), "synthetic.json")
	if err := WriteSyntheticDataset(path, dataset); err != nil {
		t.Fatalf("WriteSyntheticDataset returned error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written dataset: %v", err)
	}
	var decoded SyntheticDataset
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatalf("decode written JSON: %v", err)
	}
	if decoded.Dimension != 3 || decoded.Metric != MetricL2 {
		t.Fatalf("unexpected decoded metadata: dimension=%d metric=%s", decoded.Dimension, decoded.Metric)
	}
	if len(decoded.Records) != 2 || len(decoded.Queries) != 1 {
		t.Fatalf("unexpected decoded counts: records=%d queries=%d", len(decoded.Records), len(decoded.Queries))
	}
}

func assertApproxUnitVector(t *testing.T, vector []float64) {
	t.Helper()
	var sum float64
	for _, value := range vector {
		sum += value * value
	}
	if sum < 0.999999 || sum > 1.000001 {
		t.Fatalf("expected unit vector norm squared near 1.0, got %.12f", sum)
	}
}
