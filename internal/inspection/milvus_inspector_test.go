package inspection

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMilvusInspectorBuildsPlanFromMetadataClient(t *testing.T) {
	client := &fakeMilvusMetadataClient{
		collections: []string{"items", "events"},
		metadata: map[string]MilvusCollectionMetadata{
			"items": {
				Name:                "items",
				Description:         "product embeddings",
				AutoID:              false,
				DynamicFieldEnabled: true,
				PrimaryKey:          "id",
				RowCount:            100,
				Fields: []MilvusFieldMetadata{
					{Name: "id", DataType: MilvusDataTypeVarChar, PrimaryKey: true, MaxLength: 64},
					{Name: "embedding", DataType: MilvusDataTypeFloatVector, Dimension: 8},
					{Name: "metadata", DataType: MilvusDataTypeJSON, Nullable: true},
				},
				Indexes: []MilvusIndexMetadata{
					{Field: "embedding", IndexType: "HNSW", MetricType: "COSINE", Params: map[string]string{"M": "16"}},
				},
				Partitions: []MilvusPartitionMetadata{{Name: "_default"}, {Name: "tenant_a"}},
			},
			"events": {
				Name:       "events",
				PrimaryKey: "event_id",
				RowCount:   5,
				Fields: []MilvusFieldMetadata{
					{Name: "event_id", DataType: MilvusDataTypeInt64, PrimaryKey: true},
					{Name: "payload", DataType: MilvusDataTypeArray},
				},
			},
		},
	}

	plan, err := NewMilvusInspector(client, MilvusInspectorOptions{Address: "localhost:19530"}).Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
	if plan.SchemaVersion != MilvusInspectionSchemaVersion {
		t.Fatalf("schema version = %q", plan.SchemaVersion)
	}
	if plan.Source.Type != "milvus" || plan.Source.Address != "localhost:19530" {
		t.Fatalf("unexpected source: %+v", plan.Source)
	}
	if len(plan.Collections) != 2 {
		t.Fatalf("collections = %d, want 2", len(plan.Collections))
	}
	items := findCollectionPlan(t, plan.Collections, "items")
	if items.Name != "items" || items.RowCount != 100 || !items.DynamicFieldEnabled || items.PrimaryKey != "id" {
		t.Fatalf("unexpected items collection: %+v", items)
	}
	if items.Fields[1].TargetType != "vector(8)" {
		t.Fatalf("embedding target type = %q", items.Fields[1].TargetType)
	}
	if len(items.Indexes) != 1 || items.Indexes[0].TargetIndexType != "hnsw" || items.Indexes[0].TargetOps != "vector_cosine_ops" {
		t.Fatalf("unexpected index plan: %+v", items.Indexes)
	}
	if len(items.Partitions) != 2 || items.Partitions[1].RecommendedStrategy != "metadata_column" {
		t.Fatalf("unexpected partitions: %+v", items.Partitions)
	}
	if plan.Summary.CollectionCount != 2 || plan.Summary.WarningCount == 0 {
		t.Fatalf("unexpected summary: %+v", plan.Summary)
	}
}

func TestMilvusInspectorCanInspectSingleCollection(t *testing.T) {
	client := &fakeMilvusMetadataClient{
		collections: []string{"items", "events"},
		metadata: map[string]MilvusCollectionMetadata{
			"events": {
				Name:       "events",
				PrimaryKey: "id",
				Fields:     []MilvusFieldMetadata{{Name: "id", DataType: MilvusDataTypeInt64, PrimaryKey: true}},
			},
		},
	}

	plan, err := NewMilvusInspector(client, MilvusInspectorOptions{Collection: "events"}).Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect single collection failed: %v", err)
	}
	if len(plan.Collections) != 1 || plan.Collections[0].Name != "events" {
		t.Fatalf("unexpected collections: %+v", plan.Collections)
	}
	if client.listCalled {
		t.Fatalf("single collection inspection should not list all collections")
	}
}

func TestMilvusInspectorWrapsClientErrors(t *testing.T) {
	client := &fakeMilvusMetadataClient{listErr: errors.New("milvus unavailable")}
	_, err := NewMilvusInspector(client, MilvusInspectorOptions{}).Inspect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "list milvus collections") {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
}

func findCollectionPlan(t *testing.T, collections []MilvusCollectionPlan, name string) MilvusCollectionPlan {
	t.Helper()
	for _, collection := range collections {
		if collection.Name == name {
			return collection
		}
	}
	t.Fatalf("collection %q not found in %+v", name, collections)
	return MilvusCollectionPlan{}
}

type fakeMilvusMetadataClient struct {
	collections []string
	metadata    map[string]MilvusCollectionMetadata
	listErr     error
	listCalled  bool
}

func (f *fakeMilvusMetadataClient) ListCollections(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.listCalled = true
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]string(nil), f.collections...), nil
}

func (f *fakeMilvusMetadataClient) DescribeCollection(ctx context.Context, collection string) (MilvusCollectionMetadata, error) {
	if err := ctx.Err(); err != nil {
		return MilvusCollectionMetadata{}, err
	}
	metadata, ok := f.metadata[collection]
	if !ok {
		return MilvusCollectionMetadata{}, errors.New("collection not found")
	}
	return metadata, nil
}
