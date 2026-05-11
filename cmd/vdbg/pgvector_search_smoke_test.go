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

func TestParseSearchPGVectorOptions(t *testing.T) {
	options, err := parseSearchPGVectorOptions([]string{
		"--fixture", "fixture.json",
		"--connection-url", "postgres://[REDACTED]",
		"--table", "vectors",
		"--top-k", "3",
		"--expand-k", "5",
		"--query-index", "2",
		"--metric", "l2",
	})
	if err != nil {
		t.Fatalf("parseSearchPGVectorOptions returned error: %v", err)
	}
	if options.FixturePath != "fixture.json" {
		t.Fatalf("unexpected fixture path: %s", options.FixturePath)
	}
	if options.ConnectionURL != "postgres://[REDACTED]" {
		t.Fatalf("unexpected connection URL: %s", options.ConnectionURL)
	}
	if options.Collection != "vectors" {
		t.Fatalf("unexpected table: %s", options.Collection)
	}
	if options.TopK != 3 || options.ExpandK != 5 || options.QueryIndex != 2 {
		t.Fatalf("unexpected search options: %#v", options)
	}
	if options.Metric != connectors.PGVectorMetricL2 {
		t.Fatalf("unexpected metric: %s", options.Metric)
	}
}

func TestParseSearchPGVectorOptionsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing fixture", args: []string{"--connection-url", "postgres://[REDACTED]"}, want: "fixture"},
		{name: "missing connection url", args: []string{"--fixture", "fixture.json"}, want: "connection-url"},
		{name: "zero top k", args: []string{"--fixture", "fixture.json", "--connection-url", "postgres://[REDACTED]", "--top-k", "0"}, want: "top-k"},
		{name: "expand below top k", args: []string{"--fixture", "fixture.json", "--connection-url", "postgres://[REDACTED]", "--top-k", "5", "--expand-k", "4"}, want: "expand-k"},
		{name: "negative query index", args: []string{"--fixture", "fixture.json", "--connection-url", "postgres://[REDACTED]", "--query-index", "-1"}, want: "query-index"},
		{name: "unsupported metric", args: []string{"--fixture", "fixture.json", "--connection-url", "postgres://[REDACTED]", "--metric", "dot"}, want: "metric"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSearchPGVectorOptions(tt.args)
			if err == nil {
				t.Fatalf("expected invalid options to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestRunSearchPGVectorWithInjectedConnector(t *testing.T) {
	fixturePath := writeSearchPGVectorFixture(t, syntheticSearchPGVectorDataset())
	fake := &fakeSearchPGVectorConnector{
		count: 2,
		response: connectors.SearchResponse{Hits: []connectors.SearchHit{
			{ID: "vec-000001", Rank: 1, Score: 0.99},
			{ID: "vec-000002", Rank: 2, Score: 0.87},
		}},
	}
	err := runSearchPGVectorWithFactory(context.Background(), []string{
		"--fixture", fixturePath,
		"--connection-url", "postgres://[REDACTED]",
		"--table", "items",
		"--top-k", "1",
		"--expand-k", "2",
		"--query-index", "0",
	}, fake.newConnector)
	if err != nil {
		t.Fatalf("runSearchPGVectorWithFactory returned error: %v", err)
	}
	if fake.connectionURL != "postgres://[REDACTED]" {
		t.Fatalf("unexpected connection URL: %s", fake.connectionURL)
	}
	if fake.config.DefaultTable != "items" || fake.config.Metric != connectors.PGVectorMetricCosine {
		t.Fatalf("unexpected config: %#v", fake.config)
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

func TestRunSearchPGVectorRejectsMissingQuery(t *testing.T) {
	fixturePath := writeSearchPGVectorFixture(t, fixtures.SyntheticDataset{
		Dimension: 3,
		Records:   []fixtures.SyntheticVector{{ID: "vec-000001", Vector: []float64{0.1, 0.2, 0.3}}},
		Queries:   nil,
	})
	err := runSearchPGVectorWithFactory(context.Background(), []string{
		"--fixture", fixturePath,
		"--connection-url", "postgres://[REDACTED]",
	}, func(string, connectors.PGVectorConfig) (pgvectorSearchConnector, error) {
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

func writeSearchPGVectorFixture(t *testing.T, dataset fixtures.SyntheticDataset) string {
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

func syntheticSearchPGVectorDataset() fixtures.SyntheticDataset {
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

type fakeSearchPGVectorConnector struct {
	connectionURL string
	config        connectors.PGVectorConfig
	connected     bool
	closed        bool
	count         int64
	collection    string
	searchReq     connectors.SearchRequest
	response      connectors.SearchResponse
}

func (f *fakeSearchPGVectorConnector) newConnector(connectionURL string, config connectors.PGVectorConfig) (pgvectorSearchConnector, error) {
	f.connectionURL = connectionURL
	f.config = config
	return f, nil
}

func (f *fakeSearchPGVectorConnector) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.connected = true
	return nil
}

func (f *fakeSearchPGVectorConnector) Count(ctx context.Context, collection string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.collection = collection
	return f.count, nil
}

func (f *fakeSearchPGVectorConnector) Search(ctx context.Context, req connectors.SearchRequest) (connectors.SearchResponse, error) {
	if err := ctx.Err(); err != nil {
		return connectors.SearchResponse{}, err
	}
	f.searchReq = req
	return f.response, nil
}

func (f *fakeSearchPGVectorConnector) Close() error {
	f.closed = true
	return nil
}
