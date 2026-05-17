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
	if writeErr := WriteSyntheticDataset(path, dataset); writeErr != nil {
		t.Fatalf("WriteSyntheticDataset returned error: %v", writeErr)
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

func TestSyntheticDatasetDecodesComplexOptionalFields(t *testing.T) {
	content := []byte(`{
	  "seed": 20260516,
	  "dimension": 3,
	  "record_count": 1,
	  "query_count": 1,
	  "metric": "cosine",
	  "records": [
	    {
	      "id": "vec-complex-001",
	      "vector": [0.1, 0.2, 0.3],
	      "title": "deterministic item",
	      "price": 19.95,
	      "quantity": 7,
	      "active": true,
	      "category": null,
	      "dynamic_metadata": {
	        "tags": ["smoke", "copy"],
	        "nested": {"rank": 1, "enabled": false},
	        "nullable": null
	      },
	      "partition": "tenant_a"
	    }
	  ],
	  "queries": [
	    {"id": "query-complex-001", "vector": [0.4, 0.5, 0.6]}
	  ]
	}`)

	var decoded SyntheticDataset
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatalf("decode complex synthetic fixture: %v", err)
	}
	if len(decoded.Records) != 1 {
		t.Fatalf("expected one decoded record, got %d", len(decoded.Records))
	}
	record := decoded.Records[0]
	if record.Title == nil || *record.Title != "deterministic item" {
		t.Fatalf("expected title to round-trip, got %#v", record.Title)
	}
	if record.Price == nil || *record.Price != 19.95 {
		t.Fatalf("expected price to round-trip, got %#v", record.Price)
	}
	if record.Quantity == nil || *record.Quantity != 7 {
		t.Fatalf("expected quantity to round-trip, got %#v", record.Quantity)
	}
	if record.Active == nil || !*record.Active {
		t.Fatalf("expected active to round-trip true, got %#v", record.Active)
	}
	if record.Category != nil {
		t.Fatalf("expected null category to decode as nil, got %#v", record.Category)
	}
	if record.Partition == nil || *record.Partition != "tenant_a" {
		t.Fatalf("expected partition to round-trip, got %#v", record.Partition)
	}
	if got := record.DynamicMetadata["nullable"]; got != nil {
		t.Fatalf("expected nullable dynamic metadata to remain nil, got %#v", got)
	}
	tags, ok := record.DynamicMetadata["tags"].([]any)
	if !ok || len(tags) != 2 || tags[1] != "copy" {
		t.Fatalf("expected dynamic metadata array to round-trip, got %#v", record.DynamicMetadata["tags"])
	}
	nested, ok := record.DynamicMetadata["nested"].(map[string]any)
	if !ok || nested["rank"] != float64(1) || nested["enabled"] != false {
		t.Fatalf("expected nested dynamic metadata to round-trip, got %#v", record.DynamicMetadata["nested"])
	}
}

func TestSyntheticSmallFixtureRemainsBackwardCompatible(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "migration", "synthetic-small.json"))
	if err != nil {
		t.Fatalf("read existing synthetic fixture: %v", err)
	}

	var decoded SyntheticDataset
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatalf("decode existing synthetic fixture: %v", err)
	}
	if decoded.RecordCount == 0 || len(decoded.Records) != decoded.RecordCount {
		t.Fatalf("unexpected existing fixture records: count=%d len=%d", decoded.RecordCount, len(decoded.Records))
	}
	if decoded.QueryCount == 0 || len(decoded.Queries) != decoded.QueryCount {
		t.Fatalf("unexpected existing fixture queries: count=%d len=%d", decoded.QueryCount, len(decoded.Queries))
	}
	if decoded.Records[0].Title != nil ||
		decoded.Records[0].Price != nil ||
		decoded.Records[0].Quantity != nil ||
		decoded.Records[0].Active != nil ||
		decoded.Records[0].Category != nil ||
		decoded.Records[0].DynamicMetadata != nil ||
		decoded.Records[0].Partition != nil {
		t.Fatalf("expected id/vector fixture optional fields to remain empty, got %#v", decoded.Records[0])
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
