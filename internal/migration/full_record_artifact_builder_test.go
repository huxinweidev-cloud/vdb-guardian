package migration

import (
	"math"
	"strings"
	"testing"
)

func TestBuildFullRecordArtifactSortsRecordsAndPreservesPayload(t *testing.T) {
	artifact, err := BuildFullRecordArtifact([]VectorMigrationRecord{
		{
			ID:              "sku-2",
			Vector:          []float64{0.3, 0.4},
			Scalars:         map[string]any{"product_title": "Second", "price": 12.5},
			DynamicMetadata: map[string]any{"brand": "acme", "tags": []any{"sale"}},
			Partition:       "tenant_b",
		},
		{ID: "sku-1", Vector: []float64{0.1, 0.2}, Scalars: map[string]any{"product_title": "First"}, Partition: "tenant_a"},
	}, FullRecordArtifactBuildOptions{System: "milvus", Collection: "items", RecordMappingPath: "/tmp/record-mapping.json"})
	if err != nil {
		t.Fatalf("BuildFullRecordArtifact returned error: %v", err)
	}
	if artifact.SchemaVersion != FullRecordArtifactVersion || artifact.System != "milvus" || artifact.Collection != "items" || artifact.RecordMappingPath != "/tmp/record-mapping.json" {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	if len(artifact.Records) != 2 || artifact.Records[0].ID != "sku-1" || artifact.Records[1].ID != "sku-2" {
		t.Fatalf("records not sorted: %#v", artifact.Records)
	}
	if artifact.Records[0].VectorHash == "" || artifact.Records[0].VectorHash == artifact.Records[1].VectorHash {
		t.Fatalf("unexpected vector hashes: %#v", artifact.Records)
	}
	if artifact.Records[1].VectorDimension != 2 {
		t.Fatalf("vector dimension = %d", artifact.Records[1].VectorDimension)
	}
	if artifact.Records[1].Scalars["product_title"] != "Second" || artifact.Records[1].DynamicMetadata["brand"] != "acme" || artifact.Records[1].Partition != "tenant_b" {
		t.Fatalf("payload not preserved: %#v", artifact.Records[1])
	}
}

func TestBuildFullRecordArtifactHashesEquivalentVectorsDeterministically(t *testing.T) {
	left, err := BuildFullRecordArtifact([]VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2, 0.3}}}, FullRecordArtifactBuildOptions{System: "milvus", Collection: "items"})
	if err != nil {
		t.Fatalf("left build returned error: %v", err)
	}
	right, err := BuildFullRecordArtifact([]VectorMigrationRecord{{ID: "sku-1", Vector: []float64{float64(float32(0.1)), float64(float32(0.2)), float64(float32(0.3))}}}, FullRecordArtifactBuildOptions{System: "pgvector", Collection: "items"})
	if err != nil {
		t.Fatalf("right build returned error: %v", err)
	}
	if left.Records[0].VectorHash != right.Records[0].VectorHash {
		t.Fatalf("hashes differ for equivalent float32/float64 vectors: %s vs %s", left.Records[0].VectorHash, right.Records[0].VectorHash)
	}
}

func TestBuildFullRecordArtifactRejectsInvalidRecords(t *testing.T) {
	cases := []struct {
		name    string
		records []VectorMigrationRecord
		want    string
	}{
		{name: "empty id", records: []VectorMigrationRecord{{ID: "", Vector: []float64{1}}}, want: "record id is required"},
		{name: "duplicate id", records: []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{1}}, {ID: "sku-1", Vector: []float64{2}}}, want: "duplicate record id"},
		{name: "empty vector", records: []VectorMigrationRecord{{ID: "sku-1"}}, want: "vector is required"},
		{name: "non finite", records: []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{math.NaN()}}}, want: "non-finite vector value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildFullRecordArtifact(tc.records, FullRecordArtifactBuildOptions{System: "milvus", Collection: "items"})
			if err == nil || !containsString(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestBuildFullRecordArtifactRequiresMetadata(t *testing.T) {
	_, err := BuildFullRecordArtifact([]VectorMigrationRecord{{ID: "sku-1", Vector: []float64{1}}}, FullRecordArtifactBuildOptions{System: "", Collection: "items"})
	if err == nil || !containsString(err.Error(), "system is required") {
		t.Fatalf("expected system error, got %v", err)
	}
	_, err = BuildFullRecordArtifact([]VectorMigrationRecord{{ID: "sku-1", Vector: []float64{1}}}, FullRecordArtifactBuildOptions{System: "milvus", Collection: ""})
	if err == nil || !containsString(err.Error(), "collection is required") {
		t.Fatalf("expected collection error, got %v", err)
	}
}

func containsString(value, substring string) bool {
	return strings.Contains(value, substring)
}
