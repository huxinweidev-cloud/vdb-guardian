package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/h3xwave/vdb-guardian/internal/engine"
	"github.com/h3xwave/vdb-guardian/internal/fingerprints"
	"github.com/h3xwave/vdb-guardian/internal/jobs"
	"github.com/h3xwave/vdb-guardian/internal/pipeline"
)

type offlineVerifyOptions struct {
	FixturePath string
	ArtifactDir string
}

type offlineFixture struct {
	JobID     string                `json:"job_id"`
	TopK      int                   `json:"top_k"`
	ExpandK   int                   `json:"expand_k"`
	StableK   int                   `json:"stable_k"`
	BoundaryK int                   `json:"boundary_k"`
	Queries   []offlineQueryFixture `json:"queries"`
}

type offlineQueryFixture struct {
	QueryID    string              `json:"query_id"`
	SourceHits []offlineHitFixture `json:"source_hits"`
	TargetHits []offlineHitFixture `json:"target_hits"`
}

type offlineHitFixture struct {
	ID    string  `json:"id"`
	Rank  int     `json:"rank"`
	Score float64 `json:"score"`
}

type offlinePipelineInputs struct {
	SourceConnector connectors.Connector
	TargetConnector connectors.Connector
	BuildOptions    fingerprints.BuildOptions
	Request         pipeline.OfflineRequest
}

// runOfflineVerifyCommand parses CLI flags, creates a Python-backed engine, and
// runs the fixture-backed offline verification workflow. It keeps argument
// parsing separate from orchestration so tests can inject fake engines into
// runOfflineVerify without spawning Python.
//
// runOfflineVerifyCommand 解析 CLI 参数，创建一个由 Python 驱动的引擎，
// 并执行基于固件的离线验证工作流。它刻意将“参数解析”与“业务编排”分离开来，
// 这样在测试中就可以直接向 runOfflineVerify 注入模拟引擎 (fake engines)，
// 从而避免真正拉起 Python 子进程的开销。
func runOfflineVerifyCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("offline-verify", flag.ContinueOnError)
	fixturePath := flags.String("fixture", "", "path to offline verification fixture JSON")
	artifactDir := flags.String("artifact-dir", "", "directory for generated fingerprint and result artifacts")
	if err := flags.Parse(args); err != nil {
		return err
	}
	pythonPath, pythonWorkDir, err := discoverPythonEngine()
	if err != nil {
		return err
	}
	result, err := runOfflineVerify(ctx, offlineVerifyOptions{FixturePath: *fixturePath, ArtifactDir: *artifactDir}, engine.NewPythonRunner(pythonPath, pythonWorkDir))
	if err != nil {
		return err
	}
	fmt.Printf("offline verification completed\n")
	fmt.Printf("job_id: %s\n", result.JobID)
	fmt.Printf("consistency_score: %.6f\n", result.VerificationResult.Output.ConsistencyScore)
	fmt.Printf("source_fingerprint: %s\n", result.SourceFingerprintPath)
	fmt.Printf("target_fingerprint: %s\n", result.TargetFingerprintPath)
	fmt.Printf("result: %s\n", result.VerificationResult.ResultPath)
	return nil
}

// runOfflineVerify loads a fixture, builds memory connectors from it, runs the
// local offline pipeline, and returns generated artifact paths plus engine
// metrics. The engine parameter enables deterministic unit tests and production
// execution with PythonRunner.
//
// runOfflineVerify 加载测试固件，据此构建出内存连接器，执行本地离线流水线，
// 最后返回生成的产物路径以及引擎测算的各项指标。传入的 engine 引擎参数使得
// 该函数既能支持确定性的单元测试，又能在生产执行时顺畅挂载 PythonRunner。
func runOfflineVerify(ctx context.Context, options offlineVerifyOptions, compareEngine engine.Engine) (pipeline.OfflineResult, error) {
	if options.FixturePath == "" {
		return pipeline.OfflineResult{}, errors.New("offline verify fixture path must not be empty")
	}
	if options.ArtifactDir == "" {
		return pipeline.OfflineResult{}, errors.New("offline verify artifact dir must not be empty")
	}
	if compareEngine == nil {
		return pipeline.OfflineResult{}, errors.New("offline verify engine must not be nil")
	}
	fixture, err := loadOfflineFixture(options.FixturePath)
	if err != nil {
		return pipeline.OfflineResult{}, err
	}
	inputs, err := buildOfflinePipelineInputs(fixture)
	if err != nil {
		return pipeline.OfflineResult{}, err
	}
	runner := jobs.NewVerificationRunner(compareEngine, options.ArtifactDir)
	offlinePipeline := pipeline.NewOfflinePipeline(
		inputs.SourceConnector,
		inputs.TargetConnector,
		runner,
		options.ArtifactDir,
		inputs.BuildOptions,
	)
	return offlinePipeline.Run(ctx, inputs.Request)
}

// loadOfflineFixture decodes and validates the JSON fixture used by the
// offline-verify command. The fixture is intentionally small and deterministic so
// local functional tests do not depend on Docker, SDKs, or live databases.
//
// loadOfflineFixture 解码并验证供 offline-verify 命令使用的 JSON 固件。
// 该固件被刻意设计得非常小巧且具备绝对的确定性，这使得本地的功能测试能够
// 彻底摆脱对 Docker、数据库 SDK 或真实运行中数据库的依赖。
func loadOfflineFixture(path string) (offlineFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return offlineFixture{}, fmt.Errorf("read offline fixture %q: %w", path, err)
	}
	var fixture offlineFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return offlineFixture{}, fmt.Errorf("decode offline fixture %q: %w", path, err)
	}
	if err := fixture.validate(); err != nil {
		return offlineFixture{}, err
	}
	return fixture, nil
}

func (f offlineFixture) validate() error {
	if f.JobID == "" {
		return errors.New("offline fixture job_id must not be empty")
	}
	if f.TopK <= 0 {
		return errors.New("offline fixture top_k must be greater than zero")
	}
	if f.ExpandK < f.TopK {
		return errors.New("offline fixture expand_k must be greater than or equal to top_k")
	}
	if f.StableK <= 0 || f.StableK > f.TopK {
		return errors.New("offline fixture stable_k must be greater than zero and less than or equal to top_k")
	}
	if f.BoundaryK <= 0 {
		return errors.New("offline fixture boundary_k must be greater than zero")
	}
	if len(f.Queries) == 0 {
		return errors.New("offline fixture queries must not be empty")
	}
	seen := make(map[string]struct{}, len(f.Queries))
	for _, query := range f.Queries {
		if query.QueryID == "" {
			return errors.New("offline fixture query_id must not be empty")
		}
		if _, ok := seen[query.QueryID]; ok {
			return fmt.Errorf("offline fixture duplicate query_id %q", query.QueryID)
		}
		seen[query.QueryID] = struct{}{}
		if len(query.SourceHits) < f.ExpandK || len(query.TargetHits) < f.ExpandK {
			return fmt.Errorf("offline fixture query_id %q must contain at least expand_k source and target hits", query.QueryID)
		}
	}
	return nil
}

// buildOfflinePipelineInputs converts a fixture into memory connectors, build
// options, and the request consumed by the internal offline pipeline.
func buildOfflinePipelineInputs(fixture offlineFixture) (offlinePipelineInputs, error) {
	if err := fixture.validate(); err != nil {
		return offlinePipelineInputs{}, err
	}
	sourceResults := make(map[string][]connectors.SearchHit, len(fixture.Queries))
	targetResults := make(map[string][]connectors.SearchHit, len(fixture.Queries))
	queryIDs := make([]string, 0, len(fixture.Queries))
	for _, query := range fixture.Queries {
		queryIDs = append(queryIDs, query.QueryID)
		sourceResults[query.QueryID] = toConnectorHits(query.SourceHits)
		targetResults[query.QueryID] = toConnectorHits(query.TargetHits)
	}
	return offlinePipelineInputs{
		SourceConnector: connectors.NewMemoryConnector("fixture-source", sourceResults),
		TargetConnector: connectors.NewMemoryConnector("fixture-target", targetResults),
		BuildOptions: fingerprints.BuildOptions{
			TopK:      fixture.TopK,
			StableK:   fixture.StableK,
			BoundaryK: fixture.BoundaryK,
		},
		Request: pipeline.OfflineRequest{
			JobID:    fixture.JobID,
			QueryIDs: queryIDs,
			TopK:     fixture.TopK,
			ExpandK:  fixture.ExpandK,
		},
	}, nil
}

func toConnectorHits(hits []offlineHitFixture) []connectors.SearchHit {
	converted := make([]connectors.SearchHit, 0, len(hits))
	for _, hit := range hits {
		converted = append(converted, connectors.SearchHit{ID: hit.ID, Rank: hit.Rank, Score: hit.Score})
	}
	return converted
}

// discoverPythonEngine returns a Python executable and working directory for the
// local fingerprint engine. It prefers the repository uv virtual environment and
// falls back to system Python interpreters for simple developer checkouts.
func discoverPythonEngine() (string, string, error) {
	pythonWorkDir := "python"
	candidates := []string{
		filepath.Join(pythonWorkDir, ".venv", "bin", "python"),
		"python3",
		"python",
	}
	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) || filepath.Dir(candidate) != "." {
			if _, err := os.Stat(candidate); err == nil {
				absCandidate, err := filepath.Abs(candidate)
				if err != nil {
					return "", "", fmt.Errorf("resolve python executable %q: %w", candidate, err)
				}
				return absCandidate, pythonWorkDir, nil
			}
			continue
		}
		resolved, err := exec.LookPath(candidate)
		if err == nil {
			return resolved, pythonWorkDir, nil
		}
	}
	return "", "", errors.New("no usable Python executable found for offline verify")
}
