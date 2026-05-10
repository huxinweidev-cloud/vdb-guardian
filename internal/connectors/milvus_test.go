package connectors

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNewMilvusConnectorAppliesDefaults(t *testing.T) {
	connector, err := NewMilvusConnector(MilvusConfig{Address: "localhost:19530"}, nil)
	if err != nil {
		t.Fatalf("NewMilvusConnector returned error: %v", err)
	}

	if connector.Name() != "milvus" {
		t.Fatalf("unexpected connector name: %s", connector.Name())
	}
	if connector.config.DefaultCollection != "items" {
		t.Fatalf("unexpected default collection: %s", connector.config.DefaultCollection)
	}
	if connector.config.IDField != "id" {
		t.Fatalf("unexpected id field: %s", connector.config.IDField)
	}
	if connector.config.VectorField != "embedding" {
		t.Fatalf("unexpected vector field: %s", connector.config.VectorField)
	}
	if connector.config.Metric != MilvusMetricCosine {
		t.Fatalf("unexpected metric: %s", connector.config.Metric)
	}
}

func TestNewMilvusConnectorCreatesAdapterFromAddress(t *testing.T) {
	connector, err := NewMilvusConnector(MilvusConfig{Address: "localhost:19530"}, nil)
	if err != nil {
		t.Fatalf("NewMilvusConnector returned error: %v", err)
	}
	if connector.db == nil {
		t.Fatalf("expected address to create a Milvus adapter")
	}
}

func TestNewMilvusConnectorRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config MilvusConfig
	}{
		{
			name:   "missing address without injected adapter",
			config: MilvusConfig{},
		},
		{
			name:   "invalid default collection",
			config: MilvusConfig{Address: "localhost:19530", DefaultCollection: "bad-collection"},
		},
		{
			name:   "invalid id field",
			config: MilvusConfig{Address: "localhost:19530", IDField: "id;drop"},
		},
		{
			name:   "unsupported metric",
			config: MilvusConfig{Address: "localhost:19530", Metric: "dot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewMilvusConnector(tt.config, nil)
			if err == nil {
				t.Fatalf("expected invalid config to fail")
			}
		})
	}
}

func TestMilvusConnectorConnectCallsAdapter(t *testing.T) {
	db := &fakeMilvusDB{}
	connector := mustMilvusConnector(t, db)

	if err := connector.Connect(context.Background()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if !db.connected {
		t.Fatalf("expected adapter Connect to be called")
	}
}

func TestMilvusConnectorCountUsesCollectionOrDefault(t *testing.T) {
	db := &fakeMilvusDB{count: 42}
	connector := mustMilvusConnector(t, db)

	count, err := connector.Count(context.Background(), "vectors")
	if err != nil {
		t.Fatalf("Count returned error: %v", err)
	}
	if count != 42 {
		t.Fatalf("expected count 42, got %d", count)
	}
	if db.lastCountCollection != "vectors" {
		t.Fatalf("unexpected count collection: %s", db.lastCountCollection)
	}

	_, err = connector.Count(context.Background(), "")
	if err != nil {
		t.Fatalf("Count with default collection returned error: %v", err)
	}
	if db.lastCountCollection != "items" {
		t.Fatalf("expected default collection items, got %s", db.lastCountCollection)
	}
}

func TestMilvusConnectorCountRejectsUnsafeCollection(t *testing.T) {
	connector := mustMilvusConnector(t, &fakeMilvusDB{})

	_, err := connector.Count(context.Background(), "items;drop")
	if err == nil {
		t.Fatalf("expected unsafe collection to fail")
	}
}

func TestMilvusConnectorSearchReturnsRankedHits(t *testing.T) {
	db := &fakeMilvusDB{
		hits: []milvusRawHit{
			{ID: "a", Score: 0.99},
			{ID: "b", Score: 0.95},
			{ID: "c", Score: 0.91},
		},
	}
	connector := mustMilvusConnector(t, db)

	response, err := connector.Search(context.Background(), SearchRequest{
		Collection:  "items",
		QueryVector: []float64{0.1, 0.2, 0.3},
		TopK:        2,
		ExpandK:     3,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	expected := []SearchHit{
		{ID: "a", Rank: 1, Score: 0.99},
		{ID: "b", Rank: 2, Score: 0.95},
		{ID: "c", Rank: 3, Score: 0.91},
	}
	if !reflect.DeepEqual(response.Hits, expected) {
		t.Fatalf("unexpected hits: %#v", response.Hits)
	}
	if db.lastSearch.Collection != "items" {
		t.Fatalf("unexpected search collection: %s", db.lastSearch.Collection)
	}
	if db.lastSearch.Limit != 3 {
		t.Fatalf("expected ExpandK limit 3, got %d", db.lastSearch.Limit)
	}
	if db.lastSearch.Metric != MilvusMetricCosine {
		t.Fatalf("unexpected metric: %s", db.lastSearch.Metric)
	}
}

func TestMilvusConnectorSearchSupportsL2Metric(t *testing.T) {
	db := &fakeMilvusDB{hits: []milvusRawHit{{ID: "a", Score: 0.2}}}
	connector, err := NewMilvusConnector(MilvusConfig{Metric: MilvusMetricL2}, db)
	if err != nil {
		t.Fatalf("NewMilvusConnector returned error: %v", err)
	}

	response, err := connector.Search(context.Background(), SearchRequest{QueryVector: []float64{0.1}, TopK: 1, ExpandK: 1})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if response.Hits[0].Score != -0.2 {
		t.Fatalf("expected L2 distance to be normalized to negative score, got %f", response.Hits[0].Score)
	}
	if db.lastSearch.Metric != MilvusMetricL2 {
		t.Fatalf("expected L2 metric, got %s", db.lastSearch.Metric)
	}
}

func TestMilvusConnectorSearchRejectsInvalidRequest(t *testing.T) {
	connector := mustMilvusConnector(t, &fakeMilvusDB{})
	tests := []struct {
		name string
		req  SearchRequest
	}{
		{name: "empty query vector", req: SearchRequest{TopK: 1, ExpandK: 1}},
		{name: "zero top k", req: SearchRequest{QueryVector: []float64{0.1}, TopK: 0, ExpandK: 1}},
		{name: "expand below top k", req: SearchRequest{QueryVector: []float64{0.1}, TopK: 2, ExpandK: 1}},
		{name: "unsafe collection", req: SearchRequest{Collection: "items;drop", QueryVector: []float64{0.1}, TopK: 1, ExpandK: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := connector.Search(context.Background(), tt.req)
			if err == nil {
				t.Fatalf("expected invalid request to fail")
			}
		})
	}
}

func TestMilvusConnectorSearchPropagatesContextCancellation(t *testing.T) {
	db := &fakeMilvusDB{}
	connector := mustMilvusConnector(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := connector.Search(ctx, SearchRequest{QueryVector: []float64{0.1}, TopK: 1, ExpandK: 1})
	if err == nil {
		t.Fatalf("expected canceled context to fail")
	}
}

func TestMilvusConnectorCloseCallsAdapter(t *testing.T) {
	db := &fakeMilvusDB{}
	connector := mustMilvusConnector(t, db)

	if err := connector.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !db.closed {
		t.Fatalf("expected adapter Close to be called")
	}
}

func mustMilvusConnector(t *testing.T, db milvusDB) MilvusConnector {
	t.Helper()
	connector, err := NewMilvusConnector(MilvusConfig{}, db)
	if err != nil {
		t.Fatalf("NewMilvusConnector returned error: %v", err)
	}
	return connector
}

type fakeMilvusDB struct {
	connected           bool
	closed              bool
	count               int64
	hits                []milvusRawHit
	lastCountCollection string
	lastSearch          milvusSearchRequest
}

func (db *fakeMilvusDB) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db.connected = true
	return nil
}

func (db *fakeMilvusDB) Count(ctx context.Context, collection string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	db.lastCountCollection = collection
	return db.count, nil
}

func (db *fakeMilvusDB) Search(ctx context.Context, req milvusSearchRequest) ([]milvusRawHit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db.lastSearch = req
	return append([]milvusRawHit(nil), db.hits...), nil
}

func (db *fakeMilvusDB) Close() error {
	db.closed = true
	return nil
}

var errMilvusFake = errors.New("fake milvus error")

func TestMilvusConnectorConnectPropagatesAdapterError(t *testing.T) {
	db := &failingMilvusDB{err: errMilvusFake}
	connector := mustMilvusConnector(t, db)

	err := connector.Connect(context.Background())
	if !errors.Is(err, errMilvusFake) {
		t.Fatalf("expected adapter error, got %v", err)
	}
}

type failingMilvusDB struct {
	err error
}

func (db *failingMilvusDB) Connect(ctx context.Context) error {
	return db.err
}

func (db *failingMilvusDB) Count(ctx context.Context, collection string) (int64, error) {
	return 0, db.err
}

func (db *failingMilvusDB) Search(ctx context.Context, req milvusSearchRequest) ([]milvusRawHit, error) {
	return nil, db.err
}

func (db *failingMilvusDB) Close() error {
	return db.err
}

func TestMilvusIdentifierRejectsUnsafeNames(t *testing.T) {
	unsafeNames := []string{"", "1items", "items;drop", "public.items", "items-name"}
	for _, name := range unsafeNames {
		if err := validateMilvusIdentifier("test", name); err == nil {
			t.Fatalf("expected unsafe name %q to fail", name)
		}
	}
	if err := validateMilvusIdentifier("test", "items_2026"); err != nil {
		t.Fatalf("expected safe name to pass: %v", err)
	}
}

func TestMilvusConnectorSearchErrorIncludesContext(t *testing.T) {
	db := &failingMilvusDB{err: errMilvusFake}
	connector := mustMilvusConnector(t, db)

	_, err := connector.Search(context.Background(), SearchRequest{QueryVector: []float64{0.1}, TopK: 1, ExpandK: 1})
	if err == nil || !strings.Contains(err.Error(), "milvus search") {
		t.Fatalf("expected contextual search error, got %v", err)
	}
}
