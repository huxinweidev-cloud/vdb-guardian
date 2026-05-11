package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/h3xwave/vdb-guardian/internal/engine"
	"github.com/h3xwave/vdb-guardian/internal/fingerprints"
	"github.com/h3xwave/vdb-guardian/internal/jobs"
)

type fakeEngine struct {
	out engine.CompareOutput
	err error
}

func (f fakeEngine) Compare(ctx context.Context, input engine.CompareInput) (engine.CompareOutput, error) {
	if err := ctx.Err(); err != nil {
		return engine.CompareOutput{}, err
	}
	if f.err != nil {
		return engine.CompareOutput{}, f.err
	}
	if f.out.JobID == "" {
		f.out.JobID = input.JobID
	}
	return f.out, nil
}

func TestOfflinePipelineRunWritesArtifactsAndResult(t *testing.T) {
	artifactDir := t.TempDir()
	source := connectors.NewMemoryConnector("source", map[string][]connectors.SearchHit{
		"q-1": connectorHits("a", "b", "c", "d"),
	})
	target := connectors.NewMemoryConnector("target", map[string][]connectors.SearchHit{
		"q-1": connectorHits("a", "b", "x", "d"),
	})
	runner := jobs.NewVerificationRunner(fakeEngine{out: engine.CompareOutput{
		JobID:            "job-1",
		ConsistencyScore: 0.76,
		Metrics: engine.MetricSummary{
			FingerprintDistance:       0.24,
			StableNeighborDistance:    0.25,
			BoundaryCandidateDistance: 0.10,
			BoundaryFlipRate:          0.20,
			MatchedQueryCount:         1,
		},
	}}, artifactDir)
	pipeline := NewOfflinePipeline(source, target, runner, artifactDir, fingerprints.BuildOptions{
		TopK:      3,
		StableK:   2,
		BoundaryK: 1,
	})

	result, err := pipeline.Run(context.Background(), OfflineRequest{
		JobID:    "job-1",
		QueryIDs: []string{"q-1"},
		TopK:     3,
		ExpandK:  4,
	})
	if err != nil {
		t.Fatalf("expected pipeline to succeed: %v", err)
	}

	if result.SourceFingerprintPath != filepath.Join(artifactDir, "job-1-source-fingerprint.json") {
		t.Fatalf("unexpected source artifact path: %s", result.SourceFingerprintPath)
	}
	if result.TargetFingerprintPath != filepath.Join(artifactDir, "job-1-target-fingerprint.json") {
		t.Fatalf("unexpected target artifact path: %s", result.TargetFingerprintPath)
	}
	if result.VerificationResult.ResultPath != filepath.Join(artifactDir, "job-1-result.json") {
		t.Fatalf("unexpected result artifact path: %s", result.VerificationResult.ResultPath)
	}
	assertFileExists(t, result.SourceFingerprintPath)
	assertFileExists(t, result.TargetFingerprintPath)
	assertFileExists(t, result.VerificationResult.ResultPath)
	assertFingerprintIDs(t, result.SourceFingerprintPath, "q-1", []string{"a", "b"}, []string{"c", "d"}, []string{"a", "b", "c"})
	if result.VerificationResult.Output.ConsistencyScore != 0.76 {
		t.Fatalf("unexpected consistency score: %f", result.VerificationResult.Output.ConsistencyScore)
	}
}

func TestOfflinePipelineRunRejectsMissingConnectors(t *testing.T) {
	pipeline := NewOfflinePipeline(nil, nil, jobs.VerificationRunner{}, t.TempDir(), fingerprints.BuildOptions{TopK: 2, StableK: 1, BoundaryK: 1})

	_, err := pipeline.Run(context.Background(), OfflineRequest{JobID: "job-1", QueryIDs: []string{"q-1"}, TopK: 2, ExpandK: 3})

	if err == nil {
		t.Fatal("expected missing connector error")
	}
	if !strings.Contains(err.Error(), "connector") {
		t.Fatalf("expected connector error, got %v", err)
	}
}

func TestOfflinePipelineRunRejectsMissingQueryIDs(t *testing.T) {
	connector := connectors.NewMemoryConnector("memory", map[string][]connectors.SearchHit{"q-1": connectorHits("a", "b", "c")})
	pipeline := NewOfflinePipeline(connector, connector, jobs.NewVerificationRunner(fakeEngine{}, t.TempDir()), t.TempDir(), fingerprints.BuildOptions{TopK: 2, StableK: 1, BoundaryK: 1})

	_, err := pipeline.Run(context.Background(), OfflineRequest{JobID: "job-1", TopK: 2, ExpandK: 3})

	if err == nil {
		t.Fatal("expected missing query ids error")
	}
	if !strings.Contains(err.Error(), "query_ids") {
		t.Fatalf("expected query_ids error, got %v", err)
	}
}

func TestOfflinePipelineRunPropagatesConnectorError(t *testing.T) {
	artifactDir := t.TempDir()
	connector := connectors.NewMemoryConnector("memory", map[string][]connectors.SearchHit{"q-1": connectorHits("a", "b", "c")})
	pipeline := NewOfflinePipeline(connector, connector, jobs.NewVerificationRunner(fakeEngine{}, artifactDir), artifactDir, fingerprints.BuildOptions{TopK: 2, StableK: 1, BoundaryK: 1})

	_, err := pipeline.Run(context.Background(), OfflineRequest{JobID: "job-1", QueryIDs: []string{"missing"}, TopK: 2, ExpandK: 3})

	if err == nil {
		t.Fatal("expected connector error")
	}
	if !strings.Contains(err.Error(), "source search") {
		t.Fatalf("expected source search context, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(artifactDir, "job-1-result.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no success result artifact, stat err: %v", statErr)
	}
}

func TestOfflinePipelineRunHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	connector := connectors.NewMemoryConnector("memory", map[string][]connectors.SearchHit{"q-1": connectorHits("a", "b", "c")})
	pipeline := NewOfflinePipeline(connector, connector, jobs.NewVerificationRunner(fakeEngine{}, t.TempDir()), t.TempDir(), fingerprints.BuildOptions{TopK: 2, StableK: 1, BoundaryK: 1})

	_, err := pipeline.Run(ctx, OfflineRequest{JobID: "job-1", QueryIDs: []string{"q-1"}, TopK: 2, ExpandK: 3})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func connectorHits(ids ...string) []connectors.SearchHit {
	hits := make([]connectors.SearchHit, 0, len(ids))
	for i, id := range ids {
		hits = append(hits, connectors.SearchHit{ID: id, Rank: i + 1, Score: 1.0 - float64(i)*0.01})
	}
	return hits
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
}

func assertFingerprintIDs(t *testing.T, path string, queryID string, stable []string, boundary []string, topK []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fingerprint artifact: %v", err)
	}
	var artifact fingerprints.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("unmarshal fingerprint artifact: %v", err)
	}
	if len(artifact.Fingerprints) != 1 {
		t.Fatalf("expected one fingerprint, got %#v", artifact.Fingerprints)
	}
	fingerprint := artifact.Fingerprints[0]
	if fingerprint.QueryID != queryID {
		t.Fatalf("expected query %s, got %s", queryID, fingerprint.QueryID)
	}
	assertStrings(t, fingerprint.StableNeighbors, stable)
	assertStrings(t, fingerprint.BoundaryCandidates, boundary)
	assertStrings(t, fingerprint.TopKIDs, topK)
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
