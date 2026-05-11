package fixtures

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
)

const (
	// MetricCosine identifies cosine similarity fixtures. Generated vectors are
	// L2-normalized so Milvus and pgvector can use comparable cosine behavior.
	//
	// MetricCosine 标识使用余弦相似度的固件。生成的向量将被自动进行 L2 归一化，
	// 以便 Milvus 和 pgvector 能够在相同基准下比对各自的余弦距离行为。
	MetricCosine = "cosine"

	// MetricL2 identifies Euclidean distance fixtures. Generated vectors are left
	// unnormalized to exercise distance-based source and target connectors.
	//
	// MetricL2 标识使用欧几里得距离的固件。生成的向量会保持未归一化状态，
	// 专门用于在源端和目标端连接器上验证基于距离的运算行为。
	MetricL2 = "l2"

	maxVectorDimension = 2000
)

// SyntheticOptions describes the deterministic synthetic dataset to generate.
//
// The options intentionally use the pgvector `vector` type limit as the first
// migration MVP boundary because Milvus to pgvector verification must fit the
// target database. The seed makes generated records and queries reproducible so
// integration runs can be compared across machines and commits.
//
// SyntheticOptions 描述了待生成的确定性合成数据集。
// 该选项刻意使用了 pgvector `vector` 类型的存储上限作为首个迁移 MVP 的边界，
// 因为从 Milvus 向 pgvector 的验证，必须确保数据能够被目标库所容纳。
// Seed 参数的引入保证了生成的记录和查询是完全可复现的，
// 这使得在不同机器和不同代码提交 (commits) 之间的集成测试结果具备横向可比性。
type SyntheticOptions struct {
	Seed        int64  `json:"seed"`
	Dimension   int    `json:"dimension"`
	RecordCount int    `json:"record_count"`
	QueryCount  int    `json:"query_count"`
	Metric      string `json:"metric"`
}

// SyntheticDataset is the JSON fixture consumed by future database seeders and
// migration verification commands.
//
// Records are intended to be inserted into the source vector database, while
// Queries are used to collect comparable search results from source and target
// databases before fingerprint artifacts are built.
//
// SyntheticDataset 是一个 JSON 格式的固件，供未来的数据库灌入器 (seeders)
// 及迁移验证命令消费。
// Records (记录) 被设计为只插入源端向量数据库；而 Queries (查询) 则用于在源端
// 和目标端数据库中执行检索，以便在构建指纹产物前收集可供比对的搜索结果。
type SyntheticDataset struct {
	Seed        int64             `json:"seed"`
	Dimension   int               `json:"dimension"`
	RecordCount int               `json:"record_count"`
	QueryCount  int               `json:"query_count"`
	Metric      string            `json:"metric"`
	Records     []SyntheticVector `json:"records"`
	Queries     []SyntheticVector `json:"queries"`
}

// SyntheticVector stores a stable vector identifier and its dense vector values.
//
// IDs are generated with deterministic prefixes so later Milvus and pgvector
// seeders can map records and queries without relying on database-generated IDs.
//
// SyntheticVector 存储了一个稳定的向量标识符及其对应的稠密向量数值。
// 这些 ID 采用确定性的前缀生成，从而使后续的 Milvus 和 pgvector 灌入器在映射记录
// 和查询时，可以完全摆脱对数据库自增/自生成 ID 的依赖。
type SyntheticVector struct {
	ID     string    `json:"id"`
	Vector []float64 `json:"vector"`
}

// GenerateSyntheticDataset creates deterministic record and query vectors for
// local Milvus to pgvector migration experiments.
//
// It validates the requested dimension, record count, query count, and metric.
// Cosine fixtures are normalized to unit length so source and target databases
// receive comparable vectors even if their score conventions differ.
//
// GenerateSyntheticDataset 负责生成确定性的记录与查询向量，专供本地的
// Milvus 到 pgvector 的迁移实验使用。
// 它会对请求的维度、记录数、查询数以及测算指标进行合法性校验。对于余弦 (Cosine) 固件，
// 数据将被归一化为单位长度，以确保即使源端与目标端在底层得分换算约定上存在差异，
// 也能接收到具备等价可比性的向量输入。
func GenerateSyntheticDataset(options SyntheticOptions) (SyntheticDataset, error) {
	if err := validateSyntheticOptions(options); err != nil {
		return SyntheticDataset{}, err
	}

	rng := rand.New(rand.NewSource(options.Seed))
	records := make([]SyntheticVector, options.RecordCount)
	for index := range records {
		records[index] = SyntheticVector{
			ID:     fmt.Sprintf("vec-%06d", index+1),
			Vector: generateVector(rng, options.Dimension, options.Metric),
		}
	}

	queries := make([]SyntheticVector, options.QueryCount)
	for index := range queries {
		queries[index] = SyntheticVector{
			ID:     fmt.Sprintf("query-%06d", index+1),
			Vector: generateVector(rng, options.Dimension, options.Metric),
		}
	}

	return SyntheticDataset{
		Seed:        options.Seed,
		Dimension:   options.Dimension,
		RecordCount: options.RecordCount,
		QueryCount:  options.QueryCount,
		Metric:      options.Metric,
		Records:     records,
		Queries:     queries,
	}, nil
}

// WriteSyntheticDataset writes a synthetic dataset as indented JSON.
//
// The parent directory is created automatically so CLI commands can write
// fixtures into `testdata`, `/tmp`, or later artifact directories using the same
// helper.
//
// WriteSyntheticDataset 将合成的数据集格式化为带缩进的 JSON 并写入文件。
// 父级目录将被自动创建，因此 CLI 命令可以直接使用该工具方法，将测试固件安全地
// 写入 `testdata`、`/tmp` 或者未来引入的特定产物目录中。
func WriteSyntheticDataset(path string, dataset SyntheticDataset) error {
	if path == "" {
		return errors.New("synthetic dataset path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create synthetic dataset directory: %w", err)
	}
	content, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		return fmt.Errorf("encode synthetic dataset: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write synthetic dataset: %w", err)
	}
	return nil
}

func validateSyntheticOptions(options SyntheticOptions) error {
	if options.Dimension < 1 {
		return errors.New("synthetic dimension must be at least 1")
	}
	if options.Dimension > maxVectorDimension {
		return fmt.Errorf("synthetic dimension must be <= %d for pgvector vector compatibility", maxVectorDimension)
	}
	if options.RecordCount < 1 {
		return errors.New("synthetic record_count must be at least 1")
	}
	if options.QueryCount < 1 {
		return errors.New("synthetic query_count must be at least 1")
	}
	if options.Metric != MetricCosine && options.Metric != MetricL2 {
		return fmt.Errorf("unsupported synthetic metric %q", options.Metric)
	}
	return nil
}

func generateVector(rng *rand.Rand, dimension int, metric string) []float64 {
	vector := make([]float64, dimension)
	var normSquared float64
	for index := range vector {
		value := rng.Float64()*2 - 1
		vector[index] = value
		normSquared += value * value
	}
	if metric == MetricCosine {
		normalizeVector(vector, normSquared)
	}
	return vector
}

func normalizeVector(vector []float64, normSquared float64) {
	if normSquared == 0 {
		vector[0] = 1
		return
	}
	norm := math.Sqrt(normSquared)
	for index := range vector {
		vector[index] = vector[index] / norm
	}
}
