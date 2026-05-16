package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/migration"
)

type fakePGVectorFullRecordReader struct {
	request migration.PGVectorFullRecordReadRequest
	records []migration.VectorMigrationRecord
}

func (r *fakePGVectorFullRecordReader) ReadPGVectorFullRecords(ctx context.Context, request migration.PGVectorFullRecordReadRequest) ([]migration.VectorMigrationRecord, error) {
	r.request = request
	return r.records, nil
}

func TestParseBuildPGVectorRecordArtifactOptionsRequiresPaths(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "connection", args: []string{"--record-mapping", "mapping.json", "--output", "out.json"}, want: "pgvector-connection-url is required"},
		{name: "mapping", args: []string{"--pgvector-connection-url", "postgres://[REDACTED]", "--output", "out.json"}, want: "record-mapping is required"},
		{name: "output", args: []string{"--pgvector-connection-url", "postgres://[REDACTED]", "--record-mapping", "mapping.json"}, want: "output path is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseBuildPGVectorRecordArtifactOptions(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestRunBuildPGVectorRecordArtifactWritesMappedArtifact0600(t *testing.T) {
	dir := t.TempDir()
	mappingPath := filepath.Join(dir, "record-mapping.json")
	writeJSONFixture(t, mappingPath, fullRecordBuilderMappingFixture())
	outputPath := filepath.Join(dir, "target.json")
	reader := &fakePGVectorFullRecordReader{records: []migration.VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}, Scalars: map[string]any{"product_title": "First"}, DynamicMetadata: map[string]any{"brand": "acme"}, Partition: "tenant_a"}}}

	err := runBuildPGVectorRecordArtifactWithReader(context.Background(), []string{"--pgvector-connection-url", "postgres://[REDACTED]", "--record-mapping", mappingPath, "--output", outputPath}, reader)
	if err != nil {
		t.Fatalf("runBuildPGVectorRecordArtifactWithReader returned error: %v", err)
	}
	if reader.request.Table != "items" || reader.request.IDColumn != "id" || reader.request.VectorColumn != "embedding" || len(reader.request.ScalarColumns) != 1 || reader.request.ScalarColumns[0].SourceField != "product_title" || reader.request.ScalarColumns[0].TargetColumn != "title" || reader.request.DynamicColumn != "metadata" || reader.request.PartitionColumn != "partition" {
		t.Fatalf("request = %#v", reader.request)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("output mode = %#o", got)
	}
	var artifact migration.FullRecordArtifact
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	if artifact.System != "pgvector" || artifact.Collection != "items" || artifact.RecordMappingPath != mappingPath || len(artifact.Records) != 1 {
		t.Fatalf("artifact = %#v", artifact)
	}
}
