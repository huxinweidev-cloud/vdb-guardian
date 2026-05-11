package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/engine"
)

type fakeEngine struct {
	output engine.CompareOutput
	err    error
	input  engine.CompareInput
}

func (f *fakeEngine) Compare(_ context.Context, input engine.CompareInput) (engine.CompareOutput, error) {
	f.input = input
	if f.err != nil {
		return engine.CompareOutput{}, f.err
	}
	return f.output, nil
}

func TestVerificationRunnerRunWritesResultArtifact(t *testing.T) {
	artifactDir := t.TempDir()
	fake := &fakeEngine{output: engine.CompareOutput{
		JobID:            "job-1",
		ConsistencyScore: 0.76,
		Metrics: engine.MetricSummary{
			FingerprintDistance:       0.24,
			StableNeighborDistance:    0.25,
			BoundaryCandidateDistance: 0.10,
			BoundaryFlipRate:          0.20,
			MatchedQueryCount:         10,
			MissingSourceQueryCount:   0,
			MissingTargetQueryCount:   0,
		},
	}}

	runner := NewVerificationRunner(fake, artifactDir)
	result, err := runner.Run(context.Background(), VerificationRequest{
		JobID:                 "job-1",
		SourceFingerprintPath: "/tmp/source.json",
		TargetFingerprintPath: "/tmp/target.json",
	})

	if err != nil {
		t.Fatalf("expected runner to succeed: %v", err)
	}
	if result.State != StateSucceeded {
		t.Fatalf("expected state SUCCEEDED, got %s", result.State)
	}
	if result.ResultPath == "" {
		t.Fatal("expected result path to be populated")
	}
	if _, err := os.Stat(result.ResultPath); err != nil {
		t.Fatalf("expected result artifact to exist: %v", err)
	}
	if fake.input.SourceFingerprintPath != "/tmp/source.json" || fake.input.TargetFingerprintPath != "/tmp/target.json" {
		t.Fatalf("expected runner to pass fingerprint paths to engine, got %#v", fake.input)
	}

	payload := readResultArtifact(t, result.ResultPath)
	if payload.JobID != "job-1" {
		t.Fatalf("expected result job id job-1, got %q", payload.JobID)
	}
	if payload.State != StateSucceeded.String() {
		t.Fatalf("expected result state SUCCEEDED, got %q", payload.State)
	}
	if payload.ConsistencyScore != 0.76 {
		t.Fatalf("expected consistency score 0.76, got %f", payload.ConsistencyScore)
	}
	if payload.Metrics.FingerprintDistance != 0.24 {
		t.Fatalf("expected fingerprint distance 0.24, got %f", payload.Metrics.FingerprintDistance)
	}
}

func TestVerificationRunnerRunRejectsMissingEngine(t *testing.T) {
	runner := NewVerificationRunner(nil, t.TempDir())

	_, err := runner.Run(context.Background(), VerificationRequest{
		JobID:                 "job-1",
		SourceFingerprintPath: "/tmp/source.json",
		TargetFingerprintPath: "/tmp/target.json",
	})

	if err == nil {
		t.Fatal("expected missing engine to return an error")
	}
	if !strings.Contains(err.Error(), "engine") {
		t.Fatalf("expected error to mention engine, got %v", err)
	}
}

func TestVerificationRunnerRunRejectsMissingRequiredPaths(t *testing.T) {
	runner := NewVerificationRunner(&fakeEngine{}, t.TempDir())

	_, err := runner.Run(context.Background(), VerificationRequest{JobID: "job-1"})

	if err == nil {
		t.Fatal("expected missing fingerprint paths to return an error")
	}
	if !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("expected error to mention fingerprint paths, got %v", err)
	}
}

func TestVerificationRunnerRunPropagatesEngineError(t *testing.T) {
	artifactDir := t.TempDir()
	runner := NewVerificationRunner(&fakeEngine{err: errors.New("engine failed")}, artifactDir)

	_, err := runner.Run(context.Background(), VerificationRequest{
		JobID:                 "job-1",
		SourceFingerprintPath: "/tmp/source.json",
		TargetFingerprintPath: "/tmp/target.json",
	})

	if err == nil {
		t.Fatal("expected engine failure to return an error")
	}
	if !strings.Contains(err.Error(), "engine failed") {
		t.Fatalf("expected error to include engine failure, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(artifactDir, "job-1-result.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no success result artifact, stat err: %v", statErr)
	}
}

type resultArtifactJSON struct {
	JobID            string               `json:"job_id"`
	State            string               `json:"state"`
	ConsistencyScore float64              `json:"consistency_score"`
	Metrics          engine.MetricSummary `json:"metrics"`
}

func readResultArtifact(t *testing.T, path string) resultArtifactJSON {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result artifact: %v", err)
	}
	var payload resultArtifactJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode result artifact: %v", err)
	}
	return payload
}
