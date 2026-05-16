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
// The current runner supports one source collection, one target table, one fixed
// vector dimension, a simple batch size for record transfer, and checkpointed
// target write batch progress. Source-side cursor streaming, production bulk
// import, and stale-row reconciliation remain future increments.
//
// VectorMigrationConfig 控制着最小化的 Milvus 到 pgvector 记录迁移流程。
// 当前迁移运行器支持一个源集合、一个目标表、一个固定向量维度、简单批大小，
// 以及目标端写入批次的 checkpoint 进度。源端 cursor streaming、生产级 bulk import
// 和 stale-row reconciliation 仍属于后续增量。
type VectorMigrationConfig struct {
	SourceCollection         string
	TargetTable              string
	Dimension                int
	BatchSize                int
	CheckpointPath           string
	ResumeFromPath           string
	JobID                    string
	SchemaPlanPath           string
	RecordMappingPath        string
	SchemaPlanFingerprint    string
	RecordMappingFingerprint string
	ResumeCheckpoint         *VectorMigrationCheckpoint
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

type vectorMigrationCheckpointStore interface {
	Save(ctx context.Context, checkpoint VectorMigrationCheckpoint) error
}

type fileVectorMigrationCheckpointStore struct {
	path string
}

func (s fileVectorMigrationCheckpointStore) Save(ctx context.Context, checkpoint VectorMigrationCheckpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return WriteVectorMigrationCheckpoint(s.path, checkpoint)
}

type noopVectorMigrationCheckpointStore struct{}

func (noopVectorMigrationCheckpointStore) Save(context.Context, VectorMigrationCheckpoint) error {
	return nil
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
	config          VectorMigrationConfig
	source          vectorMigrationSource
	target          vectorMigrationTarget
	checkpointStore vectorMigrationCheckpointStore
}

// NewVectorMigrationRunner validates configuration and returns a minimal
// Milvus-to-pgvector migration runner.
//
// NewVectorMigrationRunner 校验配置，并返回一个最小化的 Milvus 到 pgvector 迁移运行器。
func NewVectorMigrationRunner(config VectorMigrationConfig, source vectorMigrationSource, target vectorMigrationTarget) (VectorMigrationRunner, error) {
	store := vectorMigrationCheckpointStore(noopVectorMigrationCheckpointStore{})
	if config.CheckpointPath != "" {
		store = fileVectorMigrationCheckpointStore{path: config.CheckpointPath}
	}
	return NewVectorMigrationRunnerWithCheckpointStore(config, source, target, store)
}

// NewVectorMigrationRunnerWithCheckpointStore validates configuration and returns
// a migration runner that persists batch-level checkpoints through the supplied
// store. Tests inject an in-memory store; CLI callers normally use a file store.
func NewVectorMigrationRunnerWithCheckpointStore(config VectorMigrationConfig, source vectorMigrationSource, target vectorMigrationTarget, checkpointStore vectorMigrationCheckpointStore) (VectorMigrationRunner, error) {
	config = applyVectorMigrationDefaults(config)
	if err := validateVectorMigrationConfig(config, source, target); err != nil {
		return VectorMigrationRunner{}, err
	}
	if checkpointStore == nil {
		checkpointStore = noopVectorMigrationCheckpointStore{}
	}
	return VectorMigrationRunner{config: config, source: source, target: target, checkpointStore: checkpointStore}, nil
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
	resumeOffset, completedBatches, recordsWritten := r.resumeState()
	if resumeOffset > len(records) {
		return VectorMigrationResult{}, fmt.Errorf("resume offset %d exceeds source record count %d", resumeOffset, len(records))
	}
	for start := resumeOffset; start < len(records); start += r.config.BatchSize {
		end := start + r.config.BatchSize
		if end > len(records) {
			end = len(records)
		}
		batchIndex := start / r.config.BatchSize
		copied := copyVectorMigrationRecords(records[start:end])
		if err := r.target.WriteRecords(ctx, r.config.TargetTable, copied); err != nil {
			failed := VectorMigrationCheckpointBatch{Index: batchIndex, Start: start, End: end, Error: SanitizeVectorMigrationCheckpointError(err)}
			checkpoint := r.buildCheckpoint(VectorMigrationCheckpointStatusFailed, len(records), recordsWritten, completedBatches, []VectorMigrationCheckpointBatch{failed}, batchIndex, start)
			if saveErr := r.checkpointStore.Save(ctx, checkpoint); saveErr != nil {
				return VectorMigrationResult{}, fmt.Errorf("write failed checkpoint: %w", saveErr)
			}
			return VectorMigrationResult{}, fmt.Errorf("write target records: %w", err)
		}
		recordsWritten += len(copied)
		completedBatches = append(completedBatches, VectorMigrationCheckpointBatch{Index: batchIndex, Start: start, End: end, RecordsWritten: len(copied)})
		checkpoint := r.buildCheckpoint(VectorMigrationCheckpointStatusRunning, len(records), recordsWritten, completedBatches, nil, batchIndex+1, end)
		if err := r.checkpointStore.Save(ctx, checkpoint); err != nil {
			return VectorMigrationResult{}, fmt.Errorf("write running checkpoint: %w", err)
		}
	}
	checkpoint := r.buildCheckpoint(VectorMigrationCheckpointStatusCompleted, len(records), recordsWritten, completedBatches, nil, len(completedBatches), len(records))
	if err := r.checkpointStore.Save(ctx, checkpoint); err != nil {
		return VectorMigrationResult{}, fmt.Errorf("write completed checkpoint: %w", err)
	}
	return VectorMigrationResult{
		SourceCollection: r.config.SourceCollection,
		TargetTable:      r.config.TargetTable,
		Dimension:        r.config.Dimension,
		RecordsRead:      len(records),
		RecordsWritten:   recordsWritten - r.resumeWrittenCount(),
	}, nil
}

func (r VectorMigrationRunner) resumeState() (int, []VectorMigrationCheckpointBatch, int) {
	if r.config.ResumeCheckpoint == nil {
		return 0, nil, 0
	}
	checkpoint := *r.config.ResumeCheckpoint
	completed := append([]VectorMigrationCheckpointBatch(nil), checkpoint.CompletedBatches...)
	return checkpoint.Resume.NextRecordOffset, completed, checkpoint.RecordsWritten
}

func (r VectorMigrationRunner) resumeWrittenCount() int {
	if r.config.ResumeCheckpoint == nil {
		return 0
	}
	return r.config.ResumeCheckpoint.RecordsWritten
}

func (r VectorMigrationRunner) buildCheckpoint(status string, recordsRead int, recordsWritten int, completed []VectorMigrationCheckpointBatch, failed []VectorMigrationCheckpointBatch, nextBatchIndex int, nextOffset int) VectorMigrationCheckpoint {
	return BuildVectorMigrationCheckpoint(VectorMigrationCheckpointInput{
		JobID:            r.config.JobID,
		Status:           status,
		SourceCollection: r.config.SourceCollection,
		TargetTable:      r.config.TargetTable,
		Dimension:        r.config.Dimension,
		BatchSize:        r.config.BatchSize,
		RecordsRead:      recordsRead,
		RecordsWritten:   recordsWritten,
		CompletedBatches: completed,
		FailedBatches:    failed,
		Resume: VectorMigrationCheckpointResume{
			NextBatchIndex:           nextBatchIndex,
			NextRecordOffset:         nextOffset,
			RecordMappingPath:        r.config.RecordMappingPath,
			SchemaPlanPath:           r.config.SchemaPlanPath,
			RecordMappingFingerprint: r.config.RecordMappingFingerprint,
			SchemaPlanFingerprint:    r.config.SchemaPlanFingerprint,
		},
	})
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
