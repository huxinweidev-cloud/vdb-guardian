package connectors

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

const (
	// MilvusMetricCosine selects cosine similarity search for Milvus collections.
	//
	// Milvus cosine scores are treated as already similarity-like, so the connector
	// passes them through as normalized SearchHit scores where larger is better.
	MilvusMetricCosine = "cosine"

	// MilvusMetricL2 selects Euclidean distance search for Milvus collections.
	//
	// Milvus L2 values are distances, so the connector normalizes them to negative
	// scores to preserve the project-wide convention that larger scores are better.
	MilvusMetricL2 = "l2"

	// MilvusMetricIP selects inner-product search for Milvus collections.
	//
	// Inner-product scores are similarity-like and are passed through unchanged.
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

type milvusSDKDB struct {
	address string
}

func newMilvusSDKDB(address string) *milvusSDKDB {
	return &milvusSDKDB{address: address}
}

func (db *milvusSDKDB) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db.address == "" {
		return errors.New("milvus address is required")
	}
	return nil
}

func (db *milvusSDKDB) Count(ctx context.Context, collection string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("real Milvus count adapter is not implemented yet")
}

func (db *milvusSDKDB) Search(ctx context.Context, req milvusSearchRequest) ([]milvusRawHit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("real Milvus search adapter is not implemented yet")
}

func (db *milvusSDKDB) Close() error {
	return nil
}

// NewMilvusConnector validates configuration and returns a minimal Milvus
// connector.
//
// Tests can inject a milvusDB adapter. When no adapter is injected, Address is
// required and a placeholder SDK adapter is created so the public connector API is
// ready for the later real Milvus SDK integration step.
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
func (c MilvusConnector) Name() string {
	return c.config.Name
}

// Connect initializes the Milvus adapter and verifies basic context/adapter
// reachability.
//
// It returns adapter errors with Milvus context so failures are diagnosable in
// future CLI and job reports.
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
