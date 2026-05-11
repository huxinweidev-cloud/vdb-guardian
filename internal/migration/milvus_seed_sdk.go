package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/fixtures"
	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

type milvusSeedSDKClient interface {
	HasCollection(ctx context.Context, collection string) (bool, error)
	DropCollection(ctx context.Context, collection string) error
	CreateCollection(ctx context.Context, req milvusSDKSeedCreateCollectionRequest) error
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
}

type milvusSDKSeedInsertRequest struct {
	Collection  string
	IDField     string
	VectorField string
	IDs         []string
	Vectors     [][]float32
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
	for index, record := range req.Records {
		ids[index] = record.ID
		vectors[index] = make([]float32, len(record.Vector))
		for dimension, value := range record.Vector {
			vectors[index][dimension] = float32(value)
		}
	}
	if err := db.client.Insert(ctx, milvusSDKSeedInsertRequest{
		Collection:  req.Collection,
		IDField:     req.IDField,
		VectorField: req.VectorField,
		IDs:         ids,
		Vectors:     vectors,
	}); err != nil {
		return err
	}
	return db.client.Flush(ctx, req.Collection)
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
	return c.client.CreateCollection(ctx, schema, 1)
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
	_, err := c.client.Insert(
		ctx,
		req.Collection,
		"",
		entity.NewColumnVarChar(req.IDField, req.IDs),
		entity.NewColumnFloatVector(req.VectorField, vectorDimension(req.Vectors), req.Vectors),
	)
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

func formatMilvusSeedSummary(result MilvusSeedResult) string {
	return fmt.Sprintf("collection=%s dimension=%d records_seeded=%d", result.Collection, result.Dimension, result.RecordsSeeded)
}
