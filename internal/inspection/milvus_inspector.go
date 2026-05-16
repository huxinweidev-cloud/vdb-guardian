package inspection

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// MilvusInspectorOptions controls which Milvus metadata should be inspected.
// Phase one keeps the options read-only: no target database writes or source
// collection mutations are performed.
type MilvusInspectorOptions struct {
	Address    string
	Collection string
}

// MilvusMetadataClient abstracts Milvus metadata reads required to build a
// migration plan without coupling inspector tests to the Milvus SDK or Docker.
type MilvusMetadataClient interface {
	ListCollections(ctx context.Context) ([]string, error)
	DescribeCollection(ctx context.Context, collection string) (MilvusCollectionMetadata, error)
}

// MilvusCollectionMetadata is the source-side collection description returned
// by a metadata client before it is converted into a migration inspection plan.
type MilvusCollectionMetadata struct {
	Name                string
	Description         string
	AutoID              bool
	DynamicFieldEnabled bool
	PrimaryKey          string
	RowCount            int64
	Fields              []MilvusFieldMetadata
	Indexes             []MilvusIndexMetadata
	Partitions          []MilvusPartitionMetadata
}

// MilvusFieldMetadata is the normalized representation of a Milvus field used
// by the inspector's schema and type-mapping logic.
type MilvusFieldMetadata struct {
	Name       string
	DataType   string
	Dimension  int
	MaxLength  int
	PrimaryKey bool
	Nullable   bool
}

// MilvusIndexMetadata is the normalized representation of a Milvus index used
// to recommend approximate pgvector or PostgreSQL index strategies.
type MilvusIndexMetadata struct {
	Field      string
	IndexType  string
	MetricType string
	Params     map[string]string
}

// MilvusPartitionMetadata is the normalized representation of a Milvus
// partition preserved in phase-one plans for later migration strategy selection.
type MilvusPartitionMetadata struct {
	Name string
}

// MilvusInspector turns Milvus collection metadata into a read-only inspection
// plan that can later drive schema planning and migration execution.
type MilvusInspector struct {
	client  MilvusMetadataClient
	options MilvusInspectorOptions
}

// NewMilvusInspector creates a metadata inspector with an injected client so
// behavior can be unit-tested without network access.
func NewMilvusInspector(client MilvusMetadataClient, options MilvusInspectorOptions) MilvusInspector {
	return MilvusInspector{client: client, options: options}
}

// Inspect reads Milvus metadata and returns a migration plan without mutating
// the source or target systems.
func (i MilvusInspector) Inspect(ctx context.Context) (MilvusInspectionPlan, error) {
	if err := ctx.Err(); err != nil {
		return MilvusInspectionPlan{}, err
	}
	if i.client == nil {
		return MilvusInspectionPlan{}, fmt.Errorf("milvus metadata client is required")
	}
	collections, err := i.collectionNames(ctx)
	if err != nil {
		return MilvusInspectionPlan{}, err
	}
	plans := make([]MilvusCollectionPlan, 0, len(collections))
	for _, collection := range collections {
		metadata, err := i.client.DescribeCollection(ctx, collection)
		if err != nil {
			return MilvusInspectionPlan{}, fmt.Errorf("describe milvus collection %q: %w", collection, err)
		}
		plans = append(plans, buildMilvusCollectionPlan(metadata))
	}
	plan := MilvusInspectionPlan{
		SchemaVersion: MilvusInspectionSchemaVersion,
		Source:        MilvusInspectionSource{Type: "milvus", Address: i.options.Address},
		Collections:   plans,
	}
	plan.Summary = BuildMilvusInspectionSummary(plan.Collections)
	return plan, nil
}

func (i MilvusInspector) collectionNames(ctx context.Context) ([]string, error) {
	if i.options.Collection != "" {
		return []string{i.options.Collection}, nil
	}
	collections, err := i.client.ListCollections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list milvus collections: %w", err)
	}
	sort.Strings(collections)
	return collections, nil
}

func buildMilvusCollectionPlan(metadata MilvusCollectionMetadata) MilvusCollectionPlan {
	fields := make([]MilvusFieldPlan, 0, len(metadata.Fields))
	warnings := make([]string, 0)
	for _, field := range metadata.Fields {
		mapped := MapMilvusFieldToPGVector(MilvusFieldPlan{
			Name:       field.Name,
			SourceType: field.DataType,
			Dimension:  field.Dimension,
			MaxLength:  field.MaxLength,
			PrimaryKey: field.PrimaryKey,
			Nullable:   field.Nullable,
		})
		fields = append(fields, mapped)
	}
	if metadata.DynamicFieldEnabled {
		warnings = append(warnings, "dynamic fields should be preserved in a jsonb metadata column during migration execution")
	}
	partitions := make([]MilvusPartitionPlan, 0, len(metadata.Partitions))
	for _, partition := range metadata.Partitions {
		partitions = append(partitions, MilvusPartitionPlan{
			Name:                partition.Name,
			SupportLevel:        SupportLevelDegraded,
			RecommendedStrategy: "metadata_column",
		})
		if partition.Name != "" && partition.Name != "_default" {
			warnings = append(warnings, fmt.Sprintf("partition %q is metadata-only in phase one", partition.Name))
		}
	}
	return MilvusCollectionPlan{
		Name:                metadata.Name,
		RowCount:            metadata.RowCount,
		Description:         metadata.Description,
		AutoID:              metadata.AutoID,
		DynamicFieldEnabled: metadata.DynamicFieldEnabled,
		PrimaryKey:          metadata.PrimaryKey,
		Fields:              fields,
		Indexes:             buildMilvusIndexPlans(metadata.Indexes),
		Partitions:          partitions,
		Warnings:            warnings,
	}
}

func buildMilvusIndexPlans(indexes []MilvusIndexMetadata) []MilvusIndexPlan {
	plans := make([]MilvusIndexPlan, 0, len(indexes))
	for _, index := range indexes {
		plan := MilvusIndexPlan{
			Field:           index.Field,
			SourceIndexType: index.IndexType,
			SourceMetric:    index.MetricType,
			Params:          cloneStringMap(index.Params),
			SupportLevel:    SupportLevelDegraded,
		}
		switch strings.ToUpper(index.IndexType) {
		case "FLAT":
			plan.TargetIndexType = "flat"
			plan.SupportLevel = SupportLevelSupported
		case "HNSW":
			plan.TargetIndexType = "hnsw"
			plan.TargetOps = pgvectorOpsForMilvusMetric(index.MetricType)
		case "IVF_FLAT", "IVFFLAT":
			plan.TargetIndexType = "ivfflat"
			plan.TargetOps = pgvectorOpsForMilvusMetric(index.MetricType)
		default:
			plan.SupportLevel = SupportLevelUnsupported
			plan.Warning = fmt.Sprintf("Milvus index type %q is metadata-only in phase one", index.IndexType)
		}
		if plan.TargetOps == "" && plan.Warning == "" && plan.TargetIndexType != "flat" {
			plan.Warning = fmt.Sprintf("Milvus metric %q needs manual pgvector operator review", index.MetricType)
		}
		plans = append(plans, plan)
	}
	return plans
}

func pgvectorOpsForMilvusMetric(metric string) string {
	switch strings.ToUpper(metric) {
	case "COSINE":
		return "vector_cosine_ops"
	case "L2":
		return "vector_l2_ops"
	case "IP":
		return "vector_ip_ops"
	default:
		return ""
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
