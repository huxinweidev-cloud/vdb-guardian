package migration

import (
	"context"
	"fmt"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
)

type milvusMigrationRecordReader interface {
	ReadMilvusMigrationRecords(ctx context.Context, collection, idField, vectorField string) ([]VectorMigrationRecord, error)
}

type pgvectorMigrationRecordWriter interface {
	WritePGVectorMigrationRecords(ctx context.Context, table, idColumn, vectorColumn string, records []VectorMigrationRecord) error
	ResetPGVectorMigrationRecords(ctx context.Context, table string) error
}

// MilvusVectorMigrationSource adapts a Milvus record reader to the generic vector migration source contract.
//
// MilvusVectorMigrationSource 将 Milvus 记录读取器适配到通用向量迁移源契约。
type MilvusVectorMigrationSource struct {
	config connectors.MilvusConfig
	reader milvusMigrationRecordReader
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
	config connectors.PGVectorConfig
	writer pgvectorMigrationRecordWriter
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

func validateMigrationAdapterIdentifier(label string, value string) error {
	if !vectorMigrationIdentifierPattern.MatchString(value) {
		return fmt.Errorf("invalid %s identifier %q", label, value)
	}
	return nil
}
