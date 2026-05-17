package migration

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestMilvusSDKSeedDBConnectUsesClientFactory(t *testing.T) {
	db := newMilvusSDKSeedDBWithClientFactory("localhost:19530", func(ctx context.Context, address string) (milvusSeedSDKClient, error) {
		if address != "localhost:19530" {
			t.Fatalf("unexpected address: %s", address)
		}
		return &fakeMilvusSeedSDKClient{}, nil
	})

	if err := db.Connect(context.Background()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
}

func TestMilvusSDKSeedDBCreateCollectionDropsExistingAndPreparesCollection(t *testing.T) {
	client := &fakeMilvusSeedSDKClient{hasCollection: true}
	db := connectedFakeMilvusSDKSeedDB(client)

	err := db.CreateCollection(context.Background(), milvusCreateCollectionRequest{
		Collection:  "items",
		IDField:     "id",
		VectorField: "embedding",
		Dimension:   8,
		Metric:      "cosine",
	})
	if err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}
	if !client.dropCalled {
		t.Fatal("expected existing collection to be dropped")
	}
	if client.created.Collection != "items" || client.created.IDField != "id" || client.created.VectorField != "embedding" {
		t.Fatalf("unexpected create request: %#v", client.created)
	}
	if client.created.Dimension != 8 || client.created.Metric != "cosine" {
		t.Fatalf("unexpected create dimension/metric: %#v", client.created)
	}
	if !client.indexCreated || !client.loaded {
		t.Fatalf("expected index and load calls, index=%v load=%v", client.indexCreated, client.loaded)
	}
}

func TestMilvusSDKSeedDBCreateCollectionSkipsDropWhenMissing(t *testing.T) {
	client := &fakeMilvusSeedSDKClient{hasCollection: false}
	db := connectedFakeMilvusSDKSeedDB(client)

	if err := db.CreateCollection(context.Background(), milvusCreateCollectionRequest{Collection: "items", IDField: "id", VectorField: "embedding", Dimension: 8, Metric: "l2"}); err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}
	if client.dropCalled {
		t.Fatal("did not expect missing collection to be dropped")
	}
	if client.created.Metric != "l2" {
		t.Fatalf("expected l2 metric, got %#v", client.created)
	}
}

func TestMilvusSDKSeedDBCreateCollectionCarriesComplexFlag(t *testing.T) {
	client := &fakeMilvusSeedSDKClient{}
	db := connectedFakeMilvusSDKSeedDB(client)

	if err := db.CreateCollection(context.Background(), milvusCreateCollectionRequest{Collection: "items", IDField: "id", VectorField: "embedding", Dimension: 8, Metric: "cosine", Complex: true}); err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}
	if !client.created.Complex {
		t.Fatalf("expected complex create request, got %#v", client.created)
	}
}

func TestMilvusSDKSeedDBInsertRecordsConvertsColumnsAndFlushes(t *testing.T) {
	client := &fakeMilvusSeedSDKClient{}
	db := connectedFakeMilvusSDKSeedDB(client)

	err := db.InsertRecords(context.Background(), milvusInsertRecordsRequest{
		Collection:  "items",
		IDField:     "id",
		VectorField: "embedding",
		Records: []milvusSeedRecord{
			{ID: "vec-1", Vector: []float64{0.1, 0.2}},
			{ID: "vec-2", Vector: []float64{0.3, 0.4}},
		},
	})
	if err != nil {
		t.Fatalf("InsertRecords returned error: %v", err)
	}
	if len(client.inserted) != 1 {
		t.Fatalf("expected one insert request, got %d", len(client.inserted))
	}
	inserted := client.inserted[0]
	if inserted.Collection != "items" || inserted.IDField != "id" || inserted.VectorField != "embedding" {
		t.Fatalf("unexpected insert request: %#v", inserted)
	}
	if !reflect.DeepEqual(inserted.IDs, []string{"vec-1", "vec-2"}) {
		t.Fatalf("unexpected ids: %#v", inserted.IDs)
	}
	assertMilvusSeedFloat32MatrixAlmostEqual(t, inserted.Vectors, [][]float32{{0.1, 0.2}, {0.3, 0.4}})
	if !client.flushed {
		t.Fatal("expected collection to be flushed after insert")
	}
}

func TestMilvusSDKSeedDBInsertRecordsConvertsComplexFieldsAndPartitions(t *testing.T) {
	client := &fakeMilvusSeedSDKClient{}
	db := connectedFakeMilvusSDKSeedDB(client)
	titleOne := "One"
	titleTwo := "Two"
	priceOne := 12.5
	priceTwo := 0.75
	quantityOne := int64(7)
	quantityTwo := int64(3)
	active := true
	inactive := false
	category := "books"
	partitionA := "tenant_a"
	partitionB := "tenant_b"

	err := db.InsertRecords(context.Background(), milvusInsertRecordsRequest{
		Collection:  "items",
		IDField:     "id",
		VectorField: "embedding",
		Records: []milvusSeedRecord{
			{
				ID:              "vec-1",
				Vector:          []float64{0.1, 0.2},
				Title:           &titleOne,
				Price:           &priceOne,
				Quantity:        &quantityOne,
				Active:          &active,
				Category:        &category,
				DynamicMetadata: map[string]any{"tags": []any{"a", "b"}},
				Partition:       &partitionA,
			},
			{
				ID:              "vec-2",
				Vector:          []float64{0.3, 0.4},
				Title:           &titleTwo,
				Price:           &priceTwo,
				Quantity:        &quantityTwo,
				Active:          &inactive,
				DynamicMetadata: map[string]any{"tags": []any{"c"}},
				Partition:       &partitionB,
			},
		},
	})
	if err != nil {
		t.Fatalf("InsertRecords returned error: %v", err)
	}
	if !reflect.DeepEqual(client.partitionRequests, []string{"tenant_a", "tenant_b"}) {
		t.Fatalf("unexpected partitions: %#v", client.partitionRequests)
	}
	if len(client.inserted) != 2 {
		t.Fatalf("expected one insert per partition, got %d", len(client.inserted))
	}
	first := client.inserted[0]
	if first.Partition != "tenant_a" || !first.Complex {
		t.Fatalf("unexpected first partition request: %#v", first)
	}
	if !reflect.DeepEqual(first.IDs, []string{"vec-1"}) || !reflect.DeepEqual(first.Titles, []string{"One"}) {
		t.Fatalf("unexpected first scalar columns: %#v", first)
	}
	if !reflect.DeepEqual(first.Prices, []float64{12.5}) || !reflect.DeepEqual(first.Quantities, []int64{7}) || !reflect.DeepEqual(first.Actives, []bool{true}) || !reflect.DeepEqual(first.Categories, []string{"books"}) {
		t.Fatalf("unexpected first typed columns: %#v", first)
	}
	assertMilvusSeedMetadata(t, first.Metadata[0], map[string]any{milvusSeedPartitionMetadataField: "tenant_a", "tags": []any{"a", "b"}})
	second := client.inserted[1]
	if second.Partition != "tenant_b" || !second.Complex {
		t.Fatalf("unexpected second partition request: %#v", second)
	}
	if !reflect.DeepEqual(second.Categories, []string{""}) {
		t.Fatalf("expected nil category to use empty-string nullable fallback, got %#v", second.Categories)
	}
	assertMilvusSeedMetadata(t, second.Metadata[0], map[string]any{milvusSeedPartitionMetadataField: "tenant_b", "tags": []any{"c"}})
	if !client.flushed {
		t.Fatal("expected collection to be flushed after partition inserts")
	}
}

func TestMilvusSDKSeedDBInsertRecordsPreservesMixedPartitionAndDefaultRecords(t *testing.T) {
	client := &fakeMilvusSeedSDKClient{}
	db := connectedFakeMilvusSDKSeedDB(client)
	partition := "tenant_a"
	title := "Partitioned"

	err := db.InsertRecords(context.Background(), milvusInsertRecordsRequest{
		Collection:  "items",
		IDField:     "id",
		VectorField: "embedding",
		Records: []milvusSeedRecord{
			{ID: "vec-default", Vector: []float64{0.1, 0.2}},
			{ID: "vec-partitioned", Vector: []float64{0.3, 0.4}, Title: &title, Partition: &partition},
		},
	})
	if err != nil {
		t.Fatalf("InsertRecords returned error: %v", err)
	}
	if !reflect.DeepEqual(client.partitionRequests, []string{"tenant_a"}) {
		t.Fatalf("unexpected partitions: %#v", client.partitionRequests)
	}
	if len(client.inserted) != 2 {
		t.Fatalf("expected default plus partition insert, got %d", len(client.inserted))
	}
	if client.inserted[0].Partition != "" || !reflect.DeepEqual(client.inserted[0].IDs, []string{"vec-default"}) {
		t.Fatalf("unexpected default insert: %#v", client.inserted[0])
	}
	if client.inserted[1].Partition != "tenant_a" || !reflect.DeepEqual(client.inserted[1].IDs, []string{"vec-partitioned"}) {
		t.Fatalf("unexpected partition insert: %#v", client.inserted[1])
	}
}

func TestMilvusSDKSeedDBRejectsDisconnectedUsage(t *testing.T) {
	db := newMilvusSDKSeedDBWithClientFactory("localhost:19530", nil)
	if err := db.CreateCollection(context.Background(), milvusCreateCollectionRequest{Collection: "items"}); err == nil {
		t.Fatal("expected CreateCollection to reject disconnected client")
	}
	if err := db.InsertRecords(context.Background(), milvusInsertRecordsRequest{Collection: "items"}); err == nil {
		t.Fatal("expected InsertRecords to reject disconnected client")
	}
}

func TestMilvusSDKSeedDBPropagatesClientErrors(t *testing.T) {
	client := &fakeMilvusSeedSDKClient{err: errMilvusSeedSDKFake}
	db := connectedFakeMilvusSDKSeedDB(client)

	if err := db.CreateCollection(context.Background(), milvusCreateCollectionRequest{Collection: "items", IDField: "id", VectorField: "embedding", Dimension: 8, Metric: "cosine"}); !errors.Is(err, errMilvusSeedSDKFake) {
		t.Fatalf("expected create error to propagate, got %v", err)
	}
	if err := db.InsertRecords(context.Background(), milvusInsertRecordsRequest{Collection: "items", IDField: "id", VectorField: "embedding", Records: []milvusSeedRecord{{ID: "vec-1", Vector: []float64{0.1}}}}); !errors.Is(err, errMilvusSeedSDKFake) {
		t.Fatalf("expected insert error to propagate, got %v", err)
	}
}

func TestMilvusSDKSeedDBCloseReleasesClient(t *testing.T) {
	client := &fakeMilvusSeedSDKClient{}
	db := connectedFakeMilvusSDKSeedDB(client)
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !client.closed {
		t.Fatal("expected client to be closed")
	}
}

func connectedFakeMilvusSDKSeedDB(client *fakeMilvusSeedSDKClient) *milvusSDKSeedDB {
	db := newMilvusSDKSeedDBWithClientFactory("localhost:19530", func(ctx context.Context, address string) (milvusSeedSDKClient, error) {
		return client, nil
	})
	if err := db.Connect(context.Background()); err != nil {
		panic(err)
	}
	return db
}

type fakeMilvusSeedSDKClient struct {
	hasCollection bool
	err           error

	dropCalled        bool
	indexCreated      bool
	loaded            bool
	flushed           bool
	closed            bool
	partitionRequests []string
	created           milvusSDKSeedCreateCollectionRequest
	inserted          []milvusSDKSeedInsertRequest
}

func (c *fakeMilvusSeedSDKClient) HasCollection(ctx context.Context, collection string) (bool, error) {
	if c.err != nil {
		return false, c.err
	}
	return c.hasCollection, nil
}

func (c *fakeMilvusSeedSDKClient) DropCollection(ctx context.Context, collection string) error {
	if c.err != nil {
		return c.err
	}
	c.dropCalled = true
	return nil
}

func (c *fakeMilvusSeedSDKClient) CreateCollection(ctx context.Context, req milvusSDKSeedCreateCollectionRequest) error {
	if c.err != nil {
		return c.err
	}
	c.created = req
	return nil
}

func (c *fakeMilvusSeedSDKClient) CreatePartition(ctx context.Context, collection string, partition string) error {
	if c.err != nil {
		return c.err
	}
	c.partitionRequests = append(c.partitionRequests, partition)
	return nil
}

func (c *fakeMilvusSeedSDKClient) CreateIndex(ctx context.Context, collection string, vectorField string, metric string) error {
	if c.err != nil {
		return c.err
	}
	c.indexCreated = true
	return nil
}

func (c *fakeMilvusSeedSDKClient) LoadCollection(ctx context.Context, collection string) error {
	if c.err != nil {
		return c.err
	}
	c.loaded = true
	return nil
}

func (c *fakeMilvusSeedSDKClient) Insert(ctx context.Context, req milvusSDKSeedInsertRequest) error {
	if c.err != nil {
		return c.err
	}
	c.inserted = append(c.inserted, req)
	return nil
}

func (c *fakeMilvusSeedSDKClient) Flush(ctx context.Context, collection string) error {
	if c.err != nil {
		return c.err
	}
	c.flushed = true
	return nil
}

func (c *fakeMilvusSeedSDKClient) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.closed = true
	return nil
}

func assertMilvusSeedFloat32MatrixAlmostEqual(t *testing.T, got [][]float32, want [][]float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("unexpected vector row count: got %d want %d", len(got), len(want))
	}
	for row := range want {
		if len(got[row]) != len(want[row]) {
			t.Fatalf("unexpected vector dimension at row %d: got %d want %d", row, len(got[row]), len(want[row]))
		}
		for col := range want[row] {
			if diff := got[row][col] - want[row][col]; diff < -1e-6 || diff > 1e-6 {
				t.Fatalf("vector[%d][%d] mismatch: got %f want %f", row, col, got[row][col], want[row][col])
			}
		}
	}
}

func assertMilvusSeedMetadata(t *testing.T, got []byte, want map[string]any) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("unexpected metadata: %#v want %#v", decoded, want)
	}
}

var errMilvusSeedSDKFake = errors.New("fake milvus seed sdk error")
