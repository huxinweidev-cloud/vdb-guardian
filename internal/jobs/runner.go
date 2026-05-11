package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/engine"
)

// VerificationRunner executes a local artifact-backed verification job. It is
// the first orchestration layer above the fingerprint engine: callers provide
// source and target fingerprint artifact paths, the runner invokes the configured
// engine, and the runner persists a structured result artifact for downstream
// CLI, API, and report generation.
//
// VerificationRunner 执行基于本地产物的验证作业。
// 这是位于指纹引擎之上的首层业务编排逻辑：调用方提供源端与目标端指纹产物的路径，
// 运行器负责调用已配置的引擎，并在执行完毕后将结构化的结果产物持久化，
// 供下游的 CLI、API 以及报告生成模块使用。
type VerificationRunner struct {
	// Engine compares source and target retrieval behavior fingerprint artifacts.
	Engine engine.Engine
	// ArtifactDir is the local directory where job result artifacts are written.
	ArtifactDir string
}

// VerificationRequest contains the minimum local inputs required to compare two
// retrieval behavior fingerprint artifacts. Future connector-backed runners can
// build this request after collecting Milvus and pgvector query results.
//
// VerificationRequest 包含了比对两份检索行为指纹产物所需的最小化本地输入参数。
// 未来由连接器驱动的运行器，可以在收集完 Milvus 和 pgvector 的查询结果并构建产物后，
// 再拼装出这个请求。
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
//
// VerificationResult 描述了一次已完成的本地验证执行。
// 它包含了最终的生命周期状态、规范化的引擎输出，以及由运行器写入的 JSON 结果产物的路径。
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
//
// NewVerificationRunner 创建一个本地验证运行器，该运行器将结果产物写入 artifactDir 目录。
// 通过依赖注入传入引擎，使得测试环境可以使用模拟引擎 (fakes)，而生产环境代码
// 则可以使用 Python 子进程引擎或是未来的远程引擎。
func NewVerificationRunner(engine engine.Engine, artifactDir string) VerificationRunner {
	return VerificationRunner{Engine: engine, ArtifactDir: artifactDir}
}

// Run validates the request, invokes the fingerprint engine, persists a
// result.json artifact, and returns a structured VerificationResult. It returns
// errors before writing a success artifact when validation or engine execution
// fails so callers do not mistake partial runs for completed verification jobs.
//
// Run 方法校验请求，调用指纹比对引擎，持久化 result.json 产物，并返回结构化的 VerificationResult。
// 如果在参数校验或引擎执行阶段发生故障，它会提前返回错误而绝不会写入标记为成功的产物，
// 以此确保调用方不会将这种“半残”的执行误认为是一次已完成的验证作业。
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
//
// writeVerificationResult 将运行器的输出序列化为持久化的 JSON 产物。
// 该产物强制使用蛇形命名法 (snake_case) 作为字段名，因为它不仅为 Go 消费而设计，
// 更主要服务于 CLI、API 以及 Python 周边的数据生态工具链。
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
