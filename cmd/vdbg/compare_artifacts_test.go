package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/engine"
)

func TestParseCompareArtifactsOptions(t *testing.T) {
	options, err := parseCompareArtifactsOptions([]string{
		"--source", "source-fingerprint.json",
		"--target", "target-fingerprint.json",
		"--artifact-dir", "/tmp/vdb-guardian-compare",
		"--job-id", "real-artifact-smoke",
	})
	if err != nil {
		t.Fatalf("parseCompareArtifactsOptions returned error: %v", err)
	}
	if options.SourceFingerprintPath != "source-fingerprint.json" {
		t.Fatalf("unexpected source path: %s", options.SourceFingerprintPath)
	}
	if options.TargetFingerprintPath != "target-fingerprint.json" {
		t.Fatalf("unexpected target path: %s", options.TargetFingerprintPath)
	}
	if options.ArtifactDir != "/tmp/vdb-guardian-compare" || options.JobID != "real-artifact-smoke" {
		t.Fatalf("unexpected runner options: %#v", options)
	}
}

func TestParseCompareArtifactsOptionsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing source", args: []string{"--target", "target.json", "--artifact-dir", "/tmp/out"}, want: "source"},
		{name: "missing target", args: []string{"--source", "source.json", "--artifact-dir", "/tmp/out"}, want: "target"},
		{name: "missing artifact dir", args: []string{"--source", "source.json", "--target", "target.json"}, want: "artifact-dir"},
		{name: "empty job id", args: []string{"--source", "source.json", "--target", "target.json", "--artifact-dir", "/tmp/out", "--job-id", ""}, want: "job-id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCompareArtifactsOptions(tt.args)
			if err == nil {
				t.Fatalf("expected invalid options to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestRunCompareArtifactsWithInjectedEngineWritesResult(t *testing.T) {
	artifactDir := t.TempDir()
	sourcePath := writeCompareArtifactFixture(t, "source.json")
	targetPath := writeCompareArtifactFixture(t, "target.json")
	fake := &fakeCompareArtifactsEngine{output: engine.CompareOutput{
		JobID:            "real-artifact-smoke",
		ConsistencyScore: 0.91,
		Metrics: engine.MetricSummary{
			FingerprintDistance:       0.09,
			StableNeighborDistance:    0.10,
			BoundaryCandidateDistance: 0.05,
			BoundaryFlipRate:          0.08,
			MatchedQueryCount:         10,
		},
	}}

	result, err := runCompareArtifacts(context.Background(), compareArtifactsOptions{
		SourceFingerprintPath: sourcePath,
		TargetFingerprintPath: targetPath,
		ArtifactDir:           artifactDir,
		JobID:                 "real-artifact-smoke",
	}, fake)
	if err != nil {
		t.Fatalf("runCompareArtifacts returned error: %v", err)
	}
	if fake.input.JobID != "real-artifact-smoke" {
		t.Fatalf("unexpected engine job id: %s", fake.input.JobID)
	}
	if fake.input.SourceFingerprintPath != sourcePath || fake.input.TargetFingerprintPath != targetPath {
		t.Fatalf("unexpected engine input: %#v", fake.input)
	}
	if result.ResultPath == "" {
		t.Fatalf("expected result path")
	}
	if _, err := os.Stat(result.ResultPath); err != nil {
		t.Fatalf("expected result artifact to exist: %v", err)
	}
	payload := readCompareArtifactsResult(t, result.ResultPath)
	if payload.ConsistencyScore != 0.91 || payload.Metrics.FingerprintDistance != 0.09 {
		t.Fatalf("unexpected result payload: %#v", payload)
	}
}

func TestRunCompareArtifactsRejectsMissingFiles(t *testing.T) {
	_, err := runCompareArtifacts(context.Background(), compareArtifactsOptions{
		SourceFingerprintPath: filepath.Join(t.TempDir(), "missing-source.json"),
		TargetFingerprintPath: filepath.Join(t.TempDir(), "missing-target.json"),
		ArtifactDir:           t.TempDir(),
		JobID:                 "real-artifact-smoke",
	}, &fakeCompareArtifactsEngine{})
	if err == nil {
		t.Fatalf("expected missing artifact paths to fail")
	}
	if !strings.Contains(err.Error(), "source") {
		t.Fatalf("expected source path error, got %v", err)
	}
}

type fakeCompareArtifactsEngine struct {
	output engine.CompareOutput
	input  engine.CompareInput
}

func (f *fakeCompareArtifactsEngine) Compare(ctx context.Context, input engine.CompareInput) (engine.CompareOutput, error) {
	if err := ctx.Err(); err != nil {
		return engine.CompareOutput{}, err
	}
	f.input = input
	return f.output, nil
}

type compareArtifactsResultPayload struct {
	JobID            string               `json:"job_id"`
	State            string               `json:"state"`
	ConsistencyScore float64              `json:"consistency_score"`
	Metrics          engine.MetricSummary `json:"metrics"`
}

func readCompareArtifactsResult(t *testing.T, path string) compareArtifactsResultPayload {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result artifact: %v", err)
	}
	var payload compareArtifactsResultPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode result artifact: %v", err)
	}
	return payload
}

func writeCompareArtifactFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	content := []byte(`{"fingerprints":[{"query_id":"query-000001","stable_neighbors":["vec-1","vec-2"],"boundary_candidates":["vec-3","vec-4"],"top_k_ids":["vec-1","vec-2","vec-3"]}]}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write artifact fixture: %v", err)
	}
	return path
}
