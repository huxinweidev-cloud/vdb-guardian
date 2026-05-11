package migration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"

	"github.com/h3xwave/vdb-guardian/internal/fixtures"
)

const maxMilvusSeedDimension = 2000

var milvusSeedIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MilvusSeederConfig controls how synthetic fixture records are prepared for a
// Milvus collection.
//
// The first migration MVP deliberately supports one collection, one text primary
// key field, one dense vector field, and a single vector dimension. Collection
// indexing, loading, partitions, and metadata fields belong in later integration
// steps.
//
// MilvusSeederConfig 控制着如何将合成的测试固件记录准备并写入 Milvus 集合。
// 首个迁移 MVP 刻意限制了功能：仅支持单一集合、单一的文本类型主键字段、单一的稠密向量字段，
// 以及单一的向量维度。集合的索引建立、加载进内存、分区划分以及元数据字段，
// 均被安排在后续的集成步骤中实现。
type MilvusSeederConfig struct {
	Collection  string
	IDField     string
	VectorField string
	Dimension   int
	Metric      string
}

// MilvusSeedResult summarizes a synthetic fixture seeding run for Milvus.
//
// The result is intended for future CLI/job reporting so users can confirm the
// target collection, vector dimension, and number of inserted records.
//
// MilvusSeedResult 总结了针对 Milvus 的一次合成固件数据灌入执行结果。
// 该结果专为未来的 CLI 或作业报告而设计，以便用户能够直观地确认
// 数据被灌入的目标集合、向量维度以及成功插入的记录总数。
type MilvusSeedResult struct {
	Collection    string
	Dimension     int
	RecordsTotal  int
	RecordsSeeded int
}

// MilvusSeeder creates a minimal Milvus collection boundary and inserts
// synthetic fixture records.
//
// It owns write-side source database preparation for migration tests. Search and
// retrieval behavior remain in the Milvus connector so seeding and querying stay
// separated.
//
// MilvusSeeder 负责创建一个极简的 Milvus 集合边界，并向其中插入合成的固件记录。
// 它独揽了迁移测试中“数据源端写入准备”的职责。与之相对的，搜索与检索行为则保留在
// Milvus 连接器中，从而保证了数据灌入 (seeding) 与数据查询 (querying) 之间的职责分离。
type MilvusSeeder struct {
	config MilvusSeederConfig
	db     milvusSeedDB
}

type milvusSeedDB interface {
	CreateCollection(ctx context.Context, req milvusCreateCollectionRequest) error
	InsertRecords(ctx context.Context, req milvusInsertRecordsRequest) error
}

type milvusCreateCollectionRequest struct {
	Collection  string
	IDField     string
	VectorField string
	Dimension   int
	Metric      string
}

type milvusInsertRecordsRequest struct {
	Collection  string
	IDField     string
	VectorField string
	Records     []milvusSeedRecord
}

type milvusSeedRecord struct {
	ID     string
	Vector []float64
}

// NewMilvusSeeder validates configuration and returns a seeder for synthetic
// Milvus fixture data.
//
// A database adapter is required because seeding performs write-side effects.
// Unit tests can inject a fake adapter; a real Milvus SDK adapter can be added in
// a later integration step without changing the seeder behavior contract.
//
// NewMilvusSeeder 校验配置，并返回一个用于写入合成 Milvus 测试数据的灌入器。
// 由于数据灌入会产生写入层的副作用，因此数据库适配器是必填项。
// 单元测试可以注入一个假的适配器 (fake adapter)；而在后续的集成步骤中，
// 可以在不改变灌入器契约的前提下，无缝挂载一个真实的 Milvus SDK 适配器。
func NewMilvusSeeder(config MilvusSeederConfig, db milvusSeedDB) (MilvusSeeder, error) {
	config = applyMilvusSeederDefaults(config)
	if err := validateMilvusSeederConfig(config, db); err != nil {
		return MilvusSeeder{}, err
	}
	return MilvusSeeder{config: config, db: db}, nil
}

// Seed creates the Milvus collection boundary and inserts all synthetic records.
//
// It validates that the fixture dimension and every record vector match the
// configured vector field dimension before any write request reaches the adapter.
//
// Seed 创建 Milvus 的集合边界，并插入所有的合成记录。
// 在任何写入请求触达适配器之前，它会严格校验固件的整体维度以及每一条记录的向量长度，
// 确保它们均与配置的向量字段维度绝对一致。
func (s MilvusSeeder) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (MilvusSeedResult, error) {
	if err := validateMilvusSeedDataset(s.config, dataset); err != nil {
		return MilvusSeedResult{}, err
	}
	if err := s.db.CreateCollection(ctx, milvusCreateCollectionRequest{
		Collection:  s.config.Collection,
		IDField:     s.config.IDField,
		VectorField: s.config.VectorField,
		Dimension:   s.config.Dimension,
		Metric:      s.config.Metric,
	}); err != nil {
		return MilvusSeedResult{}, fmt.Errorf("create milvus collection: %w", err)
	}
	records := make([]milvusSeedRecord, len(dataset.Records))
	for index, record := range dataset.Records {
		records[index] = milvusSeedRecord{
			ID:     record.ID,
			Vector: append([]float64(nil), record.Vector...),
		}
	}
	if err := s.db.InsertRecords(ctx, milvusInsertRecordsRequest{
		Collection:  s.config.Collection,
		IDField:     s.config.IDField,
		VectorField: s.config.VectorField,
		Records:     records,
	}); err != nil {
		return MilvusSeedResult{}, fmt.Errorf("insert milvus records: %w", err)
	}
	return MilvusSeedResult{
		Collection:    s.config.Collection,
		Dimension:     s.config.Dimension,
		RecordsTotal:  len(dataset.Records),
		RecordsSeeded: len(dataset.Records),
	}, nil
}

func applyMilvusSeederDefaults(config MilvusSeederConfig) MilvusSeederConfig {
	if config.Collection == "" {
		config.Collection = "items"
	}
	if config.IDField == "" {
		config.IDField = "id"
	}
	if config.VectorField == "" {
		config.VectorField = "embedding"
	}
	if config.Metric == "" {
		config.Metric = fixtures.MetricCosine
	}
	return config
}

func validateMilvusSeederConfig(config MilvusSeederConfig, db milvusSeedDB) error {
	if db == nil {
		return errors.New("milvus seed database adapter is required")
	}
	if config.Dimension <= 0 || config.Dimension > maxMilvusSeedDimension {
		return fmt.Errorf("milvus seed dimension must be in range 1..%d", maxMilvusSeedDimension)
	}
	if err := validateMilvusSeedIdentifier("collection", config.Collection); err != nil {
		return err
	}
	if err := validateMilvusSeedIdentifier("id field", config.IDField); err != nil {
		return err
	}
	if err := validateMilvusSeedIdentifier("vector field", config.VectorField); err != nil {
		return err
	}
	if config.Metric != fixtures.MetricCosine && config.Metric != fixtures.MetricL2 {
		return fmt.Errorf("unsupported milvus seed metric %q", config.Metric)
	}
	return nil
}

func validateMilvusSeedDataset(config MilvusSeederConfig, dataset fixtures.SyntheticDataset) error {
	if dataset.Dimension != config.Dimension {
		return fmt.Errorf("synthetic dataset dimension %d does not match milvus seed dimension %d", dataset.Dimension, config.Dimension)
	}
	for index, record := range dataset.Records {
		if record.ID == "" {
			return fmt.Errorf("synthetic record at index %d has empty id", index)
		}
		if len(record.Vector) != config.Dimension {
			return fmt.Errorf("synthetic record %q vector dimension %d does not match milvus seed dimension %d", record.ID, len(record.Vector), config.Dimension)
		}
		for vectorIndex, value := range record.Vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("synthetic record %q vector contains non-finite value at index %d", record.ID, vectorIndex)
			}
		}
	}
	return nil
}

func validateMilvusSeedIdentifier(label string, value string) error {
	if !milvusSeedIdentifierPattern.MatchString(value) {
		return fmt.Errorf("invalid milvus seed %s identifier %q", label, value)
	}
	return nil
}
