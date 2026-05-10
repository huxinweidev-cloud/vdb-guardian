package migration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/fixtures"
)

const maxMilvusSeedDimension = 2000

var milvusSeedIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MilvusSeederConfig controls how synthetic fixture records are prepared for a
// Milvus collection.
//
// The first migration MVP deliberately supports one collection, one text primary
// key field, one dense vector field, and a single vector dimension. Collection
// indexing, loading, partitions, and metadata fields belong in later integration
// steps.
type MilvusSeederConfig struct {
	Collection  string
	IDField     string
	VectorField string
	Dimension   int
	Metric      string
}

// MilvusSeedResult summarizes a synthetic fixture seeding run for Milvus.
//
// The result is intended for future CLI/job reporting so users can confirm the
// target collection, vector dimension, and number of inserted records.
type MilvusSeedResult struct {
	Collection    string
	Dimension     int
	RecordsTotal  int
	RecordsSeeded int
}

// MilvusSeeder creates a minimal Milvus collection boundary and inserts
// synthetic fixture records.
//
// It owns write-side source database preparation for migration tests. Search and
// retrieval behavior remain in the Milvus connector so seeding and querying stay
// separated.
type MilvusSeeder struct {
	config MilvusSeederConfig
	db     milvusSeedDB
}

type milvusSeedDB interface {
	CreateCollection(ctx context.Context, req milvusCreateCollectionRequest) error
	InsertRecords(ctx context.Context, req milvusInsertRecordsRequest) error
}

type milvusCreateCollectionRequest struct {
	Collection  string
	IDField     string
	VectorField string
	Dimension   int
	Metric      string
}

type milvusInsertRecordsRequest struct {
	Collection  string
	IDField     string
	VectorField string
	Records     []milvusSeedRecord
}

type milvusSeedRecord struct {
	ID     string
	Vector []float64
}

// NewMilvusSeeder validates configuration and returns a seeder for synthetic
// Milvus fixture data.
//
// A database adapter is required because seeding performs write-side effects.
// Unit tests can inject a fake adapter; a real Milvus SDK adapter can be added in
// a later integration step without changing the seeder behavior contract.
func NewMilvusSeeder(config MilvusSeederConfig, db milvusSeedDB) (MilvusSeeder, error) {
	config = applyMilvusSeederDefaults(config)
	if err := validateMilvusSeederConfig(config, db); err != nil {
		return MilvusSeeder{}, err
	}
	return MilvusSeeder{config: config, db: db}, nil
}

// Seed creates the Milvus collection boundary and inserts all synthetic records.
//
// It validates that the fixture dimension and every record vector match the
// configured vector field dimension before any write request reaches the adapter.
func (s MilvusSeeder) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (MilvusSeedResult, error) {
	if err := validateMilvusSeedDataset(s.config, dataset); err != nil {
		return MilvusSeedResult{}, err
	}
	if err := s.db.CreateCollection(ctx, milvusCreateCollectionRequest{
		Collection:  s.config.Collection,
		IDField:     s.config.IDField,
		VectorField: s.config.VectorField,
		Dimension:   s.config.Dimension,
		Metric:      s.config.Metric,
	}); err != nil {
		return MilvusSeedResult{}, fmt.Errorf("create milvus collection: %w", err)
	}
	records := make([]milvusSeedRecord, len(dataset.Records))
	for index, record := range dataset.Records {
		records[index] = milvusSeedRecord{
			ID:     record.ID,
			Vector: append([]float64(nil), record.Vector...),
		}
	}
	if err := s.db.InsertRecords(ctx, milvusInsertRecordsRequest{
		Collection:  s.config.Collection,
		IDField:     s.config.IDField,
		VectorField: s.config.VectorField,
		Records:     records,
	}); err != nil {
		return MilvusSeedResult{}, fmt.Errorf("insert milvus records: %w", err)
	}
	return MilvusSeedResult{
		Collection:    s.config.Collection,
		Dimension:     s.config.Dimension,
		RecordsTotal:  len(dataset.Records),
		RecordsSeeded: len(dataset.Records),
	}, nil
}

func applyMilvusSeederDefaults(config MilvusSeederConfig) MilvusSeederConfig {
	if config.Collection == "" {
		config.Collection = "items"
	}
	if config.IDField == "" {
		config.IDField = "id"
	}
	if config.VectorField == "" {
		config.VectorField = "embedding"
	}
	if config.Metric == "" {
		config.Metric = fixtures.MetricCosine
	}
	return config
}

func validateMilvusSeederConfig(config MilvusSeederConfig, db milvusSeedDB) error {
	if db == nil {
		return errors.New("milvus seed database adapter is required")
	}
	if config.Dimension <= 0 || config.Dimension > maxMilvusSeedDimension {
		return fmt.Errorf("milvus seed dimension must be in range 1..%d", maxMilvusSeedDimension)
	}
	if err := validateMilvusSeedIdentifier("collection", config.Collection); err != nil {
		return err
	}
	if err := validateMilvusSeedIdentifier("id field", config.IDField); err != nil {
		return err
	}
	if err := validateMilvusSeedIdentifier("vector field", config.VectorField); err != nil {
		return err
	}
	if config.Metric != fixtures.MetricCosine && config.Metric != fixtures.MetricL2 {
		return fmt.Errorf("unsupported milvus seed metric %q", config.Metric)
	}
	return nil
}

func validateMilvusSeedDataset(config MilvusSeederConfig, dataset fixtures.SyntheticDataset) error {
	if dataset.Dimension != config.Dimension {
		return fmt.Errorf("synthetic dataset dimension %d does not match milvus seed dimension %d", dataset.Dimension, config.Dimension)
	}
	for index, record := range dataset.Records {
		if record.ID == "" {
			return fmt.Errorf("synthetic record at index %d has empty id", index)
		}
		if len(record.Vector) != config.Dimension {
			return fmt.Errorf("synthetic record %q vector dimension %d does not match milvus seed dimension %d", record.ID, len(record.Vector), config.Dimension)
		}
		for vectorIndex, value := range record.Vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("synthetic record %q vector contains non-finite value at index %d", record.ID, vectorIndex)
			}
		}
	}
	return nil
}

func validateMilvusSeedIdentifier(label string, value string) error {
	if !milvusSeedIdentifierPattern.MatchString(value) {
		return fmt.Errorf("invalid milvus seed %s identifier %q", label, value)
	}
	return nil
}
