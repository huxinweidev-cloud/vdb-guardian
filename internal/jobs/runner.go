package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/engine"
)

// VerificationRunner executes a local artifact-backed verification job. It is
// the first orchestration layer above the fingerprint engine: callers provide
// source and target fingerprint artifact paths, the runner invokes the configured
// engine, and the runner persists a structured result artifact for downstream
// CLI, API, and report generation.
type VerificationRunner struct {
	// Engine compares source and target retrieval behavior fingerprint artifacts.
	Engine engine.Engine
	// ArtifactDir is the local directory where job result artifacts are written.
	ArtifactDir string
}

// VerificationRequest contains the minimum local inputs required to compare two
// retrieval behavior fingerprint artifacts. Future connector-backed runners can
// build this request after collecting Milvus and pgvector query results.
type VerificationRequest struct {
	// JobID links the local verification run to logs, result artifacts, and future job storage.
	JobID string
	// SourceFingerprintPath points to source database retrieval behavior fingerprint artifact JSON.
	SourceFingerprintPath string
	// TargetFingerprintPath points to target database retrieval behavior fingerprint artifact JSON.
	TargetFingerprintPath string
}

// VerificationResult describes the completed local verification run. It includes
// the final lifecycle state, normalized engine output, and the path to the JSON
// result artifact written by the runner.
type VerificationResult struct {
	// JobID identifies the verification job associated with the result.
	JobID string
	// State is the final lifecycle state for this local verification run.
	State State
	// Output contains the normalized fingerprint comparison result returned by the engine.
	Output engine.CompareOutput
	// ResultPath points to the persisted JSON result artifact.
	ResultPath string
}

type verificationResultJSON struct {
	JobID            string               `json:"job_id"`
	State            string               `json:"state"`
	ConsistencyScore float64              `json:"consistency_score"`
	Metrics          engine.MetricSummary `json:"metrics"`
}

// NewVerificationRunner creates a local verification runner that writes result
// artifacts into artifactDir. The engine is injected so tests can use fakes while
// production code can use the Python subprocess engine or a future remote engine.
func NewVerificationRunner(engine engine.Engine, artifactDir string) VerificationRunner {
	return VerificationRunner{Engine: engine, ArtifactDir: artifactDir}
}

// Run validates the request, invokes the fingerprint engine, persists a
// result.json artifact, and returns a structured VerificationResult. It returns
// errors before writing a success artifact when validation or engine execution
// fails so callers do not mistake partial runs for completed verification jobs.
func (r VerificationRunner) Run(ctx context.Context, request VerificationRequest) (VerificationResult, error) {
	if r.Engine == nil {
		return VerificationResult{}, errors.New("verification runner engine must not be nil")
	}
	if request.JobID == "" {
		return VerificationResult{}, errors.New("verification request job id must not be empty")
	}
	if request.SourceFingerprintPath == "" || request.TargetFingerprintPath == "" {
		return VerificationResult{}, errors.New("verification request fingerprint paths must not be empty")
	}
	artifactDir := r.ArtifactDir
	if artifactDir == "" {
		artifactDir = "."
	}

	output, err := r.Engine.Compare(ctx, engine.CompareInput{
		JobID:                 request.JobID,
		SourceFingerprintPath: request.SourceFingerprintPath,
		TargetFingerprintPath: request.TargetFingerprintPath,
	})
	if err != nil {
		return VerificationResult{}, fmt.Errorf("run fingerprint engine: %w", err)
	}

	resultPath := filepath.Join(artifactDir, fmt.Sprintf("%s-result.json", request.JobID))
	if err := writeVerificationResult(resultPath, StateSucceeded, output); err != nil {
		return VerificationResult{}, err
	}
	return VerificationResult{
		JobID:      request.JobID,
		State:      StateSucceeded,
		Output:     output,
		ResultPath: resultPath,
	}, nil
}

// writeVerificationResult serializes the runner output into a durable JSON
// artifact. The artifact uses snake_case field names because it is intended for
// CLI, API, and Python-adjacent tooling rather than Go-only consumption.
func writeVerificationResult(path string, state State, output engine.CompareOutput) error {
	payload := verificationResultJSON{
		JobID:            output.JobID,
		State:            state.String(),
		ConsistencyScore: output.ConsistencyScore,
		Metrics:          output.Metrics,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal verification result: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create verification artifact dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write verification result %q: %w", path, err)
	}
	return nil
}
