package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// PythonRunner invokes the Python retrieval behavior fingerprint engine through
// a subprocess. It preserves the Engine interface so the Go control plane can
// later replace subprocess execution with gRPC, HTTP, or a native Go engine
// without changing job orchestration code.
//
// PythonRunner 通过子进程 (subprocess) 唤起 Python 编写的检索行为指纹比对引擎。
// 它严格遵循了 Engine 接口契约，这意味着在未来，Go 控制平面可以在完全不修改
// 作业编排代码的前提下，将子进程执行平滑替换为 gRPC、HTTP 接口甚至纯原生 Go 引擎。
type PythonRunner struct {
	// PythonPath is the Python executable used to run the installed fingerprint engine module.
	PythonPath string
	// Module is the Python module executed with `python -m`; defaults to vdb_fingerprint_engine.cli.
	Module string
	// WorkDir is the working directory for the Python subprocess.
	WorkDir string
}

type compareInputJSON struct {
	JobID                 string `json:"job_id"`
	SourceFingerprintPath string `json:"source_fingerprint_path"`
	TargetFingerprintPath string `json:"target_fingerprint_path"`
}

type compareOutputJSON struct {
	JobID            string            `json:"job_id"`
	ConsistencyScore float64           `json:"consistency_score"`
	Metrics          metricSummaryJSON `json:"metrics"`
}

type metricSummaryJSON struct {
	FingerprintDistance       float64 `json:"fingerprint_distance"`
	StableNeighborDistance    float64 `json:"stable_neighbor_distance"`
	BoundaryCandidateDistance float64 `json:"boundary_candidate_distance"`
	BoundaryFlipRate          float64 `json:"boundary_flip_rate"`
	MatchedQueryCount         int     `json:"matched_query_count"`
	MissingSourceQueryCount   int     `json:"missing_source_query_count"`
	MissingTargetQueryCount   int     `json:"missing_target_query_count"`
}

// NewPythonRunner creates a Python subprocess-backed Engine implementation. The
// caller supplies the Python executable and working directory so local uv-managed
// environments, production virtual environments, and packaged deployments can
// all use the same runner without hardcoded machine-specific paths.
//
// NewPythonRunner 创建一个由 Python 子进程驱动的 Engine 实现。
// 调用方需负责提供 Python 解释器的路径和工作目录。得益于这种设计，无论是本地
// 由 uv 管理的环境、生产级的虚拟环境，还是打包部署的容器，都能复用这同一个运行器，
// 彻底告别对机器特定绝对路径的硬编码依赖。
func NewPythonRunner(pythonPath string, workDir string) PythonRunner {
	return PythonRunner{
		PythonPath: pythonPath,
		Module:     "vdb_fingerprint_engine.cli",
		WorkDir:    workDir,
	}
}

// Compare writes a JSON CompareInput payload, invokes the Python engine compare
// command, and reads the resulting JSON CompareOutput payload. It honors context
// cancellation through exec.CommandContext and includes captured stderr/stdout in
// errors so failed engine runs are diagnosable.
//
// Compare 负责写入包含 CompareInput 负载的 JSON，调用 Python 引擎的 compare 命令，
// 并读取返回的 JSON CompareOutput 负载。它通过 exec.CommandContext 严格遵守上下文
// 取消机制，并将子进程的标准错误/标准输出流 (stderr/stdout) 捕获并注入到抛出的错误中，
// 确保失败的引擎调用具备高度的可诊断性。
func (r PythonRunner) Compare(ctx context.Context, input CompareInput) (CompareOutput, error) {
	if r.PythonPath == "" {
		return CompareOutput{}, fmt.Errorf("python path must not be empty")
	}
	if _, err := os.Stat(r.PythonPath); err != nil {
		return CompareOutput{}, fmt.Errorf("stat python path %q: %w", r.PythonPath, err)
	}

	module := r.Module
	if module == "" {
		module = "vdb_fingerprint_engine.cli"
	}

	tempDir, err := os.MkdirTemp("", "vdb-guardian-engine-*")
	if err != nil {
		return CompareOutput{}, fmt.Errorf("create engine temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.json")
	outputPath := filepath.Join(tempDir, "output.json")
	if err := writeCompareInput(inputPath, input); err != nil {
		return CompareOutput{}, err
	}

	cmd := exec.CommandContext(ctx, r.PythonPath, "-m", module, "compare", "--input", inputPath, "--output", outputPath)
	if r.WorkDir != "" {
		cmd.Dir = r.WorkDir
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return CompareOutput{}, fmt.Errorf("run python engine compare canceled: %w", ctxErr)
		}
		return CompareOutput{}, fmt.Errorf("run python engine compare: %w: stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}

	output, err := readCompareOutput(outputPath)
	if err != nil {
		return CompareOutput{}, err
	}
	return output, nil
}

// writeCompareInput serializes the Go CompareInput into the snake_case JSON
// protocol consumed by the Python fingerprint engine CLI.
func writeCompareInput(path string, input CompareInput) error {
	payload := compareInputJSON{
		JobID:                 input.JobID,
		SourceFingerprintPath: input.SourceFingerprintPath,
		TargetFingerprintPath: input.TargetFingerprintPath,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal engine input: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write engine input %q: %w", path, err)
	}
	return nil
}

// readCompareOutput deserializes the Python engine's snake_case JSON response
// into the Go CompareOutput type used by job orchestration and reporting.
func readCompareOutput(path string) (CompareOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CompareOutput{}, fmt.Errorf("read engine output %q: %w", path, err)
	}
	var payload compareOutputJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return CompareOutput{}, fmt.Errorf("decode engine output %q: %w", err)
	}
	return CompareOutput{
		JobID:            payload.JobID,
		ConsistencyScore: payload.ConsistencyScore,
		Metrics: MetricSummary{
			FingerprintDistance:       payload.Metrics.FingerprintDistance,
			StableNeighborDistance:    payload.Metrics.StableNeighborDistance,
			BoundaryCandidateDistance: payload.Metrics.BoundaryCandidateDistance,
			BoundaryFlipRate:          payload.Metrics.BoundaryFlipRate,
			MatchedQueryCount:         payload.Metrics.MatchedQueryCount,
			MissingSourceQueryCount:   payload.Metrics.MissingSourceQueryCount,
			MissingTargetQueryCount:   payload.Metrics.MissingTargetQueryCount,
		},
	}, nil
}
