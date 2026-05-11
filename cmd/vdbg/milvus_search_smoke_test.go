package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/h3xwave/vdb-guardian/internal/fixtures"
)

func TestParseSearchMilvusOptions(t *testing.T) {
	options, err := parseSearchMilvusOptions([]string{
		"--fixture", "fixture.json",
		"--address", "localhost:19530",
		"--collection", "vectors",
		"--id-field", "vector_id",
		"--vector-field", "embedding",
		"--top-k", "3",
		"--expand-k", "5",
		"--query-index", "2",
		"--metric", "l2",
	})
	if err != nil {
		t.Fatalf("parseSearchMilvusOptions returned error: %v", err)
	}
	if options.FixturePath != "fixture.json" {
		t.Fatalf("unexpected fixture path: %s", options.FixturePath)
	}
	if options.Address != "localhost:19530" {
		t.Fatalf("unexpected address: %s", options.Address)
	}
	if options.Collection != "vectors" {
		t.Fatalf("unexpected collection: %s", options.Collection)
	}
	if options.IDField != "vector_id" || options.VectorField != "embedding" {
		t.Fatalf("unexpected fields: %#v", options)
	}
	if options.TopK != 3 || options.ExpandK != 5 || options.QueryIndex != 2 {
		t.Fatalf("unexpected search options: %#v", options)
	}
	if options.Metric != connectors.MilvusMetricL2 {
		t.Fatalf("unexpected metric: %s", options.Metric)
	}
}

func TestParseSearchMilvusOptionsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing fixture", args: []string{"--address", "localhost:19530"}, want: "fixture"},
		{name: "missing address", args: []string{"--fixture", "fixture.json"}, want: "address"},
		{name: "zero top k", args: []string{"--fixture", "fixture.json", "--address", "localhost:19530", "--top-k", "0"}, want: "top-k"},
		{name: "expand below top k", args: []string{"--fixture", "fixture.json", "--address", "localhost:19530", "--top-k", "5", "--expand-k", "4"}, want: "expand-k"},
		{name: "negative query index", args: []string{"--fixture", "fixture.json", "--address", "localhost:19530", "--query-index", "-1"}, want: "query-index"},
		{name: "unsupported metric", args: []string{"--fixture", "fixture.json", "--address", "localhost:19530", "--metric", "dot"}, want: "metric"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSearchMilvusOptions(tt.args)
			if err == nil {
				t.Fatalf("expected invalid options to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestRunSearchMilvusWithInjectedConnector(t *testing.T) {
	fixturePath := writeSearchMilvusFixture(t, syntheticSearchMilvusDataset())
	fake := &fakeSearchMilvusConnector{
		count: 2,
		response: connectors.SearchResponse{Hits: []connectors.SearchHit{
			{ID: "vec-000001", Rank: 1, Score: 0.99},
			{ID: "vec-000002", Rank: 2, Score: 0.87},
		}},
	}
	err := runSearchMilvusWithFactory(context.Background(), []string{
		"--fixture", fixturePath,
		"--address", "localhost:19530",
		"--collection", "items",
		"--id-field", "id",
		"--vector-field", "embedding",
		"--top-k", "1",
		"--expand-k", "2",
		"--query-index", "0",
	}, fake.newConnector)
	if err != nil {
		t.Fatalf("runSearchMilvusWithFactory returned error: %v", err)
	}
	if fake.address != "localhost:19530" {
		t.Fatalf("unexpected address: %s", fake.address)
	}
	if fake.config.Address != "localhost:19530" || fake.config.DefaultCollection != "items" || fake.config.Metric != connectors.MilvusMetricCosine {
		t.Fatalf("unexpected config: %#v", fake.config)
	}
	if fake.config.IDField != "id" || fake.config.VectorField != "embedding" {
		t.Fatalf("unexpected field config: %#v", fake.config)
	}
	if !fake.connected || !fake.closed {
		t.Fatalf("expected connector to connect and close")
	}
	if fake.collection != "items" {
		t.Fatalf("unexpected count collection: %s", fake.collection)
	}
	if fake.searchReq.Collection != "items" || fake.searchReq.TopK != 1 || fake.searchReq.ExpandK != 2 {
		t.Fatalf("unexpected search request: %#v", fake.searchReq)
	}
	if len(fake.searchReq.QueryVector) != 3 || fake.searchReq.QueryVector[0] != 0.7 {
		t.Fatalf("unexpected query vector: %#v", fake.searchReq.QueryVector)
	}
}

func TestRunSearchMilvusRejectsMissingQuery(t *testing.T) {
	fixturePath := writeSearchMilvusFixture(t, fixtures.SyntheticDataset{
		Dimension: 3,
		Records:   []fixtures.SyntheticVector{{ID: "vec-000001", Vector: []float64{0.1, 0.2, 0.3}}},
		Queries:   nil,
	})
	err := runSearchMilvusWithFactory(context.Background(), []string{
		"--fixture", fixturePath,
		"--address", "localhost:19530",
	}, func(string, connectors.MilvusConfig) (milvusSearchConnector, error) {
		t.Fatalf("factory should not be called when fixture has no query")
		return nil, nil
	})
	if err == nil {
		t.Fatalf("expected missing query to fail")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Fatalf("expected query error, got %v", err)
	}
}

func writeSearchMilvusFixture(t *testing.T, dataset fixtures.SyntheticDataset) string {
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

func syntheticSearchMilvusDataset() fixtures.SyntheticDataset {
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

type fakeSearchMilvusConnector struct {
	address    string
	config     connectors.MilvusConfig
	connected  bool
	closed     bool
	count      int64
	collection string
	searchReq  connectors.SearchRequest
	response   connectors.SearchResponse
}

func (f *fakeSearchMilvusConnector) newConnector(address string, config connectors.MilvusConfig) (milvusSearchConnector, error) {
	f.address = address
	f.config = config
	return f, nil
}

func (f *fakeSearchMilvusConnector) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.connected = true
	return nil
}

func (f *fakeSearchMilvusConnector) Count(ctx context.Context, collection string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.collection = collection
	return f.count, nil
}

func (f *fakeSearchMilvusConnector) Search(ctx context.Context, req connectors.SearchRequest) (connectors.SearchResponse, error) {
	if err := ctx.Err(); err != nil {
		return connectors.SearchResponse{}, err
	}
	f.searchReq = req
	return f.response, nil
}

func (f *fakeSearchMilvusConnector) Close() error {
	f.closed = true
	return nil
}
