package connectors

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	// MilvusMetricCosine selects cosine similarity search for Milvus collections.
	//
	// Milvus cosine scores are treated as already similarity-like, so the connector
	// passes them through as normalized SearchHit scores where larger is better.
	//
	// MilvusMetricCosine 为 Milvus 集合选择余弦相似度 (cosine similarity) 检索。
	// Milvus 返回的余弦得分本身就类似于相似度指标，因此连接器会直接将其透传
	// 为规范化的 SearchHit 得分，且得分越大代表匹配度越好。
	MilvusMetricCosine = "cosine"

	// MilvusMetricL2 selects Euclidean distance search for Milvus collections.
	//
	// Milvus L2 values are distances, so the connector normalizes them to negative
	// scores to preserve the project-wide convention that larger scores are better.
	//
	// MilvusMetricL2 为 Milvus 集合选择欧几里得距离 (Euclidean distance) 检索。
	// Milvus 返回的 L2 值是距离指标，因此连接器会将其归一化为负分，
	// 以保持项目全局统一的“得分越大越好”惯例。
	MilvusMetricL2 = "l2"

	// MilvusMetricIP selects inner-product search for Milvus collections.
	//
	// Inner-product scores are similarity-like and are passed through unchanged.
	//
	// MilvusMetricIP 为 Milvus 集合选择内积 (inner-product) 检索。
	// 内积得分同样类似于相似度指标，因此会被原样透传。
	MilvusMetricIP = "ip"
)

var milvusIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MilvusConfig configures the minimal Milvus connector used by the first
// Milvus-to-pgvector migration MVP.
//
// The first version intentionally supports one vector field, one ID field, one
// default collection, and simple identifier names. Collection creation, index
// management, load orchestration, and metadata filtering are handled by later
// migration/integration steps.
//
// MilvusConfig 用于配置首个 Milvus 到 pgvector 迁移 MVP 所需的极简 Milvus 连接器。
// 初版实现刻意限制了功能：仅支持单一的向量字段、单一的 ID 字段、一个默认集合以及
// 简单的标识符命名。至于集合的创建、索引管理、加载编排以及元数据过滤等复杂功能，
// 将由后续的迁移/集成步骤来处理。
type MilvusConfig struct {
	Name              string
	Address           string
	DefaultCollection string
	IDField           string
	VectorField       string
	Metric            string
}

// MilvusConnector implements normalized vector search against Milvus.
//
// The connector translates the generic SearchRequest contract into a small
// Milvus adapter request and returns normalized SearchResponse values for the
// fingerprint artifact builder. It keeps Milvus SDK details behind milvusDB so
// core search normalization can be tested without Docker or network state.
//
// MilvusConnector 针对 Milvus 实现了规范化的向量检索。
// 该连接器将通用的 SearchRequest 契约转换为精简的 Milvus 适配器请求，
// 并返回规范化的 SearchResponse 以供指纹产物构建器使用。它将 Milvus SDK
// 的细节隐藏在 milvusDB 接口之后，从而确保核心的检索规范化逻辑可以在
// 摆脱 Docker 和网络状态的情况下进行单元测试。
type MilvusConnector struct {
	config MilvusConfig
	db     milvusDB
}

type milvusDB interface {
	Connect(ctx context.Context) error
	Count(ctx context.Context, collection string) (int64, error)
	Search(ctx context.Context, req milvusSearchRequest) ([]milvusRawHit, error)
	Close() error
}

type milvusSearchRequest struct {
	Collection  string
	IDField     string
	VectorField string
	QueryVector []float64
	Limit       int
	Metric      string
	Params      map[string]string
}

type milvusRawHit struct {
	ID    string
	Score float64
}

type milvusSDKClient interface {
	Count(ctx context.Context, collection string) (map[string]string, error)
	Search(ctx context.Context, req milvusSDKSearchRequest) ([]milvusSDKSearchResult, error)
	Close(ctx context.Context) error
}

type milvusSDKSearchRequest struct {
	Collection  string
	IDField     string
	VectorField string
	QueryVector []float32
	Limit       int
	Metric      string
	Params      map[string]string
}

type milvusSDKSearchResult struct {
	IDs    []string
	Scores []float32
}

type milvusSDKClientFactory func(ctx context.Context, address string) (milvusSDKClient, error)

type milvusSDKDB struct {
	address string
	factory milvusSDKClientFactory
	client  milvusSDKClient
}

func newMilvusSDKDB(address string) *milvusSDKDB {
	return newMilvusSDKAdapterWithClientFactory(address, newRealMilvusSDKClient)
}

func newMilvusSDKAdapterWithClientFactory(address string, factory milvusSDKClientFactory) *milvusSDKDB {
	return &milvusSDKDB{address: address, factory: factory}
}

func newRealMilvusSDKClient(ctx context.Context, address string) (milvusSDKClient, error) {
	client, err := milvusclient.NewClient(ctx, milvusclient.Config{Address: address})
	if err != nil {
		return nil, err
	}
	return realMilvusSDKClient{client: client}, nil
}

func (db *milvusSDKDB) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db.address == "" {
		return errors.New("milvus address is required")
	}
	if db.factory == nil {
		db.factory = newRealMilvusSDKClient
	}
	client, err := db.factory(ctx, db.address)
	if err != nil {
		return err
	}
	db.client = client
	return nil
}

func (db *milvusSDKDB) Count(ctx context.Context, collection string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if db.client == nil {
		return 0, errors.New("milvus client is not connected")
	}
	stats, err := db.client.Count(ctx, collection)
	if err != nil {
		return 0, err
	}
	rowCount, ok := stats["row_count"]
	if !ok {
		return 0, errors.New("milvus stats missing row_count")
	}
	count, err := strconv.ParseInt(rowCount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse milvus row_count %q: %w", rowCount, err)
	}
	return count, nil
}

func (db *milvusSDKDB) Search(ctx context.Context, req milvusSearchRequest) ([]milvusRawHit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db.client == nil {
		return nil, errors.New("milvus client is not connected")
	}
	queryVector := make([]float32, len(req.QueryVector))
	for index, value := range req.QueryVector {
		queryVector[index] = float32(value)
	}
	results, err := db.client.Search(ctx, milvusSDKSearchRequest{
		Collection:  req.Collection,
		IDField:     req.IDField,
		VectorField: req.VectorField,
		QueryVector: queryVector,
		Limit:       req.Limit,
		Metric:      req.Metric,
		Params:      cloneStringMap(req.Params),
	})
	if err != nil {
		return nil, err
	}
	if len(results) != 1 {
		return nil, fmt.Errorf("expected one milvus result set, got %d", len(results))
	}
	result := results[0]
	if len(result.IDs) != len(result.Scores) {
		return nil, fmt.Errorf("milvus result ids length %d does not match scores length %d", len(result.IDs), len(result.Scores))
	}
	hits := make([]milvusRawHit, len(result.IDs))
	for index, id := range result.IDs {
		hits[index] = milvusRawHit{ID: id, Score: float64(result.Scores[index])}
	}
	return hits, nil
}

func (db *milvusSDKDB) Close() error {
	if db.client == nil {
		return nil
	}
	err := db.client.Close(context.Background())
	db.client = nil
	return err
}

type realMilvusSDKClient struct {
	client milvusclient.Client
}

func (c realMilvusSDKClient) Count(ctx context.Context, collection string) (map[string]string, error) {
	return c.client.GetCollectionStatistics(ctx, collection)
}

func (c realMilvusSDKClient) Search(ctx context.Context, req milvusSDKSearchRequest) ([]milvusSDKSearchResult, error) {
	searchParam, err := entity.NewIndexFlatSearchParam()
	if err != nil {
		return nil, err
	}
	resultSets, err := c.client.Search(
		ctx,
		req.Collection,
		nil,
		"",
		[]string{req.IDField},
		[]entity.Vector{entity.FloatVector(req.QueryVector)},
		req.VectorField,
		typeMilvusMetric(req.Metric),
		req.Limit,
		searchParam,
	)
	if err != nil {
		return nil, err
	}
	results := make([]milvusSDKSearchResult, len(resultSets))
	for resultIndex, resultSet := range resultSets {
		ids, err := stringIDsFromMilvusColumn(resultSet.Fields.GetColumn(req.IDField))
		if err != nil {
			return nil, err
		}
		results[resultIndex] = milvusSDKSearchResult{IDs: ids, Scores: append([]float32(nil), resultSet.Scores...)}
	}
	return results, nil
}

func (c realMilvusSDKClient) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.client.Close()
	return nil
}

func stringIDsFromMilvusColumn(idColumn entity.Column) ([]string, error) {
	if idColumn == nil {
		return nil, errors.New("milvus result missing id field")
	}
	ids := make([]string, idColumn.Len())
	for index := range ids {
		id, err := idColumn.GetAsString(index)
		if err != nil {
			return nil, fmt.Errorf("read milvus id at index %d: %w", index, err)
		}
		ids[index] = id
	}
	return ids, nil
}

func typeMilvusMetric(metric string) entity.MetricType {
	switch metric {
	case MilvusMetricL2:
		return entity.L2
	case MilvusMetricIP:
		return entity.IP
	default:
		return entity.COSINE
	}
}

// NewMilvusConnector validates configuration and returns a minimal Milvus
// connector.
//
// Tests can inject a milvusDB adapter. When no adapter is injected, Address is
// required and a placeholder SDK adapter is created so the public connector API is
// ready for the later real Milvus SDK integration step.
//
// NewMilvusConnector 校验配置并返回一个极简的 Milvus 连接器。
// 测试代码可以通过依赖注入的方式提供 milvusDB 适配器。当未注入适配器时，
// Address (地址) 字段为必填项，此时系统会创建一个默认的 SDK 适配器占位符，
// 从而确保公开的连接器 API 已经为后续集成真实的 Milvus SDK 做好了准备。
func NewMilvusConnector(config MilvusConfig, db milvusDB) (MilvusConnector, error) {
	config = applyMilvusDefaults(config)
	if err := validateMilvusConfig(config, db); err != nil {
		return MilvusConnector{}, err
	}
	if db == nil {
		db = newMilvusSDKDB(config.Address)
	}
	return MilvusConnector{config: config, db: db}, nil
}

// Name returns the stable connector name used in logs, configuration, and
// reports.
//
// Name 返回该连接器用于日志、配置及报告中的稳定名称。
func (c MilvusConnector) Name() string {
	return c.config.Name
}

// Connect initializes the Milvus adapter and verifies basic context/adapter
// reachability.
//
// It returns adapter errors with Milvus context so failures are diagnosable in
// future CLI and job reports.
//
// Connect 初始化 Milvus 适配器并验证基础的上下文/适配器连通性。
// 它会将适配器抛出的错误附加上 Milvus 的上下文信息后返回，以便在未来的
// CLI 和作业报告中能够对此类故障进行诊断。
func (c MilvusConnector) Connect(ctx context.Context) error {
	if c.db == nil {
		return errors.New("milvus adapter is not configured")
	}
	if err := c.db.Connect(ctx); err != nil {
		return fmt.Errorf("connect milvus: %w", err)
	}
	return nil
}

// Count returns the number of entities in a Milvus collection.
//
// If collection is empty, DefaultCollection is used. Only simple collection
// identifiers are accepted so invalid dynamic names are rejected before reaching
// the Milvus SDK.
//
// Count 返回指定 Milvus 集合中的实体数量。
// 如果 collection 集合名为空，则回退使用 DefaultCollection (默认集合)。
// 该方法仅接受简单的集合标识符，从而确保在将请求发送至 Milvus SDK 之前，
// 就能主动拦截并拒绝不合规的动态名称。
func (c MilvusConnector) Count(ctx context.Context, collection string) (int64, error) {
	if c.db == nil {
		return 0, errors.New("milvus adapter is not configured")
	}
	resolvedCollection, err := c.collectionForRequest(collection)
	if err != nil {
		return 0, err
	}
	count, err := c.db.Count(ctx, resolvedCollection)
	if err != nil {
		return 0, fmt.Errorf("count milvus collection: %w", err)
	}
	return count, nil
}

// Search executes a normalized vector search request against Milvus.
//
// ExpandK is used as the Milvus search limit so boundary candidates can be
// captured for retrieval behavior fingerprints. Cosine and IP scores are passed
// through; L2 distances are converted to negative scores so larger values remain
// better across all connectors.
//
// Search 对 Milvus 执行规范化的向量检索请求。
// 内部将 ExpandK 作为向 Milvus 发起检索的上限 (limit)，以便捕获边界候选者，
// 从而构建检索行为指纹。Cosine 和 IP 得分会被原样透传；而 L2 距离则被转换为
// 负分，借此确保在所有的连接器中，“值越大代表匹配度越好” 这一规则依然成立。
func (c MilvusConnector) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if c.db == nil {
		return SearchResponse{}, errors.New("milvus adapter is not configured")
	}
	if err := validateMilvusSearchRequest(req); err != nil {
		return SearchResponse{}, err
	}
	collection, err := c.collectionForRequest(req.Collection)
	if err != nil {
		return SearchResponse{}, err
	}
	rawHits, err := c.db.Search(ctx, milvusSearchRequest{
		Collection:  collection,
		IDField:     c.config.IDField,
		VectorField: c.config.VectorField,
		QueryVector: append([]float64(nil), req.QueryVector...),
		Limit:       req.ExpandK,
		Metric:      c.config.Metric,
		Params:      cloneStringMap(req.Params),
	})
	if err != nil {
		return SearchResponse{}, fmt.Errorf("milvus search: %w", err)
	}
	hits := make([]SearchHit, len(rawHits))
	for index, rawHit := range rawHits {
		hits[index] = SearchHit{
			ID:    rawHit.ID,
			Rank:  index + 1,
			Score: c.normalizeScore(rawHit.Score),
		}
	}
	return SearchResponse{Hits: hits}, nil
}

// Close releases the underlying Milvus adapter when one is configured.
//
// Close 方法在底层 Milvus 适配器已配置时，负责释放其占用的资源。
func (c MilvusConnector) Close() error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}

func applyMilvusDefaults(config MilvusConfig) MilvusConfig {
	if config.Name == "" {
		config.Name = "milvus"
	}
	if config.DefaultCollection == "" {
		config.DefaultCollection = "items"
	}
	if config.IDField == "" {
		config.IDField = "id"
	}
	if config.VectorField == "" {
		config.VectorField = "embedding"
	}
	if config.Metric == "" {
		config.Metric = MilvusMetricCosine
	}
	return config
}

func validateMilvusConfig(config MilvusConfig, db milvusDB) error {
	if config.Address == "" && db == nil {
		return errors.New("milvus address is required when no adapter is injected")
	}
	if err := validateMilvusIdentifier("default collection", config.DefaultCollection); err != nil {
		return err
	}
	if err := validateMilvusIdentifier("id field", config.IDField); err != nil {
		return err
	}
	if err := validateMilvusIdentifier("vector field", config.VectorField); err != nil {
		return err
	}
	if config.Metric != MilvusMetricCosine && config.Metric != MilvusMetricL2 && config.Metric != MilvusMetricIP {
		return fmt.Errorf("unsupported milvus metric %q", config.Metric)
	}
	return nil
}

func validateMilvusSearchRequest(req SearchRequest) error {
	if len(req.QueryVector) == 0 {
		return errors.New("milvus query vector is required")
	}
	if req.TopK <= 0 {
		return errors.New("milvus topK must be positive")
	}
	if req.ExpandK <= 0 {
		return errors.New("milvus expandK must be positive")
	}
	if req.ExpandK < req.TopK {
		return errors.New("milvus expandK must be greater than or equal to topK")
	}
	return nil
}

func (c MilvusConnector) collectionForRequest(collection string) (string, error) {
	resolvedCollection := collection
	if resolvedCollection == "" {
		resolvedCollection = c.config.DefaultCollection
	}
	if err := validateMilvusIdentifier("collection", resolvedCollection); err != nil {
		return "", err
	}
	return resolvedCollection, nil
}

func (c MilvusConnector) normalizeScore(score float64) float64 {
	if c.config.Metric == MilvusMetricL2 {
		return -score
	}
	return score
}

func validateMilvusIdentifier(label string, value string) error {
	if !milvusIdentifierPattern.MatchString(value) {
		return fmt.Errorf("invalid milvus %s identifier %q", label, value)
	}
	return nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
