package migration

import (
	"context"
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

type PGVectorMigrationWriteRequest struct {
	Table           string
	IDColumn        string
	VectorColumn    string
	ScalarColumns   []string
	DynamicColumn   string
	PartitionColumn string
	Records         []VectorMigrationRecord
}

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
	resolvedTable := table
	if resolvedTable == "" {
		resolvedTable = t.config.DefaultTable
	}
	if err := validateMigrationAdapterIdentifier("target table", resolvedTable); err != nil {
		return err
	}
	if t.mapping != nil {
		request, err := pgvectorWriteRequestFromMapping(*t.mapping, records)
		if err != nil {
			return err
		}
		if table != "" {
			request.Table = resolvedTable
		}
		mappingWriter, ok := t.writer.(pgvectorMappingMigrationRecordWriter)
		if !ok {
			return fmt.Errorf("pgvector migration writer does not support record mapping")
		}
		if err := mappingWriter.WritePGVectorMigrationRecordsWithMapping(ctx, request); err != nil {
			return fmt.Errorf("write pgvector migration records: %w", err)
		}
		return nil
	}
	if err := t.writer.WritePGVectorMigrationRecords(ctx, resolvedTable, t.config.IDColumn, t.config.VectorColumn, copyVectorMigrationRecords(records)); err != nil {
		return fmt.Errorf("write pgvector migration records: %w", err)
	}
	return nil
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
	return nil
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
	request := PGVectorMigrationWriteRequest{Table: mapping.TargetTable, IDColumn: mapping.PrimaryKey.TargetColumn, VectorColumn: mapping.Vector.TargetColumn, Records: copyVectorMigrationRecords(records)}
	for _, scalar := range mapping.Scalars {
		request.ScalarColumns = append(request.ScalarColumns, scalar.TargetColumn)
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
	for label, value := range map[string]string{"target table": request.Table, "pgvector id column": request.IDColumn, "pgvector vector column": request.VectorColumn, "pgvector dynamic column": request.DynamicColumn, "pgvector partition column": request.PartitionColumn} {
		if value != "" {
			if err := validateMigrationAdapterIdentifier(label, value); err != nil {
				return err
			}
		}
	}
	for _, column := range request.ScalarColumns {
		if err := validateMigrationAdapterIdentifier("pgvector scalar column", column); err != nil {
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
