package inspection

import (
	"context"
	"fmt"
	"strconv"

	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

type milvusSDKMetadataClient interface {
	ListCollections(ctx context.Context, opts ...milvusclient.ListCollectionOption) ([]*entity.Collection, error)
	DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error)
	GetCollectionStatistics(ctx context.Context, collName string) (map[string]string, error)
	ShowPartitions(ctx context.Context, collName string) ([]*entity.Partition, error)
	DescribeIndex(ctx context.Context, collName string, fieldName string, opts ...milvusclient.IndexOption) ([]entity.Index, error)
	Close() error
}

// RealMilvusMetadataClient adapts the Milvus Go SDK to the read-only metadata
// client interface used by MilvusInspector.
type RealMilvusMetadataClient struct {
	client milvusSDKMetadataClient
}

// NewRealMilvusMetadataClient connects to Milvus and returns a read-only
// metadata client for inspection planning.
func NewRealMilvusMetadataClient(ctx context.Context, address string) (*RealMilvusMetadataClient, error) {
	if address == "" {
		return nil, fmt.Errorf("milvus address is required")
	}
	client, err := milvusclient.NewClient(ctx, milvusclient.Config{Address: address})
	if err != nil {
		return nil, err
	}
	return &RealMilvusMetadataClient{client: client}, nil
}

// Close releases the underlying Milvus SDK connection.
func (c *RealMilvusMetadataClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// ListCollections returns collection names available in the connected Milvus
// database without loading schemas or records.
func (c *RealMilvusMetadataClient) ListCollections(ctx context.Context) ([]string, error) {
	collections, err := c.client.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(collections))
	for _, collection := range collections {
		names = append(names, collection.Name)
	}
	return names, nil
}

// DescribeCollection returns normalized metadata for one Milvus collection.
func (c *RealMilvusMetadataClient) DescribeCollection(ctx context.Context, collection string) (MilvusCollectionMetadata, error) {
	description, err := c.client.DescribeCollection(ctx, collection)
	if err != nil {
		return MilvusCollectionMetadata{}, err
	}
	metadata := MilvusCollectionMetadata{Name: collection}
	if description.Schema != nil {
		metadata.Name = description.Schema.CollectionName
		metadata.Description = description.Schema.Description
		metadata.AutoID = description.Schema.AutoID
		metadata.DynamicFieldEnabled = description.Schema.EnableDynamicField
		metadata.PrimaryKey = description.Schema.PKFieldName()
		metadata.Fields = fieldsFromMilvusSchema(description.Schema)
	}
	rowCount, err := c.rowCount(ctx, collection)
	if err != nil {
		return MilvusCollectionMetadata{}, err
	}
	metadata.RowCount = rowCount
	metadata.Partitions, err = c.partitions(ctx, collection)
	if err != nil {
		return MilvusCollectionMetadata{}, err
	}
	metadata.Indexes, err = c.indexes(ctx, collection, metadata.Fields)
	if err != nil {
		return MilvusCollectionMetadata{}, err
	}
	return metadata, nil
}

func (c *RealMilvusMetadataClient) rowCount(ctx context.Context, collection string) (int64, error) {
	stats, err := c.client.GetCollectionStatistics(ctx, collection)
	if err != nil {
		return 0, err
	}
	value := stats["row_count"]
	if value == "" {
		return 0, nil
	}
	count, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse milvus row_count %q: %w", value, err)
	}
	return count, nil
}

func (c *RealMilvusMetadataClient) partitions(ctx context.Context, collection string) ([]MilvusPartitionMetadata, error) {
	partitions, err := c.client.ShowPartitions(ctx, collection)
	if err != nil {
		return nil, err
	}
	metadata := make([]MilvusPartitionMetadata, 0, len(partitions))
	for _, partition := range partitions {
		metadata = append(metadata, MilvusPartitionMetadata{Name: partition.Name})
	}
	return metadata, nil
}

func (c *RealMilvusMetadataClient) indexes(ctx context.Context, collection string, fields []MilvusFieldMetadata) ([]MilvusIndexMetadata, error) {
	indexes := make([]MilvusIndexMetadata, 0)
	for _, field := range fields {
		if !isVectorMilvusType(field.DataType) {
			continue
		}
		fieldIndexes, err := c.client.DescribeIndex(ctx, collection, field.Name)
		if err != nil {
			return nil, err
		}
		for _, index := range fieldIndexes {
			params := index.Params()
			indexes = append(indexes, MilvusIndexMetadata{
				Field:      field.Name,
				IndexType:  firstNonEmpty(string(index.IndexType()), params["index_type"]),
				MetricType: params["metric_type"],
				Params:     params,
			})
		}
	}
	return indexes, nil
}

func fieldsFromMilvusSchema(schema *entity.Schema) []MilvusFieldMetadata {
	fields := make([]MilvusFieldMetadata, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		fields = append(fields, MilvusFieldMetadata{
			Name:       field.Name,
			DataType:   dataTypeName(field.DataType),
			Dimension:  parseIntParam(field.TypeParams, "dim"),
			MaxLength:  parseIntParam(field.TypeParams, "max_length"),
			PrimaryKey: field.PrimaryKey,
			Nullable:   false,
		})
	}
	return fields
}

func dataTypeName(dataType entity.FieldType) string {
	if dataType == entity.FieldTypeSparseVector {
		return MilvusDataTypeSparseFloatVector
	}
	return dataType.Name()
}

func isVectorMilvusType(dataType string) bool {
	switch dataType {
	case MilvusDataTypeFloatVector, MilvusDataTypeBinaryVector, MilvusDataTypeSparseFloatVector:
		return true
	default:
		return false
	}
}

func parseIntParam(params map[string]string, key string) int {
	if params == nil || params[key] == "" {
		return 0
	}
	value, err := strconv.Atoi(params[key])
	if err != nil {
		return 0
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
