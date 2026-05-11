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

func TestParsePGVectorArtifactOptions(t *testing.T) {
	options, err := parsePGVectorArtifactOptions([]string{
		"--fixture", "fixture.json",
		"--connection-url", "postgres://[REDACTED]",
		"--output", "target-fingerprint.json",
		"--table", "vectors",
		"--top-k", "4",
		"--expand-k", "7",
		"--stable-k", "3",
		"--boundary-k", "2",
		"--metric", "l2",
	})
	if err != nil {
		t.Fatalf("parsePGVectorArtifactOptions returned error: %v", err)
	}
	if options.FixturePath != "fixture.json" || options.OutputPath != "target-fingerprint.json" {
		t.Fatalf("unexpected paths: %#v", options)
	}
	if options.ConnectionURL != "postgres://[REDACTED]" || options.Collection != "vectors" {
		t.Fatalf("unexpected connection/table options: %#v", options)
	}
	if options.TopK != 4 || options.ExpandK != 7 || options.StableK != 3 || options.BoundaryK != 2 {
		t.Fatalf("unexpected build options: %#v", options)
	}
	if options.Metric != connectors.PGVectorMetricL2 {
		t.Fatalf("unexpected metric: %s", options.Metric)
	}
}

func TestParsePGVectorArtifactOptionsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing fixture", args: []string{"--connection-url", "postgres://[REDACTED]", "--output", "out.json"}, want: "fixture"},
		{name: "missing connection url", args: []string{"--fixture", "fixture.json", "--output", "out.json"}, want: "connection-url"},
		{name: "missing output", args: []string{"--fixture", "fixture.json", "--connection-url", "postgres://[REDACTED]"}, want: "output"},
		{name: "stable above top k", args: []string{"--fixture", "fixture.json", "--connection-url", "postgres://[REDACTED]", "--output", "out.json", "--top-k", "2", "--stable-k", "3"}, want: "stable-k"},
		{name: "expand below boundary", args: []string{"--fixture", "fixture.json", "--connection-url", "postgres://[REDACTED]", "--output", "out.json", "--top-k", "3", "--expand-k", "3", "--boundary-k", "2"}, want: "expand-k"},
		{name: "unsupported metric", args: []string{"--fixture", "fixture.json", "--connection-url", "postgres://[REDACTED]", "--output", "out.json", "--metric", "dot"}, want: "metric"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePGVectorArtifactOptions(tt.args)
			if err == nil {
				t.Fatalf("expected invalid options to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestRunPGVectorArtifactWithInjectedConnectorWritesArtifact(t *testing.T) {
	fixturePath := writeSearchPGVectorFixture(t, syntheticSearchPGVectorDataset())
	outputPath := filepath.Join(t.TempDir(), "target-fingerprint.json")
	fake := &fakePGVectorArtifactConnector{
		response: connectors.SearchResponse{Hits: []connectors.SearchHit{
			{ID: "vec-000002", Rank: 2, Score: 0.9},
			{ID: "vec-000001", Rank: 1, Score: 0.99},
			{ID: "vec-000003", Rank: 3, Score: 0.8},
			{ID: "vec-000004", Rank: 4, Score: 0.7},
		}},
	}
	err := runPGVectorArtifactWithFactory(context.Background(), []string{
		"--fixture", fixturePath,
		"--connection-url", "postgres://[REDACTED]",
		"--output", outputPath,
		"--table", "items",
		"--top-k", "3",
		"--expand-k", "4",
		"--stable-k", "2",
		"--boundary-k", "1",
	}, fake.newConnector)
	if err != nil {
		t.Fatalf("runPGVectorArtifactWithFactory returned error: %v", err)
	}
	if !fake.connected || !fake.closed {
		t.Fatalf("expected connector to connect and close")
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

type fakePGVectorArtifactConnector struct {
	connected      bool
	closed         bool
	searchRequests []connectors.SearchRequest
	response       connectors.SearchResponse
}

func (f *fakePGVectorArtifactConnector) newConnector(string, connectors.PGVectorConfig) (pgvectorSearchConnector, error) {
	return f, nil
}

func (f *fakePGVectorArtifactConnector) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.connected = true
	return nil
}

func (f *fakePGVectorArtifactConnector) Count(ctx context.Context, collection string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, nil
}

func (f *fakePGVectorArtifactConnector) Search(ctx context.Context, req connectors.SearchRequest) (connectors.SearchResponse, error) {
	if err := ctx.Err(); err != nil {
		return connectors.SearchResponse{}, err
	}
	f.searchRequests = append(f.searchRequests, req)
	return f.response, nil
}

func (f *fakePGVectorArtifactConnector) Close() error {
	f.closed = true
	return nil
}

func assertEqualStringSlices(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
