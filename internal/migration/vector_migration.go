package migration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
)

const maxVectorMigrationDimension = 2000

var vectorMigrationIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// VectorMigrationConfig controls the minimal Milvus-to-pgvector record transfer.
//
// The first migration runner keeps only the boundaries required for a runnable
// MVP: one source collection, one target table, one fixed vector dimension, and
// a simple batch size for record transfer. Metadata mapping, partitions, and
// incremental checkpoints are intentionally deferred.
//
// VectorMigrationConfig 控制着最小化的 Milvus 到 pgvector 记录迁移流程。
// 首个迁移运行器仅保留 MVP 所需的边界：一个源集合、一个目标表、一个固定向量维度，
// 以及一个用于批量转移记录的简单批大小。元数据映射、分区以及增量检查点功能都被有意延后。
type VectorMigrationConfig struct {
	SourceCollection string
	TargetTable      string
	Dimension        int
	BatchSize        int
}

// VectorMigrationRecord is the normalized record model transferred between the
// source reader and target writer.
//
// The runner copies records into this neutral shape so future source connectors
// and target writers can be added without exposing database-specific SDK types.
//
// VectorMigrationRecord 是源读取器与目标写入器之间转运所使用的规范化记录模型。
// 运行器会将记录复制到这一中性结构中，从而使后续源连接器与目标写入器的扩展
// 无需暴露任何特定数据库的 SDK 类型。
type VectorMigrationRecord struct {
	ID              string
	Vector          []float64
	Scalars         map[string]any
	DynamicMetadata map[string]any
	Partition       string
}

// VectorMigrationResult summarizes one minimal migration run.
//
// The result is intended for CLI and job reporting so callers can confirm which
// source collection and target table were used and how many records were moved.
//
// VectorMigrationResult 总结了一次最小化迁移的执行结果。
// 该结果专为 CLI 与作业报告而设计，以便调用方能够确认使用了哪一个源集合与目标表，
// 以及实际转移了多少条记录。
type VectorMigrationResult struct {
	SourceCollection string
	TargetTable      string
	Dimension        int
	RecordsRead      int
	RecordsWritten   int
}

type vectorMigrationSource interface {
	ReadRecords(ctx context.Context, collection string) ([]VectorMigrationRecord, error)
}

type vectorMigrationTarget interface {
	WriteRecords(ctx context.Context, table string, records []VectorMigrationRecord) error
}

// VectorMigrationRunner transfers normalized records from a source reader into a
// target writer using a fixed vector dimension.
//
// The runner deliberately does not own connector discovery or database-specific
// SQL/SDK behavior. It simply validates the migration contract, copies records,
// and delegates to injected source and target boundaries.
//
// VectorMigrationRunner 负责使用固定的向量维度，将规范化记录从源读取器转移到目标写入器。
// 该运行器有意不承担连接器发现或数据库特定 SQL/SDK 行为；它只验证迁移契约、复制记录，
// 并把具体的读写职责委托给注入的源/目标边界。
type VectorMigrationRunner struct {
	config VectorMigrationConfig
	source vectorMigrationSource
	target vectorMigrationTarget
}

// NewVectorMigrationRunner validates configuration and returns a minimal
// Milvus-to-pgvector migration runner.
//
// NewVectorMigrationRunner 校验配置，并返回一个最小化的 Milvus 到 pgvector 迁移运行器。
func NewVectorMigrationRunner(config VectorMigrationConfig, source vectorMigrationSource, target vectorMigrationTarget) (VectorMigrationRunner, error) {
	config = applyVectorMigrationDefaults(config)
	if err := validateVectorMigrationConfig(config, source, target); err != nil {
		return VectorMigrationRunner{}, err
	}
	return VectorMigrationRunner{config: config, source: source, target: target}, nil
}

// Migrate reads records from the source collection and writes them to the target table.
//
// It validates the transferred record batch before writing so the first runner
// fails fast on malformed or incomplete source data.
//
// Migrate 从源集合读取记录并写入目标表。
// 在写入之前，它会对迁移批次进行校验，以便首个运行器能够在发现格式错误或源数据不完整时快速失败。
func (r VectorMigrationRunner) Migrate(ctx context.Context) (VectorMigrationResult, error) {
	if err := ctx.Err(); err != nil {
		return VectorMigrationResult{}, err
	}
	records, err := r.source.ReadRecords(ctx, r.config.SourceCollection)
	if err != nil {
		return VectorMigrationResult{}, fmt.Errorf("read source records: %w", err)
	}
	if err := validateVectorMigrationRecords(r.config, records); err != nil {
		return VectorMigrationResult{}, err
	}
	copied := copyVectorMigrationRecords(records)
	if err := r.target.WriteRecords(ctx, r.config.TargetTable, copied); err != nil {
		return VectorMigrationResult{}, fmt.Errorf("write target records: %w", err)
	}
	return VectorMigrationResult{
		SourceCollection: r.config.SourceCollection,
		TargetTable:      r.config.TargetTable,
		Dimension:        r.config.Dimension,
		RecordsRead:      len(records),
		RecordsWritten:   len(copied),
	}, nil
}

func applyVectorMigrationDefaults(config VectorMigrationConfig) VectorMigrationConfig {
	if config.SourceCollection == "" {
		config.SourceCollection = DefaultSeedCollection
	}
	if config.TargetTable == "" {
		config.TargetTable = DefaultSeedCollection
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	return config
}

func validateVectorMigrationConfig(config VectorMigrationConfig, source vectorMigrationSource, target vectorMigrationTarget) error {
	if source == nil {
		return errors.New("source reader is required")
	}
	if target == nil {
		return errors.New("target writer is required")
	}
	if config.Dimension <= 0 || config.Dimension > maxVectorMigrationDimension {
		return fmt.Errorf("dimension must be in range 1..%d", maxVectorMigrationDimension)
	}
	if config.BatchSize <= 0 {
		return errors.New("batch size must be positive")
	}
	if err := validateVectorMigrationIdentifier("source collection", config.SourceCollection); err != nil {
		return err
	}
	if err := validateVectorMigrationIdentifier("target table", config.TargetTable); err != nil {
		return err
	}
	return nil
}

func validateVectorMigrationIdentifier(label string, value string) error {
	if !vectorMigrationIdentifierPattern.MatchString(value) {
		return fmt.Errorf("invalid vector migration %s identifier %q", label, value)
	}
	return nil
}

func validateVectorMigrationRecords(config VectorMigrationConfig, records []VectorMigrationRecord) error {
	for index, record := range records {
		if record.ID == "" {
			return fmt.Errorf("record at index %d has empty id", index)
		}
		if len(record.Vector) != config.Dimension {
			return fmt.Errorf("record %q vector dimension %d does not match migration dimension %d", record.ID, len(record.Vector), config.Dimension)
		}
		for vectorIndex, value := range record.Vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("record %q vector contains non-finite value at index %d", record.ID, vectorIndex)
			}
		}
		if err := validateMigrationRecordValues(record.ID, "scalar", record.Scalars); err != nil {
			return err
		}
		if err := validateMigrationRecordValues(record.ID, "dynamic metadata", record.DynamicMetadata); err != nil {
			return err
		}
	}
	return nil
}

func validateMigrationRecordValues(recordID string, label string, values map[string]any) error {
	for key, value := range values {
		if err := validateMigrationRecordValue(recordID, label, key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateMigrationRecordValue(recordID string, label string, key string, value any) error {
	switch typed := value.(type) {
	case float32:
		if math.IsNaN(float64(typed)) || math.IsInf(float64(typed), 0) {
			return fmt.Errorf("record %q %s %q contains non-finite value", recordID, label, key)
		}
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return fmt.Errorf("record %q %s %q contains non-finite value", recordID, label, key)
		}
	case []any:
		for index, item := range typed {
			if err := validateMigrationRecordValue(recordID, label, fmt.Sprintf("%s[%d]", key, index), item); err != nil {
				return err
			}
		}
	case map[string]any:
		for nestedKey, item := range typed {
			if err := validateMigrationRecordValue(recordID, label, key+"."+nestedKey, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyVectorMigrationRecords(records []VectorMigrationRecord) []VectorMigrationRecord {
	copied := make([]VectorMigrationRecord, len(records))
	for index, record := range records {
		copied[index] = VectorMigrationRecord{
			ID:              record.ID,
			Vector:          append([]float64(nil), record.Vector...),
			Scalars:         copyMigrationValueMap(record.Scalars),
			DynamicMetadata: copyMigrationValueMap(record.DynamicMetadata),
			Partition:       record.Partition,
		}
	}
	return copied
}

func copyMigrationValueMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = copyMigrationValue(value)
	}
	return copied
}

func copyMigrationValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return copyMigrationValueMap(typed)
	case []any:
		copied := make([]any, len(typed))
		for index, item := range typed {
			copied[index] = copyMigrationValue(item)
		}
		return copied
	case []string:
		return append([]string(nil), typed...)
	case []float64:
		return append([]float64(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	default:
		return typed
	}
}
