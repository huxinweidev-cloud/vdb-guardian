package migration

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/h3xwave/vdb-guardian/internal/fixtures"
	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	milvusSeedDynamicMetadataField   = "_milvus_dynamic"
	milvusSeedPartitionMetadataField = "_milvus_partition"
)

type milvusSeedSDKClient interface {
	HasCollection(ctx context.Context, collection string) (bool, error)
	DropCollection(ctx context.Context, collection string) error
	CreateCollection(ctx context.Context, req milvusSDKSeedCreateCollectionRequest) error
	CreatePartition(ctx context.Context, collection string, partition string) error
	CreateIndex(ctx context.Context, collection string, vectorField string, metric string) error
	LoadCollection(ctx context.Context, collection string) error
	Insert(ctx context.Context, req milvusSDKSeedInsertRequest) error
	Flush(ctx context.Context, collection string) error
	Close(ctx context.Context) error
}

type milvusSDKSeedCreateCollectionRequest struct {
	Collection  string
	IDField     string
	VectorField string
	Dimension   int
	Metric      string
	Complex     bool
}

type milvusSDKSeedInsertRequest struct {
	Collection  string
	IDField     string
	VectorField string
	IDs         []string
	Vectors     [][]float32
	Titles      []string
	Prices      []float64
	Quantities  []int64
	Actives     []bool
	Categories  []string
	Metadata    [][]byte
	Partition   string
	Complex     bool
}

type milvusSeedSDKClientFactory func(ctx context.Context, address string) (milvusSeedSDKClient, error)

type milvusSDKSeedDB struct {
	address string
	factory milvusSeedSDKClientFactory
	client  milvusSeedSDKClient
}

// NewMilvusSDKSeedDB returns a real Milvus SDK-backed seed database adapter.
//
// The adapter drops and recreates the configured collection before inserting a
// deterministic synthetic fixture batch. It is intended for local migration MVP
// smoke checks, not production data loading.
//
// NewMilvusSDKSeedDB 返回一个由真实 Milvus SDK 支持的数据灌入数据库适配器。
// 在插入确定性的合成测试固件批次之前，该适配器会先删除并重新创建已配置的目标集合。
// 它专为本地迁移 MVP 的冒烟测试而设计，切勿用于生产环境的数据导入。
func NewMilvusSDKSeedDB(address string) *milvusSDKSeedDB {
	return newMilvusSDKSeedDBWithClientFactory(address, newRealMilvusSeedSDKClient)
}

func newMilvusSDKSeedDBWithClientFactory(address string, factory milvusSeedSDKClientFactory) *milvusSDKSeedDB {
	return &milvusSDKSeedDB{address: address, factory: factory}
}

func newRealMilvusSeedSDKClient(ctx context.Context, address string) (milvusSeedSDKClient, error) {
	client, err := milvusclient.NewClient(ctx, milvusclient.Config{Address: address})
	if err != nil {
		return nil, err
	}
	return realMilvusSeedSDKClient{client: client}, nil
}

// Connect opens the underlying Milvus SDK connection.
//
// Connect 开启底层的 Milvus SDK 连接。
func (db *milvusSDKSeedDB) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db.address == "" {
		return errors.New("milvus address is required")
	}
	if db.factory == nil {
		db.factory = newRealMilvusSeedSDKClient
	}
	client, err := db.factory(ctx, db.address)
	if err != nil {
		return err
	}
	db.client = client
	return nil
}

func (db *milvusSDKSeedDB) CreateCollection(ctx context.Context, req milvusCreateCollectionRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db.client == nil {
		return errors.New("milvus seed client is not connected")
	}
	exists, err := db.client.HasCollection(ctx, req.Collection)
	if err != nil {
		return err
	}
	if exists {
		if err := db.client.DropCollection(ctx, req.Collection); err != nil {
			return err
		}
	}
	createReq := milvusSDKSeedCreateCollectionRequest{
		Collection:  req.Collection,
		IDField:     req.IDField,
		VectorField: req.VectorField,
		Dimension:   req.Dimension,
		Metric:      req.Metric,
		Complex:     req.Complex,
	}
	if err := db.client.CreateCollection(ctx, createReq); err != nil {
		return err
	}
	if err := db.client.CreateIndex(ctx, req.Collection, req.VectorField, req.Metric); err != nil {
		return err
	}
	if err := db.client.LoadCollection(ctx, req.Collection); err != nil {
		return err
	}
	return nil
}

func (db *milvusSDKSeedDB) InsertRecords(ctx context.Context, req milvusInsertRecordsRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db.client == nil {
		return errors.New("milvus seed client is not connected")
	}
	ids := make([]string, len(req.Records))
	vectors := make([][]float32, len(req.Records))
	sdkReq := milvusSDKSeedInsertRequest{
		Collection:  req.Collection,
		IDField:     req.IDField,
		VectorField: req.VectorField,
		IDs:         ids,
		Vectors:     vectors,
		Complex:     milvusSeedRecordsHaveComplexFields(req.Records),
	}
	if sdkReq.Complex {
		sdkReq.Titles = make([]string, len(req.Records))
		sdkReq.Prices = make([]float64, len(req.Records))
		sdkReq.Quantities = make([]int64, len(req.Records))
		sdkReq.Actives = make([]bool, len(req.Records))
		sdkReq.Categories = make([]string, len(req.Records))
		sdkReq.Metadata = make([][]byte, len(req.Records))
	}
	partitions := make(map[string][]int)
	partitionedIndexes := make(map[int]struct{})
	for index, record := range req.Records {
		ids[index] = record.ID
		vectors[index] = make([]float32, len(record.Vector))
		for dimension, value := range record.Vector {
			vectors[index][dimension] = float32(value)
		}
		if sdkReq.Complex {
			sdkReq.Titles[index] = stringValue(record.Title)
			sdkReq.Prices[index] = float64Value(record.Price)
			sdkReq.Quantities[index] = int64Value(record.Quantity)
			sdkReq.Actives[index] = boolValue(record.Active)
			sdkReq.Categories[index] = stringValue(record.Category)
			metadata, err := marshalMilvusSeedMetadata(record)
			if err != nil {
				return err
			}
			sdkReq.Metadata[index] = metadata
			if record.Partition != nil && *record.Partition != "" {
				partitions[*record.Partition] = append(partitions[*record.Partition], index)
				partitionedIndexes[index] = struct{}{}
			}
		}
	}
	if len(partitions) == 0 {
		if err := db.client.Insert(ctx, sdkReq); err != nil {
			return err
		}
		return db.client.Flush(ctx, req.Collection)
	}
	unpartitionedIndexes := make([]int, 0, len(req.Records)-len(partitionedIndexes))
	for index := range req.Records {
		if _, ok := partitionedIndexes[index]; !ok {
			unpartitionedIndexes = append(unpartitionedIndexes, index)
		}
	}
	if len(unpartitionedIndexes) > 0 {
		if err := db.client.Insert(ctx, subsetMilvusSDKSeedInsertRequest(sdkReq, unpartitionedIndexes)); err != nil {
			return err
		}
	}
	for _, partition := range sortedMilvusSeedPartitionNames(partitions) {
		indexes := partitions[partition]
		if err := db.client.CreatePartition(ctx, req.Collection, partition); err != nil {
			return err
		}
		partitionReq := subsetMilvusSDKSeedInsertRequest(sdkReq, indexes)
		partitionReq.Partition = partition
		if err := db.client.Insert(ctx, partitionReq); err != nil {
			return err
		}
	}
	return db.client.Flush(ctx, req.Collection)
}

func sortedMilvusSeedPartitionNames(partitions map[string][]int) []string {
	names := make([]string, 0, len(partitions))
	for partition := range partitions {
		names = append(names, partition)
	}
	sort.Strings(names)
	return names
}

func subsetMilvusSDKSeedInsertRequest(req milvusSDKSeedInsertRequest, indexes []int) milvusSDKSeedInsertRequest {
	subset := milvusSDKSeedInsertRequest{
		Collection:  req.Collection,
		IDField:     req.IDField,
		VectorField: req.VectorField,
		IDs:         make([]string, len(indexes)),
		Vectors:     make([][]float32, len(indexes)),
		Complex:     req.Complex,
	}
	if req.Complex {
		subset.Titles = make([]string, len(indexes))
		subset.Prices = make([]float64, len(indexes))
		subset.Quantities = make([]int64, len(indexes))
		subset.Actives = make([]bool, len(indexes))
		subset.Categories = make([]string, len(indexes))
		subset.Metadata = make([][]byte, len(indexes))
	}
	for outIndex, sourceIndex := range indexes {
		subset.IDs[outIndex] = req.IDs[sourceIndex]
		subset.Vectors[outIndex] = req.Vectors[sourceIndex]
		if req.Complex {
			subset.Titles[outIndex] = req.Titles[sourceIndex]
			subset.Prices[outIndex] = req.Prices[sourceIndex]
			subset.Quantities[outIndex] = req.Quantities[sourceIndex]
			subset.Actives[outIndex] = req.Actives[sourceIndex]
			subset.Categories[outIndex] = req.Categories[sourceIndex]
			subset.Metadata[outIndex] = req.Metadata[sourceIndex]
		}
	}
	return subset
}

func milvusSeedRecordsHaveComplexFields(records []milvusSeedRecord) bool {
	for _, record := range records {
		if record.Title != nil || record.Price != nil || record.Quantity != nil || record.Active != nil || record.Category != nil || len(record.DynamicMetadata) > 0 || record.Partition != nil {
			return true
		}
	}
	return false
}

func marshalMilvusSeedMetadata(record milvusSeedRecord) ([]byte, error) {
	metadata := copyMilvusSeedDynamicMetadata(record.DynamicMetadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	if record.Partition != nil {
		metadata[milvusSeedPartitionMetadataField] = *record.Partition
	}
	return json.Marshal(metadata)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func float64Value(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func boolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

// Close releases the underlying Milvus SDK connection.
//
// Close 释放底层的 Milvus SDK 连接。
func (db *milvusSDKSeedDB) Close() error {
	if db.client == nil {
		return nil
	}
	err := db.client.Close(context.Background())
	db.client = nil
	return err
}

type realMilvusSeedSDKClient struct {
	client milvusclient.Client
}

func (c realMilvusSeedSDKClient) HasCollection(ctx context.Context, collection string) (bool, error) {
	return c.client.HasCollection(ctx, collection)
}

func (c realMilvusSeedSDKClient) DropCollection(ctx context.Context, collection string) error {
	return c.client.DropCollection(ctx, collection)
}

func (c realMilvusSeedSDKClient) CreateCollection(ctx context.Context, req milvusSDKSeedCreateCollectionRequest) error {
	schema := entity.NewSchema().
		WithName(req.Collection).
		WithDescription("vdb-guardian synthetic migration fixture").
		WithAutoID(false).
		WithField(entity.NewField().
			WithName(req.IDField).
			WithDataType(entity.FieldTypeVarChar).
			WithIsPrimaryKey(true).
			WithIsAutoID(false).
			WithMaxLength(256)).
		WithField(entity.NewField().
			WithName(req.VectorField).
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(req.Dimension)))
	if req.Complex {
		schema.WithDynamicFieldEnabled(true).
			WithField(entity.NewField().WithName("title").WithDataType(entity.FieldTypeVarChar).WithMaxLength(512)).
			WithField(entity.NewField().WithName("price").WithDataType(entity.FieldTypeDouble)).
			WithField(entity.NewField().WithName("quantity").WithDataType(entity.FieldTypeInt64)).
			WithField(entity.NewField().WithName("active").WithDataType(entity.FieldTypeBool)).
			WithField(entity.NewField().WithName("category").WithDataType(entity.FieldTypeVarChar).WithMaxLength(256)).
			WithField(entity.NewField().WithName(milvusSeedDynamicMetadataField).WithDataType(entity.FieldTypeJSON))
	}
	return c.client.CreateCollection(ctx, schema, 1)
}

func (c realMilvusSeedSDKClient) CreatePartition(ctx context.Context, collection string, partition string) error {
	return c.client.CreatePartition(ctx, collection, partition)
}

func (c realMilvusSeedSDKClient) CreateIndex(ctx context.Context, collection string, vectorField string, metric string) error {
	index, err := entity.NewIndexFlat(typeMilvusSeedMetric(metric))
	if err != nil {
		return err
	}
	return c.client.CreateIndex(ctx, collection, vectorField, index, false)
}

func (c realMilvusSeedSDKClient) LoadCollection(ctx context.Context, collection string) error {
	return c.client.LoadCollection(ctx, collection, false)
}

func (c realMilvusSeedSDKClient) Insert(ctx context.Context, req milvusSDKSeedInsertRequest) error {
	columns := []entity.Column{
		entity.NewColumnVarChar(req.IDField, req.IDs),
		entity.NewColumnFloatVector(req.VectorField, vectorDimension(req.Vectors), req.Vectors),
	}
	if req.Complex {
		columns = append(columns,
			entity.NewColumnVarChar("title", req.Titles),
			entity.NewColumnDouble("price", req.Prices),
			entity.NewColumnInt64("quantity", req.Quantities),
			entity.NewColumnBool("active", req.Actives),
			entity.NewColumnVarChar("category", req.Categories),
			entity.NewColumnJSONBytes(milvusSeedDynamicMetadataField, req.Metadata),
		)
	}
	_, err := c.client.Insert(ctx, req.Collection, req.Partition, columns...)
	return err
}

func (c realMilvusSeedSDKClient) Flush(ctx context.Context, collection string) error {
	return c.client.Flush(ctx, collection, false)
}

func (c realMilvusSeedSDKClient) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.client.Close()
}

func typeMilvusSeedMetric(metric string) entity.MetricType {
	switch metric {
	case fixtures.MetricL2:
		return entity.L2
	default:
		return entity.COSINE
	}
}

func vectorDimension(vectors [][]float32) int {
	if len(vectors) == 0 {
		return 0
	}
	return len(vectors[0])
}
