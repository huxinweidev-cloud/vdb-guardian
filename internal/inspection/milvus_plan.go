package inspection

import "fmt"

// MilvusInspectionSchemaVersion identifies the stable JSON schema emitted by the
// first Milvus inspection plan. Consumers can use this value to reject unknown
// plan formats before attempting migration planning.
const MilvusInspectionSchemaVersion = "v1"

const (
	// SupportLevelSupported means the inspected Milvus feature has a direct and
	// currently planned PostgreSQL or pgvector representation.
	SupportLevelSupported = "supported"
	// SupportLevelDegraded means the feature can be preserved with reduced
	// semantics, usually as JSONB metadata or a non-identical index strategy.
	SupportLevelDegraded = "degraded"
	// SupportLevelUnsupported means the feature cannot be represented by the first
	// pgvector migration plan and must be handled manually or skipped.
	SupportLevelUnsupported = "unsupported"
)

const (
	// MilvusDataTypeBool represents a Milvus boolean scalar field.
	MilvusDataTypeBool = "Bool"
	// MilvusDataTypeInt8 represents a Milvus 8-bit integer scalar field.
	MilvusDataTypeInt8 = "Int8"
	// MilvusDataTypeInt16 represents a Milvus 16-bit integer scalar field.
	MilvusDataTypeInt16 = "Int16"
	// MilvusDataTypeInt32 represents a Milvus 32-bit integer scalar field.
	MilvusDataTypeInt32 = "Int32"
	// MilvusDataTypeInt64 represents a Milvus 64-bit integer scalar field.
	MilvusDataTypeInt64 = "Int64"
	// MilvusDataTypeFloat represents a Milvus single precision scalar field.
	MilvusDataTypeFloat = "Float"
	// MilvusDataTypeDouble represents a Milvus double precision scalar field.
	MilvusDataTypeDouble = "Double"
	// MilvusDataTypeVarChar represents a Milvus variable length text field.
	MilvusDataTypeVarChar = "VarChar"
	// MilvusDataTypeJSON represents a Milvus JSON field.
	MilvusDataTypeJSON = "JSON"
	// MilvusDataTypeFloatVector represents a dense float vector field.
	MilvusDataTypeFloatVector = "FloatVector"
	// MilvusDataTypeBinaryVector represents a binary vector field.
	MilvusDataTypeBinaryVector = "BinaryVector"
	// MilvusDataTypeSparseFloatVector represents a sparse float vector field.
	MilvusDataTypeSparseFloatVector = "SparseFloatVector"
	// MilvusDataTypeArray represents a Milvus array field.
	MilvusDataTypeArray = "Array"
)

// MilvusInspectionPlan is the machine-readable result of inspecting Milvus
// metadata before any data migration or target database writes occur.
type MilvusInspectionPlan struct {
	SchemaVersion string                  `json:"schema_version"`
	Source        MilvusInspectionSource  `json:"source"`
	Collections   []MilvusCollectionPlan  `json:"collections"`
	Summary       MilvusInspectionSummary `json:"summary"`
}

// MilvusInspectionSource identifies the source system that was inspected while
// avoiding credentials or sensitive connection material.
type MilvusInspectionSource struct {
	Type    string `json:"type"`
	Address string `json:"address,omitempty"`
}

// MilvusCollectionPlan describes one inspected Milvus collection and the first
// pgvector migration recommendations derived from its metadata.
type MilvusCollectionPlan struct {
	Name                string                `json:"name"`
	RowCount            int64                 `json:"row_count"`
	Description         string                `json:"description,omitempty"`
	AutoID              bool                  `json:"auto_id"`
	DynamicFieldEnabled bool                  `json:"dynamic_field_enabled"`
	PrimaryKey          string                `json:"primary_key,omitempty"`
	Fields              []MilvusFieldPlan     `json:"fields"`
	Indexes             []MilvusIndexPlan     `json:"indexes,omitempty"`
	Partitions          []MilvusPartitionPlan `json:"partitions,omitempty"`
	Warnings            []string              `json:"warnings,omitempty"`
}

// MilvusFieldPlan describes one source Milvus field plus the recommended target
// PostgreSQL or pgvector type for later schema creation.
type MilvusFieldPlan struct {
	Name         string `json:"name"`
	SourceType   string `json:"source_type"`
	TargetType   string `json:"target_type,omitempty"`
	Dimension    int    `json:"dimension,omitempty"`
	MaxLength    int    `json:"max_length,omitempty"`
	PrimaryKey   bool   `json:"primary_key,omitempty"`
	Nullable     bool   `json:"nullable"`
	SupportLevel string `json:"support_level"`
	Warning      string `json:"warning,omitempty"`
}

// MilvusIndexPlan describes one source index and the approximate target index
// recommendation, if a pgvector or PostgreSQL equivalent is known.
type MilvusIndexPlan struct {
	Field           string            `json:"field"`
	SourceIndexType string            `json:"source_index_type"`
	SourceMetric    string            `json:"source_metric,omitempty"`
	TargetIndexType string            `json:"target_index_type,omitempty"`
	TargetOps       string            `json:"target_ops,omitempty"`
	Params          map[string]string `json:"params,omitempty"`
	SupportLevel    string            `json:"support_level"`
	Warning         string            `json:"warning,omitempty"`
}

// MilvusPartitionPlan preserves source partition metadata for later migration
// strategy decisions without writing partitioned target tables in phase one.
type MilvusPartitionPlan struct {
	Name                string `json:"name"`
	SupportLevel        string `json:"support_level"`
	RecommendedStrategy string `json:"recommended_strategy,omitempty"`
}

// MilvusInspectionSummary contains aggregate counts for quick CLI reporting and
// for CI checks that reject unsupported migration plans.
type MilvusInspectionSummary struct {
	CollectionCount          int `json:"collection_count"`
	SupportedCollectionCount int `json:"supported_collection_count"`
	WarningCount             int `json:"warning_count"`
	UnsupportedFeatureCount  int `json:"unsupported_feature_count"`
}

// MapMilvusFieldToPGVector maps a Milvus field into a PostgreSQL or pgvector
// target type recommendation while preserving support-level warnings.
func MapMilvusFieldToPGVector(field MilvusFieldPlan) MilvusFieldPlan {
	mapped := field
	mapped.SupportLevel = SupportLevelSupported
	switch field.SourceType {
	case MilvusDataTypeBool:
		mapped.TargetType = "boolean"
	case MilvusDataTypeInt8, MilvusDataTypeInt16:
		mapped.TargetType = "smallint"
	case MilvusDataTypeInt32:
		mapped.TargetType = "integer"
	case MilvusDataTypeInt64:
		mapped.TargetType = "bigint"
	case MilvusDataTypeFloat:
		mapped.TargetType = "real"
	case MilvusDataTypeDouble:
		mapped.TargetType = "double precision"
	case MilvusDataTypeVarChar:
		if field.MaxLength > 0 {
			mapped.TargetType = fmt.Sprintf("varchar(%d)", field.MaxLength)
		} else {
			mapped.TargetType = "text"
		}
	case MilvusDataTypeJSON:
		mapped.TargetType = "jsonb"
	case MilvusDataTypeFloatVector:
		if field.Dimension > 0 {
			mapped.TargetType = fmt.Sprintf("vector(%d)", field.Dimension)
		} else {
			mapped.TargetType = "vector"
			mapped.SupportLevel = SupportLevelDegraded
			mapped.Warning = "float vector dimension is unknown; target DDL must be reviewed"
		}
	case MilvusDataTypeBinaryVector:
		mapped.TargetType = "bytea"
		mapped.SupportLevel = SupportLevelDegraded
		mapped.Warning = "binary vectors are preserved as bytes and do not have pgvector search parity"
	case MilvusDataTypeSparseFloatVector:
		mapped.TargetType = "jsonb"
		mapped.SupportLevel = SupportLevelDegraded
		mapped.Warning = "sparse vectors are preserved as jsonb until sparsevec support is enabled"
	case MilvusDataTypeArray:
		mapped.TargetType = "jsonb"
		mapped.SupportLevel = SupportLevelDegraded
		mapped.Warning = "array fields are preserved as jsonb until PostgreSQL array mapping is configured"
	default:
		mapped.SupportLevel = SupportLevelUnsupported
		mapped.Warning = fmt.Sprintf("Milvus data type %q is not mapped to pgvector yet", field.SourceType)
	}
	return mapped
}

// BuildMilvusInspectionSummary computes aggregate plan counts from collection
// plans so the CLI can report risk without parsing every nested feature.
func BuildMilvusInspectionSummary(collections []MilvusCollectionPlan) MilvusInspectionSummary {
	summary := MilvusInspectionSummary{CollectionCount: len(collections)}
	for _, collection := range collections {
		collectionUnsupported := false
		collectionWarnings := len(collection.Warnings)
		for _, field := range collection.Fields {
			if field.Warning != "" {
				collectionWarnings++
			}
			if field.SupportLevel == SupportLevelUnsupported {
				summary.UnsupportedFeatureCount++
				collectionUnsupported = true
			}
		}
		for _, index := range collection.Indexes {
			if index.Warning != "" {
				collectionWarnings++
			}
			if index.SupportLevel == SupportLevelUnsupported {
				summary.UnsupportedFeatureCount++
				collectionUnsupported = true
			}
		}
		summary.WarningCount += collectionWarnings
		if !collectionUnsupported && collectionWarnings == 0 {
			summary.SupportedCollectionCount++
		}
	}
	return summary
}
