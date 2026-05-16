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

type fakeMilvusFullRecordReader struct {
	request migration.MilvusMigrationReadRequest
	records []migration.VectorMigrationRecord
}

func (r *fakeMilvusFullRecordReader) ReadMilvusMigrationRecords(ctx context.Context, collection, idField, vectorField string) ([]migration.VectorMigrationRecord, error) {
	return nil, nil
}

func (r *fakeMilvusFullRecordReader) ReadMilvusMigrationRecordsWithMapping(ctx context.Context, request migration.MilvusMigrationReadRequest) ([]migration.VectorMigrationRecord, error) {
	r.request = request
	return r.records, nil
}

func TestParseBuildMilvusRecordArtifactOptionsRequiresPaths(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "address", args: []string{"--record-mapping", "mapping.json", "--output", "out.json"}, want: "milvus-address is required"},
		{name: "mapping", args: []string{"--milvus-address", "localhost:19530", "--output", "out.json"}, want: "record-mapping is required"},
		{name: "output", args: []string{"--milvus-address", "localhost:19530", "--record-mapping", "mapping.json"}, want: "output path is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseBuildMilvusRecordArtifactOptions(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestRunBuildMilvusRecordArtifactWritesMappedArtifact0600(t *testing.T) {
	dir := t.TempDir()
	mappingPath := filepath.Join(dir, "record-mapping.json")
	writeJSONFixture(t, mappingPath, fullRecordBuilderMappingFixture())
	outputPath := filepath.Join(dir, "source.json")
	reader := &fakeMilvusFullRecordReader{records: []migration.VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}, Scalars: map[string]any{"product_title": "First"}, DynamicMetadata: map[string]any{"brand": "acme"}, Partition: "tenant_a"}}}

	err := runBuildMilvusRecordArtifactWithReader(context.Background(), []string{"--milvus-address", "localhost:19530", "--record-mapping", mappingPath, "--output", outputPath}, reader)
	if err != nil {
		t.Fatalf("runBuildMilvusRecordArtifactWithReader returned error: %v", err)
	}
	if reader.request.Collection != "items" || reader.request.IDField != "id" || reader.request.VectorField != "embedding" || !equalStrings(reader.request.ScalarFields, []string{"product_title"}) || reader.request.DynamicField != "_milvus_dynamic" || reader.request.PartitionField != "_milvus_partition" {
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
	if artifact.System != "milvus" || artifact.Collection != "items" || artifact.RecordMappingPath != mappingPath || len(artifact.Records) != 1 {
		t.Fatalf("artifact = %#v", artifact)
	}
}

func TestRunBuildMilvusRecordArtifactRejectsInvalidMapping(t *testing.T) {
	dir := t.TempDir()
	mappingPath := filepath.Join(dir, "record-mapping.json")
	mapping := fullRecordBuilderMappingFixture()
	mapping.Status = migration.RecordMappingStatusFail
	writeJSONFixture(t, mappingPath, mapping)
	reader := &fakeMilvusFullRecordReader{}

	err := runBuildMilvusRecordArtifactWithReader(context.Background(), []string{"--milvus-address", "localhost:19530", "--record-mapping", mappingPath, "--output", filepath.Join(dir, "out.json")}, reader)
	if err == nil || !strings.Contains(err.Error(), "record mapping status") {
		t.Fatalf("expected record mapping status error, got %v", err)
	}
	if reader.request.Collection != "" {
		t.Fatalf("reader should not be called before mapping validation")
	}
}

func fullRecordBuilderMappingFixture() migration.RecordMappingPlan {
	return migration.RecordMappingPlan{
		SchemaVersion: migration.RecordMappingPlanVersion,
		Status:        migration.RecordMappingStatusPass,
		Mappings: []migration.CollectionRecordMapping{{
			SourceCollection:  "items",
			TargetTable:       "items",
			PrimaryKey:        &migration.RecordFieldMapping{Kind: migration.RecordMappingKindPrimaryKey, SourceField: "id", TargetColumn: "id", SupportLevel: "supported"},
			Vector:            &migration.RecordFieldMapping{Kind: migration.RecordMappingKindVector, SourceField: "embedding", TargetColumn: "embedding", TargetType: "vector(2)", SupportLevel: "supported"},
			Scalars:           []migration.RecordFieldMapping{{Kind: migration.RecordMappingKindScalar, SourceField: "product_title", TargetColumn: "title", TargetType: "text", SupportLevel: "supported"}},
			DynamicMetadata:   &migration.RecordFieldMapping{Kind: migration.RecordMappingKindDynamicMetadata, SourceField: "_milvus_dynamic", TargetColumn: "metadata", SupportLevel: "supported"},
			PartitionMetadata: &migration.RecordFieldMapping{Kind: migration.RecordMappingKindPartitionMetadata, SourceField: "_milvus_partition", TargetColumn: "partition", SupportLevel: "supported"},
		}},
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
