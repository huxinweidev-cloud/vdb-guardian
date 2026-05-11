package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/h3xwave/vdb-guardian/internal/fingerprints"
)

func TestParseMilvusArtifactOptions(t *testing.T) {
	options, err := parseMilvusArtifactOptions([]string{
		"--fixture", "fixture.json",
		"--address", "localhost:19530",
		"--output", "source-fingerprint.json",
		"--collection", "vectors",
		"--id-field", "vector_id",
		"--vector-field", "embedding",
		"--top-k", "4",
		"--expand-k", "7",
		"--stable-k", "3",
		"--boundary-k", "2",
		"--metric", "l2",
	})
	if err != nil {
		t.Fatalf("parseMilvusArtifactOptions returned error: %v", err)
	}
	if options.FixturePath != "fixture.json" || options.OutputPath != "source-fingerprint.json" {
		t.Fatalf("unexpected paths: %#v", options)
	}
	if options.Address != "localhost:19530" || options.Collection != "vectors" {
		t.Fatalf("unexpected address/collection options: %#v", options)
	}
	if options.IDField != "vector_id" || options.VectorField != "embedding" {
		t.Fatalf("unexpected field options: %#v", options)
	}
	if options.TopK != 4 || options.ExpandK != 7 || options.StableK != 3 || options.BoundaryK != 2 {
		t.Fatalf("unexpected build options: %#v", options)
	}
	if options.Metric != connectors.MilvusMetricL2 {
		t.Fatalf("unexpected metric: %s", options.Metric)
	}
}

func TestParseMilvusArtifactOptionsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing fixture", args: []string{"--address", "localhost:19530", "--output", "out.json"}, want: "fixture"},
		{name: "missing address", args: []string{"--fixture", "fixture.json", "--output", "out.json"}, want: "address"},
		{name: "missing output", args: []string{"--fixture", "fixture.json", "--address", "localhost:19530"}, want: "output"},
		{name: "stable above top k", args: []string{"--fixture", "fixture.json", "--address", "localhost:19530", "--output", "out.json", "--top-k", "2", "--stable-k", "3"}, want: "stable-k"},
		{name: "expand below boundary", args: []string{"--fixture", "fixture.json", "--address", "localhost:19530", "--output", "out.json", "--top-k", "3", "--expand-k", "3", "--boundary-k", "2"}, want: "expand-k"},
		{name: "unsupported metric", args: []string{"--fixture", "fixture.json", "--address", "localhost:19530", "--output", "out.json", "--metric", "dot"}, want: "metric"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMilvusArtifactOptions(tt.args)
			if err == nil {
				t.Fatalf("expected invalid options to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestRunMilvusArtifactWithInjectedConnectorWritesArtifact(t *testing.T) {
	fixturePath := writeSearchMilvusFixture(t, syntheticSearchMilvusDataset())
	outputPath := filepath.Join(t.TempDir(), "source-fingerprint.json")
	fake := &fakeMilvusArtifactConnector{
		response: connectors.SearchResponse{Hits: []connectors.SearchHit{
			{ID: "vec-000002", Rank: 2, Score: 0.9},
			{ID: "vec-000001", Rank: 1, Score: 0.99},
			{ID: "vec-000003", Rank: 3, Score: 0.8},
			{ID: "vec-000004", Rank: 4, Score: 0.7},
		}},
	}
	err := runMilvusArtifactWithFactory(context.Background(), []string{
		"--fixture", fixturePath,
		"--address", "localhost:19530",
		"--output", outputPath,
		"--collection", "items",
		"--id-field", "id",
		"--vector-field", "embedding",
		"--top-k", "3",
		"--expand-k", "4",
		"--stable-k", "2",
		"--boundary-k", "1",
	}, fake.newConnector)
	if err != nil {
		t.Fatalf("runMilvusArtifactWithFactory returned error: %v", err)
	}
	if !fake.connected || !fake.closed {
		t.Fatalf("expected connector to connect and close")
	}
	if fake.address != "localhost:19530" {
		t.Fatalf("unexpected address: %s", fake.address)
	}
	if fake.config.DefaultCollection != "items" || fake.config.IDField != "id" || fake.config.VectorField != "embedding" {
		t.Fatalf("unexpected Milvus config: %#v", fake.config)
	}
	if len(fake.searchRequests) != 1 {
		t.Fatalf("expected one search request, got %d", len(fake.searchRequests))
	}
	if fake.searchRequests[0].ExpandK != 4 || fake.searchRequests[0].TopK != 3 {
		t.Fatalf("unexpected search request: %#v", fake.searchRequests[0])
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output artifact: %v", err)
	}
	var artifact fingerprints.Artifact
	if err := json.Unmarshal(content, &artifact); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	if len(artifact.Fingerprints) != 1 {
		t.Fatalf("expected one fingerprint, got %d", len(artifact.Fingerprints))
	}
	fingerprint := artifact.Fingerprints[0]
	assertEqualStringSlices(t, fingerprint.TopKIDs, []string{"vec-000001", "vec-000002", "vec-000003"})
	assertEqualStringSlices(t, fingerprint.StableNeighbors, []string{"vec-000001", "vec-000002"})
	assertEqualStringSlices(t, fingerprint.BoundaryCandidates, []string{"vec-000003", "vec-000004"})
}

type fakeMilvusArtifactConnector struct {
	address        string
	config         connectors.MilvusConfig
	connected      bool
	closed         bool
	searchRequests []connectors.SearchRequest
	response       connectors.SearchResponse
}

func (f *fakeMilvusArtifactConnector) newConnector(address string, config connectors.MilvusConfig) (milvusSearchConnector, error) {
	f.address = address
	f.config = config
	return f, nil
}

func (f *fakeMilvusArtifactConnector) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.connected = true
	return nil
}

func (f *fakeMilvusArtifactConnector) Count(ctx context.Context, collection string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, nil
}

func (f *fakeMilvusArtifactConnector) Search(ctx context.Context, req connectors.SearchRequest) (connectors.SearchResponse, error) {
	if err := ctx.Err(); err != nil {
		return connectors.SearchResponse{}, err
	}
	f.searchRequests = append(f.searchRequests, req)
	return f.response, nil
}

func (f *fakeMilvusArtifactConnector) Close() error {
	f.closed = true
	return nil
}
