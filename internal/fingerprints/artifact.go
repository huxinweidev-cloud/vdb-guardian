package fingerprints

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SearchResult contains normalized search hits for one verification query. It
// is the connector-to-fingerprint boundary used before source and target results
// are transformed into Python-compatible retrieval behavior fingerprint artifacts.
//
// SearchResult 包含了单条验证查询的规范化命中结果。
// 它是连接器与指纹构建器之间的桥梁边界，用于将源端与目标端的原始检索结果，
// 转化为兼容 Python 规范的检索行为指纹产物。
type SearchResult struct {
	// QueryID is the stable identifier used to align source and target query behavior.
	QueryID string
	// Hits are normalized vector identifiers sorted by rank or sortable by rank.
	Hits []SearchHit
}

// SearchHit describes one normalized vector search hit. Connectors should map
// database-specific hit IDs, ranks, and scores into this structure before the
// fingerprint artifact builder derives topK, stable-neighbor, and boundary sets.
//
// SearchHit 描述了一条规范化的向量检索命中记录。
// 连接器应当将特定数据库的命中 ID、排名和得分映射为此结构，然后指纹产物构建器
// 会基于此结构衍生出 TopK 集合、稳定邻居集合以及边界候选者集合。
type SearchHit struct {
	// ID is the stable vector or document identifier returned by the database.
	ID string
	// Rank is the one-based result position where lower values are better.
	Rank int
	// Score is the connector-provided similarity or distance score for future scoring modes.
	Score float64
}

// BuildOptions configures rank-window fingerprint artifact generation. The
// first implementation intentionally uses ranks rather than score deltas so it
// remains portable across databases with different score scales.
//
// BuildOptions 用于配置基于排位窗口 (rank-window) 的指纹产物生成规则。
// 初版的实现刻意使用了排位 (ranks) 而非得分差值 (score deltas)，
// 这样可以确保产物在面对具有不同得分刻度尺的异构数据库时依然保持通用性 (portable)。
type BuildOptions struct {
	// TopK controls how many leading hits become visible topK identifiers.
	TopK int
	// StableK controls how many leading hits become stable-neighbor identifiers.
	StableK int
	// BoundaryK controls how many hits around the topK cutoff become boundary candidates.
	BoundaryK int
}

// Artifact is the JSON file shape consumed by the Python fingerprint engine.
// It contains query-level retrieval behavior fingerprints collected from one
// vector database.
//
// Artifact 是供 Python 指纹引擎消费的 JSON 文件结构。
// 它包含了从单一向量数据库中收集到的、在查询级别的检索行为指纹。
type Artifact struct {
	// Fingerprints contains one retrieval behavior fingerprint per query.
	Fingerprints []QueryFingerprint `json:"fingerprints"`
}

// QueryFingerprint captures the retrieval behavior for one query in a format
// shared between Go-generated connector artifacts and the Python comparison
// engine.
//
// QueryFingerprint 捕获了针对单次查询的检索行为画像，
// 该格式是 Go 语言生成的连接器产物与 Python 比对引擎之间共享的契约。
type QueryFingerprint struct {
	// QueryID is the stable query identifier used for source/target alignment.
	QueryID string `json:"query_id"`
	// StableNeighbors are the high-confidence leading hits for the query.
	StableNeighbors []string `json:"stable_neighbors"`
	// BoundaryCandidates are hits near the topK cutoff and sensitive to migration drift.
	BoundaryCandidates []string `json:"boundary_candidates"`
	// TopKIDs are the visible topK hit identifiers used to calculate boundary flips.
	TopKIDs []string `json:"top_k_ids"`
}

// BuildArtifact converts normalized search results into a Python-compatible
// fingerprint artifact. It validates query uniqueness, required identifiers, and
// option bounds before deriving stable-neighbor, boundary-candidate, and topK ID
// lists from rank-ordered hits.
//
// BuildArtifact 将规范化的搜索结果转换为兼容 Python 的指纹产物。
// 在从排序后的命中结果中推导出稳定邻居、边界候选者以及 TopK ID 列表之前，
// 它会对查询 ID 的唯一性、必需的标识符以及配置选项的边界合法性进行强校验。
func BuildArtifact(results []SearchResult, options BuildOptions) (Artifact, error) {
	if err := validateBuildOptions(options); err != nil {
		return Artifact{}, err
	}
	seenQueryIDs := make(map[string]struct{}, len(results))
	fingerprints := make([]QueryFingerprint, 0, len(results))
	for _, result := range results {
		fingerprint, err := buildQueryFingerprint(result, options, seenQueryIDs)
		if err != nil {
			return Artifact{}, err
		}
		fingerprints = append(fingerprints, fingerprint)
	}
	return Artifact{Fingerprints: fingerprints}, nil
}

// WriteArtifact writes a fingerprint artifact JSON file with restrictive file
// permissions. The output uses snake_case field names so the Python engine can
// consume it directly through the documented artifact protocol.
//
// WriteArtifact 以极其严格的文件权限将指纹产物写入 JSON 文件中。
// 输出的内容强制使用蛇形命名法 (snake_case) 作为字段名，以便 Python 引擎
// 能够通过既定的产物协议规范直接将其消费入库。
func WriteArtifact(path string, artifact Artifact) error {
	if path == "" {
		return errors.New("artifact path must not be empty")
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal fingerprint artifact: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create fingerprint artifact dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write fingerprint artifact %q: %w", path, err)
	}
	return nil
}

func validateBuildOptions(options BuildOptions) error {
	if options.TopK <= 0 {
		return errors.New("top_k must be greater than zero")
	}
	if options.StableK <= 0 {
		return errors.New("stable_k must be greater than zero")
	}
	if options.StableK > options.TopK {
		return errors.New("stable_k must be less than or equal to top_k")
	}
	if options.BoundaryK <= 0 {
		return errors.New("boundary_k must be greater than zero")
	}
	return nil
}

func buildQueryFingerprint(
	result SearchResult,
	options BuildOptions,
	seenQueryIDs map[string]struct{},
) (QueryFingerprint, error) {
	if result.QueryID == "" {
		return QueryFingerprint{}, errors.New("query_id must not be empty")
	}
	if _, ok := seenQueryIDs[result.QueryID]; ok {
		return QueryFingerprint{}, fmt.Errorf("duplicate query_id %q", result.QueryID)
	}
	seenQueryIDs[result.QueryID] = struct{}{}
	if len(result.Hits) < options.TopK {
		return QueryFingerprint{}, fmt.Errorf("query_id %q must contain at least top_k hits", result.QueryID)
	}

	hits := append([]SearchHit(nil), result.Hits...)
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Rank < hits[j].Rank
	})
	for _, hit := range hits {
		if hit.ID == "" {
			return QueryFingerprint{}, fmt.Errorf("query_id %q contains hit with empty id", result.QueryID)
		}
		if hit.Rank <= 0 {
			return QueryFingerprint{}, fmt.Errorf("query_id %q contains hit with non-positive rank", result.QueryID)
		}
	}

	return QueryFingerprint{
		QueryID:            result.QueryID,
		StableNeighbors:    hitIDs(hits[:options.StableK]),
		BoundaryCandidates: boundaryCandidateIDs(hits, options),
		TopKIDs:            hitIDs(hits[:options.TopK]),
	}, nil
}

func boundaryCandidateIDs(hits []SearchHit, options BuildOptions) []string {
	start := options.TopK - options.BoundaryK
	if start < 0 {
		start = 0
	}
	end := options.TopK + options.BoundaryK
	if end > len(hits) {
		end = len(hits)
	}
	return hitIDs(hits[start:end])
}

func hitIDs(hits []SearchHit) []string {
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.ID)
	}
	return ids
}
