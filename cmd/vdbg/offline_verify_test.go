package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/engine"
)

type fakeEngine struct {
	out engine.CompareOutput
}

func (f fakeEngine) Compare(ctx context.Context, input engine.CompareInput) (engine.CompareOutput, error) {
	if err := ctx.Err(); err != nil {
		return engine.CompareOutput{}, err
	}
	if f.out.JobID == "" {
		f.out.JobID = input.JobID
	}
	return f.out, nil
}

func TestLoadOfflineFixtureParsesFixture(t *testing.T) {
	fixture, err := loadOfflineFixture(filepath.Join("..", "..", "testdata", "offline", "basic.json"))
	if err != nil {
		t.Fatalf("expected fixture to load: %v", err)
	}

	if fixture.JobID != "offline-basic" {
		t.Fatalf("unexpected job id: %s", fixture.JobID)
	}
	if fixture.TopK != 3 || fixture.ExpandK != 4 || fixture.StableK != 2 || fixture.BoundaryK != 1 {
		t.Fatalf("unexpected fixture options: %#v", fixture)
	}
	if len(fixture.Queries) != 1 {
		t.Fatalf("expected one query, got %d", len(fixture.Queries))
	}
	query := fixture.Queries[0]
	if query.QueryID != "q-1" {
		t.Fatalf("unexpected query id: %s", query.QueryID)
	}
	if query.SourceHits[0].ID != "a" || query.TargetHits[2].ID != "x" {
		t.Fatalf("unexpected fixture hits: %#v", query)
	}
}

func TestBuildOfflinePipelineInputsCreatesConnectorsAndRequest(t *testing.T) {
	fixture := offlineFixture{
		JobID:     "job-1",
		TopK:      3,
		ExpandK:   4,
		StableK:   2,
		BoundaryK: 1,
		Queries: []offlineQueryFixture{
			{
				QueryID:    "q-1",
				SourceHits: []offlineHitFixture{{ID: "a", Rank: 1, Score: 0.99}, {ID: "b", Rank: 2, Score: 0.95}, {ID: "c", Rank: 3, Score: 0.90}, {ID: "d", Rank: 4, Score: 0.85}},
				TargetHits: []offlineHitFixture{{ID: "a", Rank: 1, Score: 0.99}, {ID: "b", Rank: 2, Score: 0.95}, {ID: "x", Rank: 3, Score: 0.89}, {ID: "d", Rank: 4, Score: 0.85}},
			},
		},
	}

	inputs, err := buildOfflinePipelineInputs(fixture)
	if err != nil {
		t.Fatalf("expected inputs to build: %v", err)
	}

	if inputs.Request.JobID != "job-1" || inputs.Request.TopK != 3 || inputs.Request.ExpandK != 4 {
		t.Fatalf("unexpected request: %#v", inputs.Request)
	}
	if len(inputs.Request.QueryIDs) != 1 || inputs.Request.QueryIDs[0] != "q-1" {
		t.Fatalf("unexpected query ids: %#v", inputs.Request.QueryIDs)
	}
	if inputs.BuildOptions.TopK != 3 || inputs.BuildOptions.StableK != 2 || inputs.BuildOptions.BoundaryK != 1 {
		t.Fatalf("unexpected build options: %#v", inputs.BuildOptions)
	}
}

func TestRunOfflineVerifyRejectsMissingFixture(t *testing.T) {
	_, err := runOfflineVerify(context.Background(), offlineVerifyOptions{ArtifactDir: t.TempDir()}, fakeEngine{})

	if err == nil {
		t.Fatal("expected missing fixture error")
	}
	if !strings.Contains(err.Error(), "fixture") {
		t.Fatalf("expected fixture error, got %v", err)
	}
}

func TestRunOfflineVerifyRejectsInvalidFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte(`{"job_id":"bad","top_k":1,"expand_k":1,"stable_k":1,"boundary_k":1,"queries":[]}`), 0o600); err != nil {
		t.Fatalf("write invalid fixture: %v", err)
	}

	_, err := runOfflineVerify(context.Background(), offlineVerifyOptions{FixturePath: path, ArtifactDir: t.TempDir()}, fakeEngine{})

	if err == nil {
		t.Fatal("expected invalid fixture error")
	}
	if !strings.Contains(err.Error(), "queries") {
		t.Fatalf("expected queries error, got %v", err)
	}
}

func TestRunOfflineVerifyRunsWithInjectedEngine(t *testing.T) {
	artifactDir := t.TempDir()
	result, err := runOfflineVerify(context.Background(), offlineVerifyOptions{
		FixturePath: filepath.Join("..", "..", "testdata", "offline", "basic.json"),
		ArtifactDir: artifactDir,
	}, fakeEngine{out: engine.CompareOutput{
		JobID:            "offline-basic",
		ConsistencyScore: 0.76,
		Metrics: engine.MetricSummary{
			FingerprintDistance: 0.24,
			MatchedQueryCount:   1,
		},
	}})
	if err != nil {
		t.Fatalf("expected offline verify to succeed: %v", err)
	}

	if result.VerificationResult.Output.ConsistencyScore != 0.76 {
		t.Fatalf("unexpected score: %f", result.VerificationResult.Output.ConsistencyScore)
	}
	assertPathExists(t, filepath.Join(artifactDir, "offline-basic-source-fingerprint.json"))
	assertPathExists(t, filepath.Join(artifactDir, "offline-basic-target-fingerprint.json"))
	assertPathExists(t, filepath.Join(artifactDir, "offline-basic-result.json"))
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected path %s to exist: %v", path, err)
	}
}
