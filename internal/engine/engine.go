package engine

import "context"

// CompareInput is the stable Go-side request passed to a fingerprint engine. In
// the first enterprise scaffold this type only carries artifact locations; later
// implementations can use the same boundary for Python subprocess, gRPC, or a
// native Go engine.
//
// CompareInput 是传递给指纹引擎的、稳定的 Go 语言端请求结构。
// 在首个企业级脚手架中，该类型仅携带产物文件的路径；在未来的实现中，
// 可以复用同一个边界来支持 Python 子进程、gRPC 接口甚至纯 Go 原生引擎。
type CompareInput struct {
	// JobID links the comparison request to a durable verification job.
	JobID string
	// SourceFingerprintPath points to the artifact containing source retrieval behavior fingerprints.
	SourceFingerprintPath string
	// TargetFingerprintPath points to the artifact containing target retrieval behavior fingerprints.
	TargetFingerprintPath string
}

// MetricSummary contains the primary retrieval behavior consistency metrics that
// reports, APIs, and future CI gates can consume without understanding engine
// internals.
//
// MetricSummary 包含了主要的检索行为一致性指标。
// 报告生成器、API 接口以及未来的 CI 门禁系统可以直接消费这些指标，
// 而无需了解引擎内部的任何复杂计算逻辑。
type MetricSummary struct {
	// FingerprintDistance is the normalized overall distance between source and target fingerprints.
	FingerprintDistance float64
	// StableNeighborDistance is the average Jaccard distance between stable-neighbor sets.
	StableNeighborDistance float64
	// BoundaryCandidateDistance is the average Jaccard distance between boundary-candidate sets.
	BoundaryCandidateDistance float64
	// BoundaryFlipRate measures how often topK boundary candidates enter or leave visible results.
	BoundaryFlipRate float64
	// MatchedQueryCount is the number of query IDs found in both source and target artifacts.
	MatchedQueryCount int
	// MissingSourceQueryCount is the number of target query IDs missing from the source artifact.
	MissingSourceQueryCount int
	// MissingTargetQueryCount is the number of source query IDs missing from the target artifact.
	MissingTargetQueryCount int
}

// CompareOutput is the normalized response returned by any fingerprint engine
// implementation. Keeping the response small gives the Go control plane a stable
// contract while detailed artifacts remain in the artifact store.
//
// CompareOutput 是任何指纹引擎实现都必须返回的规范化响应。
// 刻意保持响应负载的轻量化，可以为 Go 控制平面提供一个极为稳定的契约，
// 而更为详尽的比对产物则会留在文件产物存储 (artifact store) 中。
type CompareOutput struct {
	// JobID identifies the verification job associated with this comparison result.
	JobID string
	// ConsistencyScore is a normalized score in [0, 1], where higher means more consistent.
	ConsistencyScore float64
	// Metrics contains the main decomposed fingerprint comparison values.
	Metrics MetricSummary
}

// Engine defines the boundary between Go orchestration and retrieval behavior
// fingerprint algorithms. Implementations must honor context cancellation and
// return deterministic output for a given set of source and target artifacts.
//
// Engine 定义了 Go 编排平面与底层检索行为指纹算法之间的边界。
// 引擎的具体实现方必须遵守上下文的取消机制 (context cancellation)，
// 并且对于给定的一组源端和目标端产物，必须返回绝对确定性的输出结果。
type Engine interface {
	// Compare compares source and target retrieval behavior fingerprints and returns consistency metrics.
	Compare(ctx context.Context, input CompareInput) (CompareOutput, error)
}
