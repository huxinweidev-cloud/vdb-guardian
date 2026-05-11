package connectors

import "context"

// SearchRequest describes a normalized vector search operation that every
// database connector must understand. It intentionally hides database-specific
// parameter names so Milvus, pgvector, and future connectors can feed comparable
// retrieval results into the fingerprint engine.
//
// SearchRequest 描述了所有数据库连接器都必须理解的规范化向量检索操作。
// 它刻意隐藏了特定于具体数据库的参数名称，从而使得 Milvus、pgvector 以及
// 未来的其他连接器能够向指纹引擎输送格式一致、可供比对的检索结果。
type SearchRequest struct {
	// Collection identifies the source collection, table, index, or namespace.
	Collection string
	// QueryVector contains the query embedding used for nearest-neighbor search.
	QueryVector []float64
	// TopK is the business-visible result count used for ordinary topK comparison.
	TopK int
	// ExpandK is the larger result count used to observe topK boundary candidates.
	ExpandK int
	// Filters contains normalized metadata constraints such as tenant or document type.
	Filters map[string]string
	// Params contains connector-specific search parameters after explicit opt-in.
	Params map[string]string
}

// SearchHit represents one normalized nearest-neighbor result returned by a
// connector. The fingerprint engine consumes this structure to calculate stable
// neighbors, boundary candidates, score curves, and ranking differences.
//
// SearchHit 代表连接器返回的单条规范化的最近邻检索结果。
// 指纹引擎消费该结构体来计算稳定邻居、边界候选者、得分曲线以及排名的差异。
type SearchHit struct {
	// ID is the stable vector or document identifier used to compare source and target results.
	ID string
	// Rank is the one-based rank assigned by the source or target vector database.
	Rank int
	// Score is the normalized similarity score or distance-derived score for comparison.
	Score float64
	// Metadata contains optional normalized fields returned with the vector hit.
	Metadata map[string]string
}

// SearchResponse contains normalized search hits for a single query. It is kept
// deliberately small so connector implementations can stream or batch responses
// later without leaking database-specific SDK objects into core packages.
//
// SearchResponse 包含了针对单次查询的规范化命中结果。
// 该结构体被刻意设计得非常轻量，以便连接器的具体实现可以在未来采用流式 (stream)
// 或批处理 (batch) 方式返回结果，同时坚决防止特定数据库的 SDK 对象泄露到核心包中。
type SearchResponse struct {
	// Hits contains ranked results ordered by ascending rank.
	Hits []SearchHit
}

// Connector defines the enterprise boundary between vdb-guardian and concrete
// vector databases. Implementations must honor context cancellation and return
// normalized SearchResponse values suitable for retrieval behavior comparison.
//
// Connector 定义了 vdb-guardian 与具体底层向量数据库之间的企业级边界。
// 该接口的实现方必须遵守上下文的取消机制 (context cancellation)，
// 并返回适合用于检索行为比对的、标准化的 SearchResponse。
type Connector interface {
	// Name returns a stable connector identifier for logs, configuration, and reports.
	Name() string
	// Connect initializes the connector and validates that the target database is reachable.
	Connect(ctx context.Context) error
	// Count returns the number of records in a collection or table for migration completeness checks.
	Count(ctx context.Context, collection string) (int64, error)
	// Search executes a normalized vector search request and returns comparable ranked hits.
	Search(ctx context.Context, req SearchRequest) (SearchResponse, error)
	// Close releases any network connections, pools, or local resources held by the connector.
	Close() error
}
