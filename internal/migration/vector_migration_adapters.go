package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
)

type MilvusMigrationReadRequest struct {
	Collection     string
	IDField        string
	VectorField    string
	ScalarFields   []string
	DynamicField   string
	PartitionField string
}

type PGVectorMigrationScalarColumn struct {
	SourceField  string
	TargetColumn string
}

type PGVectorMigrationWriteRequest struct {
	Table           string
	IDColumn        string
	VectorColumn    string
	WriteMode       PGVectorMigrationWriteMode
	ScalarColumns   []PGVectorMigrationScalarColumn
	DynamicColumn   string
	PartitionColumn string
	Records         []VectorMigrationRecord
}

// PGVectorMigrationWriteMode identifies how pgvector migration records should be written.
//
// The empty mode is accepted for backward-compatible configuration and is
// normalized to PGVectorMigrationWriteModeBatchUpsert before writes are issued.
type PGVectorMigrationWriteMode string

const (
	// PGVectorMigrationWriteModeBatchUpsert writes records with the existing row-by-row upsert path.
	PGVectorMigrationWriteModeBatchUpsert PGVectorMigrationWriteMode = "batch-upsert"
	// PGVectorMigrationWriteModeCopy reserves the PostgreSQL COPY bulk-import path.
	PGVectorMigrationWriteModeCopy PGVectorMigrationWriteMode = "copy"
	// PGVectorMigrationWriteModeAuto lets the migration target select the best supported write path.
	PGVectorMigrationWriteModeAuto PGVectorMigrationWriteMode = "auto"
)

type milvusMigrationRecordReader interface {
	ReadMilvusMigrationRecords(ctx context.Context, collection, idField, vectorField string) ([]VectorMigrationRecord, error)
}

type milvusMappingMigrationRecordReader interface {
	ReadMilvusMigrationRecordsWithMapping(ctx context.Context, request MilvusMigrationReadRequest) ([]VectorMigrationRecord, error)
}

type pgvectorMigrationRecordWriter interface {
	WritePGVectorMigrationRecords(ctx context.Context, table, idColumn, vectorColumn string, records []VectorMigrationRecord) error
	ResetPGVectorMigrationRecords(ctx context.Context, table string) error
}

type pgvectorMappingMigrationRecordWriter interface {
	WritePGVectorMigrationRecordsWithMapping(ctx context.Context, request PGVectorMigrationWriteRequest) error
}

type pgvectorCopyMigrationRecordWriter interface {
	WritePGVectorMigrationRecordsWithMappingCopy(ctx context.Context, request PGVectorMigrationWriteRequest) error
}

type pgvectorCopyExecutionError struct {
	err error
}

func newPGVectorCopyExecutionError(err error) error {
	if err == nil {
		return nil
	}
	return pgvectorCopyExecutionError{err: err}
}

func (e pgvectorCopyExecutionError) Error() string {
	return fmt.Sprintf("copy pgvector migration records: %v", e.err)
}

func (e pgvectorCopyExecutionError) Unwrap() error {
	return e.err
}

// MilvusVectorMigrationSource adapts a Milvus record reader to the generic vector migration source contract.
//
// MilvusVectorMigrationSource 将 Milvus 记录读取器适配到通用向量迁移源契约。
type MilvusVectorMigrationSource struct {
	config  connectors.MilvusConfig
	reader  milvusMigrationRecordReader
	mapping *CollectionRecordMapping
}

// NewMilvusVectorMigrationSource validates config and returns a Milvus-backed migration source.
//
// NewMilvusVectorMigrationSource 校验配置，并返回一个由 Milvus 支撑的迁移源。
func NewMilvusVectorMigrationSource(config connectors.MilvusConfig, reader milvusMigrationRecordReader) (MilvusVectorMigrationSource, error) {
	if err := validateMilvusMigrationSourceConfig(config, reader); err != nil {
		return MilvusVectorMigrationSource{}, err
	}
	if config.DefaultCollection == "" {
		config.DefaultCollection = DefaultSeedCollection
	}
	if config.IDField == "" {
		config.IDField = DefaultSeedIDField
	}
	if config.VectorField == "" {
		config.VectorField = DefaultSeedVectorField
	}
	if reader == nil {
		reader = newMilvusSDKMigrationReader(config.Address)
	}
	return MilvusVectorMigrationSource{config: config, reader: reader}, nil
}

// WithRecordMapping returns a source configured to read the fields required by a full-record mapping.
//
// WithRecordMapping 返回一个按 full-record mapping 所需字段读取的迁移源。
func (s MilvusVectorMigrationSource) WithRecordMapping(mapping CollectionRecordMapping) MilvusVectorMigrationSource {
	s.mapping = &mapping
	return s
}

// ReadRecords reads normalized vector migration records from Milvus.
//
// ReadRecords 从 Milvus 读取规范化的向量迁移记录。
func (s MilvusVectorMigrationSource) ReadRecords(ctx context.Context, collection string) ([]VectorMigrationRecord, error) {
	resolvedCollection := collection
	if resolvedCollection == "" {
		resolvedCollection = s.config.DefaultCollection
	}
	if err := validateMigrationAdapterIdentifier("source collection", resolvedCollection); err != nil {
		return nil, err
	}
	if s.mapping != nil {
		request, err := milvusReadRequestFromMapping(*s.mapping)
		if err != nil {
			return nil, err
		}
		if collection != "" {
			request.Collection = resolvedCollection
		}
		mappingReader, ok := s.reader.(milvusMappingMigrationRecordReader)
		if !ok {
			return nil, fmt.Errorf("milvus migration reader does not support record mapping")
		}
		records, err := mappingReader.ReadMilvusMigrationRecordsWithMapping(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("query milvus migration records: %w", err)
		}
		return copyVectorMigrationRecords(records), nil
	}
	records, err := s.reader.ReadMilvusMigrationRecords(ctx, resolvedCollection, s.config.IDField, s.config.VectorField)
	if err != nil {
		return nil, fmt.Errorf("query milvus migration records: %w", err)
	}
	return copyVectorMigrationRecords(records), nil
}

// PGVectorMigrationTarget adapts a pgvector record writer to the generic vector migration target contract.
//
// PGVectorMigrationTarget 将 pgvector 记录写入器适配到通用向量迁移目标契约。
type PGVectorMigrationTarget struct {
	config  connectors.PGVectorConfig
	writer  pgvectorMigrationRecordWriter
	mapping *CollectionRecordMapping
}

// NewPGVectorMigrationTarget validates config and returns a pgvector-backed migration target.
//
// NewPGVectorMigrationTarget 校验配置，并返回一个由 pgvector 支撑的迁移目标。
func NewPGVectorMigrationTarget(config connectors.PGVectorConfig, writer pgvectorMigrationRecordWriter) (PGVectorMigrationTarget, error) {
	if err := validatePGVectorMigrationTargetConfig(config, writer); err != nil {
		return PGVectorMigrationTarget{}, err
	}
	if config.DefaultTable == "" {
		config.DefaultTable = DefaultSeedCollection
	}
	if config.IDColumn == "" {
		config.IDColumn = DefaultSeedIDField
	}
	if config.VectorColumn == "" {
		config.VectorColumn = DefaultSeedVectorField
	}
	config.WriteMode = string(normalizePGVectorMigrationWriteMode(PGVectorMigrationWriteMode(config.WriteMode)))
	if writer == nil {
		writer = newPGXPGVectorMigrationWriter(config.ConnectionURL)
	}
	return PGVectorMigrationTarget{config: config, writer: writer}, nil
}

// WithRecordMapping returns a target configured to write all columns required by a full-record mapping.
//
// WithRecordMapping 返回一个按 full-record mapping 所需列写入的迁移目标。
func (t PGVectorMigrationTarget) WithRecordMapping(mapping CollectionRecordMapping) PGVectorMigrationTarget {
	t.mapping = &mapping
	return t
}

// WriteRecords writes normalized vector migration records to pgvector.
//
// WriteRecords 将规范化向量迁移记录写入 pgvector。
func (t PGVectorMigrationTarget) WriteRecords(ctx context.Context, table string, records []VectorMigrationRecord) error {
	_, err := t.WriteRecordsWithResult(ctx, table, records)
	return err
}

// WriteRecordsWithResult writes normalized vector migration records to pgvector and returns per-batch write-mode metrics.
//
// It preserves the existing pgvector target behavior while exposing the mode
// selected for this batch so the migration runner can aggregate write metrics.
func (t PGVectorMigrationTarget) WriteRecordsWithResult(ctx context.Context, table string, records []VectorMigrationRecord) (VectorMigrationWriteResult, error) {
	resolvedTable := table
	if resolvedTable == "" {
		resolvedTable = t.config.DefaultTable
	}
	if err := validateMigrationAdapterIdentifier("target table", resolvedTable); err != nil {
		return VectorMigrationWriteResult{}, err
	}
	writeMode := normalizePGVectorMigrationWriteMode(PGVectorMigrationWriteMode(t.config.WriteMode))
	if t.mapping != nil {
		request, err := pgvectorWriteRequestFromMapping(*t.mapping, records)
		if err != nil {
			return VectorMigrationWriteResult{}, err
		}
		request.WriteMode = writeMode
		if table != "" {
			request.Table = resolvedTable
		}
		return t.writePGVectorMigrationRequest(ctx, request)
	}
	request := PGVectorMigrationWriteRequest{
		Table:        resolvedTable,
		IDColumn:     t.config.IDColumn,
		VectorColumn: t.config.VectorColumn,
		WriteMode:    writeMode,
		Records:      copyVectorMigrationRecords(records),
	}
	return t.writePGVectorMigrationRequest(ctx, request)
}

func (t PGVectorMigrationTarget) writePGVectorMigrationRequest(ctx context.Context, request PGVectorMigrationWriteRequest) (VectorMigrationWriteResult, error) {
	switch normalizePGVectorMigrationWriteMode(request.WriteMode) {
	case PGVectorMigrationWriteModeCopy:
		copyWriter, ok := t.writer.(pgvectorCopyMigrationRecordWriter)
		if !ok {
			return VectorMigrationWriteResult{}, fmt.Errorf("pgvector migration writer does not support pgvector copy migration write mode")
		}
		if err := copyWriter.WritePGVectorMigrationRecordsWithMappingCopy(ctx, request); err != nil {
			return VectorMigrationWriteResult{}, fmt.Errorf("write pgvector migration records with copy: %w", err)
		}
		return vectorMigrationWriteResultForMode(PGVectorMigrationWriteModeCopy), nil
	case PGVectorMigrationWriteModeAuto:
		copyWriter, ok := t.writer.(pgvectorCopyMigrationRecordWriter)
		if !ok {
			return t.writePGVectorMigrationRequestBatchUpsert(ctx, request)
		}
		copyRequest := request
		copyRequest.WriteMode = PGVectorMigrationWriteModeCopy
		if err := copyWriter.WritePGVectorMigrationRecordsWithMappingCopy(ctx, copyRequest); err != nil {
			if !isRecoverablePGVectorCopyFailure(err) {
				return VectorMigrationWriteResult{}, fmt.Errorf("write pgvector migration records with copy: %w", err)
			}
			result, fallbackErr := t.writePGVectorMigrationRequestBatchUpsert(ctx, request)
			if fallbackErr != nil {
				return VectorMigrationWriteResult{}, fallbackErr
			}
			result.CopyFallbacks++
			return result, nil
		}
		return vectorMigrationWriteResultForMode(PGVectorMigrationWriteModeCopy), nil
	case PGVectorMigrationWriteModeBatchUpsert:
		return t.writePGVectorMigrationRequestBatchUpsert(ctx, request)
	default:
		return VectorMigrationWriteResult{}, fmt.Errorf("unsupported pgvector migration write mode %q", request.WriteMode)
	}
}

func (t PGVectorMigrationTarget) writePGVectorMigrationRequestBatchUpsert(ctx context.Context, request PGVectorMigrationWriteRequest) (VectorMigrationWriteResult, error) {
	if t.mapping != nil {
		mappingWriter, ok := t.writer.(pgvectorMappingMigrationRecordWriter)
		if !ok {
			return VectorMigrationWriteResult{}, fmt.Errorf("pgvector migration writer does not support record mapping")
		}
		batchRequest := request
		batchRequest.WriteMode = PGVectorMigrationWriteModeBatchUpsert
		if err := mappingWriter.WritePGVectorMigrationRecordsWithMapping(ctx, batchRequest); err != nil {
			return VectorMigrationWriteResult{}, fmt.Errorf("write pgvector migration records: %w", err)
		}
		return vectorMigrationWriteResultForActualBatchUpsert(), nil
	}
	if err := t.writer.WritePGVectorMigrationRecords(ctx, request.Table, request.IDColumn, request.VectorColumn, copyVectorMigrationRecords(request.Records)); err != nil {
		return VectorMigrationWriteResult{}, fmt.Errorf("write pgvector migration records: %w", err)
	}
	return vectorMigrationWriteResultForActualBatchUpsert(), nil
}

func isRecoverablePGVectorCopyFailure(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var copyErr pgvectorCopyExecutionError
	return errors.As(err, &copyErr)
}

// ResetRecords truncates the pgvector target table before a clean migration verification run.
//
// ResetRecords 在干净迁移验证运行前清空 pgvector 目标表。
func (t PGVectorMigrationTarget) ResetRecords(ctx context.Context, table string) error {
	resolvedTable := table
	if resolvedTable == "" {
		resolvedTable = t.config.DefaultTable
	}
	if err := validateMigrationAdapterIdentifier("target table", resolvedTable); err != nil {
		return err
	}
	if err := t.writer.ResetPGVectorMigrationRecords(ctx, resolvedTable); err != nil {
		return fmt.Errorf("reset pgvector migration records: %w", err)
	}
	return nil
}

func validateMilvusMigrationSourceConfig(config connectors.MilvusConfig, reader milvusMigrationRecordReader) error {
	if config.Address == "" && reader == nil {
		return fmt.Errorf("milvus address is required when no migration record reader is injected")
	}
	if config.DefaultCollection != "" {
		if err := validateMigrationAdapterIdentifier("milvus default collection", config.DefaultCollection); err != nil {
			return err
		}
	}
	if config.IDField != "" {
		if err := validateMigrationAdapterIdentifier("milvus id field", config.IDField); err != nil {
			return err
		}
	}
	if config.VectorField != "" {
		if err := validateMigrationAdapterIdentifier("milvus vector field", config.VectorField); err != nil {
			return err
		}
	}
	return nil
}

func validatePGVectorMigrationTargetConfig(config connectors.PGVectorConfig, writer pgvectorMigrationRecordWriter) error {
	if config.ConnectionURL == "" && writer == nil {
		return fmt.Errorf("pgvector connection URL is required when no migration record writer is injected")
	}
	if config.DefaultTable != "" {
		if err := validateMigrationAdapterIdentifier("pgvector default table", config.DefaultTable); err != nil {
			return err
		}
	}
	if config.IDColumn != "" {
		if err := validateMigrationAdapterIdentifier("pgvector id column", config.IDColumn); err != nil {
			return err
		}
	}
	if config.VectorColumn != "" {
		if err := validateMigrationAdapterIdentifier("pgvector vector column", config.VectorColumn); err != nil {
			return err
		}
	}
	if err := validatePGVectorMigrationWriteMode(PGVectorMigrationWriteMode(config.WriteMode)); err != nil {
		return err
	}
	return nil
}

func validatePGVectorMigrationWriteMode(mode PGVectorMigrationWriteMode) error {
	switch mode {
	case "", PGVectorMigrationWriteModeBatchUpsert, PGVectorMigrationWriteModeCopy, PGVectorMigrationWriteModeAuto:
		return nil
	default:
		return fmt.Errorf("unsupported pgvector migration write mode %q", mode)
	}
}

func normalizePGVectorMigrationWriteMode(mode PGVectorMigrationWriteMode) PGVectorMigrationWriteMode {
	if mode == "" {
		return PGVectorMigrationWriteModeBatchUpsert
	}
	return mode
}

func vectorMigrationWriteResultForActualBatchUpsert() VectorMigrationWriteResult {
	return vectorMigrationWriteResultForMode(PGVectorMigrationWriteModeBatchUpsert)
}

func vectorMigrationWriteResultForMode(mode PGVectorMigrationWriteMode) VectorMigrationWriteResult {
	result := VectorMigrationWriteResult{WriteModeUsed: string(mode)}
	switch mode {
	case PGVectorMigrationWriteModeCopy:
		result.CopyBatches = 1
	default:
		result.BatchUpsertBatches = 1
	}
	return result
}

// MilvusReadRequestFromRecordMapping converts a collection record mapping into
// the Milvus read request needed to fetch full records from the source collection.
func MilvusReadRequestFromRecordMapping(mapping CollectionRecordMapping) (MilvusMigrationReadRequest, error) {
	return milvusReadRequestFromMapping(mapping)
}

// PGVectorWriteRequestFromRecordMapping converts a collection record mapping into
// the pgvector write request needed to store mapped full records.
func PGVectorWriteRequestFromRecordMapping(mapping CollectionRecordMapping, records []VectorMigrationRecord) (PGVectorMigrationWriteRequest, error) {
	return pgvectorWriteRequestFromMapping(mapping, records)
}

func milvusReadRequestFromMapping(mapping CollectionRecordMapping) (MilvusMigrationReadRequest, error) {
	if mapping.PrimaryKey == nil {
		return MilvusMigrationReadRequest{}, fmt.Errorf("record mapping primary key is required")
	}
	if mapping.Vector == nil {
		return MilvusMigrationReadRequest{}, fmt.Errorf("record mapping vector is required")
	}
	request := MilvusMigrationReadRequest{Collection: mapping.SourceCollection, IDField: mapping.PrimaryKey.SourceField, VectorField: mapping.Vector.SourceField}
	for _, scalar := range mapping.Scalars {
		request.ScalarFields = append(request.ScalarFields, scalar.SourceField)
	}
	if mapping.DynamicMetadata != nil {
		request.DynamicField = mapping.DynamicMetadata.SourceField
	}
	if mapping.PartitionMetadata != nil {
		request.PartitionField = mapping.PartitionMetadata.SourceField
	}
	if err := validateMigrationReadRequest(request); err != nil {
		return MilvusMigrationReadRequest{}, err
	}
	return request, nil
}

func pgvectorWriteRequestFromMapping(mapping CollectionRecordMapping, records []VectorMigrationRecord) (PGVectorMigrationWriteRequest, error) {
	if mapping.PrimaryKey == nil {
		return PGVectorMigrationWriteRequest{}, fmt.Errorf("record mapping primary key is required")
	}
	if mapping.Vector == nil {
		return PGVectorMigrationWriteRequest{}, fmt.Errorf("record mapping vector is required")
	}
	request := PGVectorMigrationWriteRequest{Table: mapping.TargetTable, IDColumn: mapping.PrimaryKey.TargetColumn, VectorColumn: mapping.Vector.TargetColumn, WriteMode: PGVectorMigrationWriteModeBatchUpsert, Records: copyVectorMigrationRecords(records)}
	for _, scalar := range mapping.Scalars {
		request.ScalarColumns = append(request.ScalarColumns, PGVectorMigrationScalarColumn{SourceField: scalar.SourceField, TargetColumn: scalar.TargetColumn})
	}
	if mapping.DynamicMetadata != nil {
		request.DynamicColumn = mapping.DynamicMetadata.TargetColumn
	}
	if mapping.PartitionMetadata != nil {
		request.PartitionColumn = mapping.PartitionMetadata.TargetColumn
	}
	if err := validateMigrationWriteRequest(request); err != nil {
		return PGVectorMigrationWriteRequest{}, err
	}
	return request, nil
}

func validateMigrationReadRequest(request MilvusMigrationReadRequest) error {
	for label, value := range map[string]string{"source collection": request.Collection, "milvus id field": request.IDField, "milvus vector field": request.VectorField, "milvus dynamic field": request.DynamicField, "milvus partition field": request.PartitionField} {
		if value != "" {
			if err := validateMigrationAdapterIdentifier(label, value); err != nil {
				return err
			}
		}
	}
	for _, field := range request.ScalarFields {
		if err := validateMigrationAdapterIdentifier("milvus scalar field", field); err != nil {
			return err
		}
	}
	return nil
}

func validateMigrationWriteRequest(request PGVectorMigrationWriteRequest) error {
	if err := validatePGVectorMigrationWriteMode(request.WriteMode); err != nil {
		return err
	}
	for label, value := range map[string]string{"target table": request.Table, "pgvector id column": request.IDColumn, "pgvector vector column": request.VectorColumn, "pgvector dynamic column": request.DynamicColumn, "pgvector partition column": request.PartitionColumn} {
		if value != "" {
			if err := validateMigrationAdapterIdentifier(label, value); err != nil {
				return err
			}
		}
	}
	for _, scalar := range request.ScalarColumns {
		if err := validateMigrationAdapterIdentifier("pgvector scalar source field", scalar.SourceField); err != nil {
			return err
		}
		if err := validateMigrationAdapterIdentifier("pgvector scalar column", scalar.TargetColumn); err != nil {
			return err
		}
	}
	return nil
}

func validateMigrationAdapterIdentifier(label string, value string) error {
	if !vectorMigrationIdentifierPattern.MatchString(value) {
		return fmt.Errorf("invalid %s identifier %q", label, value)
	}
	return nil
}
