package connectors

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// MemoryConnector is a deterministic in-memory connector for local verification
// and tests. It implements the Connector contract without contacting a real
// vector database, which lets the control plane exercise search normalization,
// fingerprint artifact building, and verification orchestration before Milvus or
// pgvector connectors are available.
//
// MemoryConnector 是一种专为本地验证和测试设计的、具有绝对确定性的内存型连接器。
// 它在不连接任何真实向量数据库的情况下实现了 Connector 契约。这使得控制平面能够在
// 真实的 Milvus 或 pgvector 连接器就绪之前，就提前将检索规范化、指纹产物构建以及
// 验证编排等核心逻辑完全跑通。
type MemoryConnector struct {
	// name is the stable connector identifier used in logs, tests, and reports.
	name string
	// results maps a query identifier to precomputed ranked hits.
	results map[string][]SearchHit
}

// NewMemoryConnector creates a connector backed by precomputed query results.
// The input map and slices are deep-copied so later caller mutations cannot
// change connector behavior during deterministic local verification tests.
//
// NewMemoryConnector 创建一个由预计算检索结果驱动的连接器。
// 输入的 Map 和切片会被执行深拷贝 (deep-copied)，这样一来，即使调用方后续修改了原始数据，
// 也绝不会干扰确定性本地验证测试期间连接器的行为。
func NewMemoryConnector(name string, results map[string][]SearchHit) MemoryConnector {
	if name == "" {
		name = "memory"
	}
	copied := make(map[string][]SearchHit, len(results))
	for queryID, hits := range results {
		copied[queryID] = cloneAndSortHits(hits)
	}
	return MemoryConnector{name: name, results: copied}
}

// Name returns the stable connector identifier configured at construction time.
//
// Name 返回在构建时配置的稳定的连接器标识符。
func (c MemoryConnector) Name() string {
	return c.name
}

// Connect validates the in-memory connector context. It performs no network I/O
// because all search results are already loaded in memory.
//
// Connect 校验内存连接器的上下文。它不会执行任何网络 I/O，
// 因为所有的检索结果都已预先加载到了内存中。
func (c MemoryConnector) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Count returns the number of precomputed hits for the provided query key. In
// the memory connector, the collection argument represents the query identifier
// so tests can use the same Connector interface without database-specific state.
//
// Count 返回针对指定查询键 (query key) 的预计算命中结果数量。
// 在内存连接器中，collection 参数实际上代表的是查询标识符 (query identifier)，
// 这样测试代码就可以在不引入特定数据库状态的前提下，复用完全相同的 Connector 接口契约。
func (c MemoryConnector) Count(ctx context.Context, collection string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	hits, ok := c.results[collection]
	if !ok {
		return 0, fmt.Errorf("memory connector query %q not found", collection)
	}
	return int64(len(hits)), nil
}

// Search returns deterministic ranked hits for the request collection key. The
// collection field acts as a query identifier until real connectors provide
// query-vector based search; returned slices are copied so callers cannot mutate
// connector state.
//
// Search 返回针对请求集合键的确定性排名命中结果。
// 在真实的连接器提供基于查询向量 (query-vector) 的搜索之前，collection 字段在此处
// 充当了查询标识符的角色。返回的切片均为深拷贝，以防调用方篡改连接器的内部状态。
func (c MemoryConnector) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if err := ctx.Err(); err != nil {
		return SearchResponse{}, err
	}
	if req.TopK <= 0 {
		return SearchResponse{}, errors.New("top_k must be greater than zero")
	}
	if req.ExpandK <= 0 {
		return SearchResponse{}, errors.New("expand_k must be greater than zero")
	}
	if req.ExpandK < req.TopK {
		return SearchResponse{}, errors.New("expand_k must be greater than or equal to top_k")
	}
	if req.Collection == "" {
		return SearchResponse{}, errors.New("memory connector collection query key must not be empty")
	}

	hits, ok := c.results[req.Collection]
	if !ok {
		return SearchResponse{}, fmt.Errorf("memory connector query %q not found", req.Collection)
	}
	if len(hits) < req.ExpandK {
		return SearchResponse{}, fmt.Errorf("memory connector query %q must contain at least expand_k hits", req.Collection)
	}
	return SearchResponse{Hits: cloneHits(hits[:req.ExpandK])}, nil
}

// Close releases memory connector resources. It is currently a no-op because no
// external handles are acquired, but it preserves the Connector lifecycle shape.
//
// Close 释放内存连接器的资源。由于没有获取任何外部句柄，它目前是一个无操作 (no-op)，
// 但它依然严格保持了 Connector 接口生命周期的完整性。
func (c MemoryConnector) Close() error {
	return nil
}

func cloneAndSortHits(hits []SearchHit) []SearchHit {
	copied := cloneHits(hits)
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Rank < copied[j].Rank
	})
	return copied
}

func cloneHits(hits []SearchHit) []SearchHit {
	copied := make([]SearchHit, len(hits))
	for i, hit := range hits {
		copied[i] = hit
		if hit.Metadata != nil {
			copied[i].Metadata = make(map[string]string, len(hit.Metadata))
			for key, value := range hit.Metadata {
				copied[i].Metadata[key] = value
			}
		}
	}
	return copied
}
