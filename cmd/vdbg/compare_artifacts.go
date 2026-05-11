package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/h3xwave/vdb-guardian/internal/engine"
	"github.com/h3xwave/vdb-guardian/internal/jobs"
)

type compareArtifactsOptions struct {
	SourceFingerprintPath string
	TargetFingerprintPath string
	ArtifactDir           string
	JobID                 string
}

// runCompareArtifactsCommand parses CLI flags, creates a Python-backed engine,
// and compares existing source and target fingerprint artifacts.
//
// It is the first real database artifact comparison command: callers can generate
// source artifacts from Milvus and target artifacts from pgvector, then use this
// command to persist a normalized consistency result artifact.
//
// runCompareArtifactsCommand 解析 CLI 参数，创建一个由 Python 驱动的比对引擎，
// 并对比本地现有的源端和目标端指纹产物。
//
// 它是第一个真正串联起真实数据库的产物比对命令：调用方可以先从 Milvus 生成源端产物，
// 从 pgvector 生成目标端产物，然后使用本命令来生成并持久化一份规范化的一致性比对报告。
func runCompareArtifactsCommand(ctx context.Context, args []string) error {
	options, err := parseCompareArtifactsOptions(args)
	if err != nil {
		return err
	}
	pythonPath, pythonWorkDir, err := discoverPythonEngine()
	if err != nil {
		return err
	}
	result, err := runCompareArtifacts(ctx, options, engine.NewPythonRunner(pythonPath, pythonWorkDir))
	if err != nil {
		return err
	}
	fmt.Printf("artifact comparison completed\n")
	fmt.Printf("job_id: %s\n", result.JobID)
	fmt.Printf("consistency_score: %.6f\n", result.Output.ConsistencyScore)
	fmt.Printf("fingerprint_distance: %.6f\n", result.Output.Metrics.FingerprintDistance)
	fmt.Printf("stable_neighbor_distance: %.6f\n", result.Output.Metrics.StableNeighborDistance)
	fmt.Printf("boundary_candidate_distance: %.6f\n", result.Output.Metrics.BoundaryCandidateDistance)
	fmt.Printf("boundary_flip_rate: %.6f\n", result.Output.Metrics.BoundaryFlipRate)
	fmt.Printf("matched_queries: %d\n", result.Output.Metrics.MatchedQueryCount)
	fmt.Printf("missing_source_queries: %d\n", result.Output.Metrics.MissingSourceQueryCount)
	fmt.Printf("missing_target_queries: %d\n", result.Output.Metrics.MissingTargetQueryCount)
	fmt.Printf("source_fingerprint: %s\n", options.SourceFingerprintPath)
	fmt.Printf("target_fingerprint: %s\n", options.TargetFingerprintPath)
	fmt.Printf("result: %s\n", result.ResultPath)
	return nil
}

func parseCompareArtifactsOptions(args []string) (compareArtifactsOptions, error) {
	flagSet := flag.NewFlagSet("compare-artifacts", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var options compareArtifactsOptions
	flagSet.StringVar(&options.SourceFingerprintPath, "source", "", "path to source fingerprint artifact JSON")
	flagSet.StringVar(&options.TargetFingerprintPath, "target", "", "path to target fingerprint artifact JSON")
	flagSet.StringVar(&options.ArtifactDir, "artifact-dir", "", "directory to write the comparison result artifact")
	flagSet.StringVar(&options.JobID, "job-id", "artifact-compare", "job id used for the result artifact filename")
	if err := flagSet.Parse(args); err != nil {
		return compareArtifactsOptions{}, err
	}
	if options.SourceFingerprintPath == "" {
		return compareArtifactsOptions{}, errors.New("source fingerprint path is required")
	}
	if options.TargetFingerprintPath == "" {
		return compareArtifactsOptions{}, errors.New("target fingerprint path is required")
	}
	if options.ArtifactDir == "" {
		return compareArtifactsOptions{}, errors.New("artifact-dir is required")
	}
	if options.JobID == "" {
		return compareArtifactsOptions{}, errors.New("job-id is required")
	}
	return options, nil
}

// runCompareArtifacts validates artifact paths, invokes the verification runner,
// and returns the persisted comparison result. The engine is injected so tests can
// avoid spawning Python while production commands use the PythonRunner.
//
// runCompareArtifacts 校验产物路径，调用验证运行器 (verification runner)，
// 并返回持久化之后的比对结果。引擎参数通过依赖注入的方式传入，这样测试代码就可以
// 避开繁重的拉起 Python 进程的开销，而生产环境命令则可以如期挂载 PythonRunner。
func runCompareArtifacts(ctx context.Context, options compareArtifactsOptions, compareEngine engine.Engine) (jobs.VerificationResult, error) {
	if options.SourceFingerprintPath == "" {
		return jobs.VerificationResult{}, errors.New("source fingerprint path is required")
	}
	if options.TargetFingerprintPath == "" {
		return jobs.VerificationResult{}, errors.New("target fingerprint path is required")
	}
	if options.ArtifactDir == "" {
		return jobs.VerificationResult{}, errors.New("artifact-dir is required")
	}
	if options.JobID == "" {
		return jobs.VerificationResult{}, errors.New("job-id is required")
	}
	if compareEngine == nil {
		return jobs.VerificationResult{}, errors.New("compare engine must not be nil")
	}
	if err := requireReadableFile("source fingerprint", options.SourceFingerprintPath); err != nil {
		return jobs.VerificationResult{}, err
	}
	if err := requireReadableFile("target fingerprint", options.TargetFingerprintPath); err != nil {
		return jobs.VerificationResult{}, err
	}
	runner := jobs.NewVerificationRunner(compareEngine, options.ArtifactDir)
	return runner.Run(ctx, jobs.VerificationRequest{
		JobID:                 options.JobID,
		SourceFingerprintPath: options.SourceFingerprintPath,
		TargetFingerprintPath: options.TargetFingerprintPath,
	})
}

func requireReadableFile(label string, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s %q: %w", label, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s %q must be a file", label, path)
	}
	return nil
}
