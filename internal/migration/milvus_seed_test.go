package migration

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/fixtures"
)

func TestNewMilvusSeederAppliesDefaults(t *testing.T) {
	seeder, err := NewMilvusSeeder(MilvusSeederConfig{Dimension: 3}, &fakeMilvusSeedDB{})
	if err != nil {
		t.Fatalf("NewMilvusSeeder returned error: %v", err)
	}

	if seeder.config.Collection != "items" {
		t.Fatalf("unexpected collection: %s", seeder.config.Collection)
	}
	if seeder.config.IDField != "id" {
		t.Fatalf("unexpected id field: %s", seeder.config.IDField)
	}
	if seeder.config.VectorField != "embedding" {
		t.Fatalf("unexpected vector field: %s", seeder.config.VectorField)
	}
	if seeder.config.Dimension != 3 {
		t.Fatalf("unexpected dimension: %d", seeder.config.Dimension)
	}
	if seeder.config.Metric != fixtures.MetricCosine {
		t.Fatalf("unexpected metric: %s", seeder.config.Metric)
	}
}

func TestNewMilvusSeederRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config MilvusSeederConfig
		db     milvusSeedDB
	}{
		{name: "missing db", config: MilvusSeederConfig{Dimension: 3}},
		{name: "zero dimension", config: MilvusSeederConfig{}, db: &fakeMilvusSeedDB{}},
		{name: "dimension too large", config: MilvusSeederConfig{Dimension: 2001}, db: &fakeMilvusSeedDB{}},
		{name: "unsafe collection", config: MilvusSeederConfig{Collection: "items;drop", Dimension: 3}, db: &fakeMilvusSeedDB{}},
		{name: "unsafe id field", config: MilvusSeederConfig{IDField: "id-name", Dimension: 3}, db: &fakeMilvusSeedDB{}},
		{name: "unsafe vector field", config: MilvusSeederConfig{VectorField: "public.embedding", Dimension: 3}, db: &fakeMilvusSeedDB{}},
		{name: "unsupported metric", config: MilvusSeederConfig{Dimension: 3, Metric: "hamming"}, db: &fakeMilvusSeedDB{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewMilvusSeeder(tt.config, tt.db)
			if err == nil {
				t.Fatalf("expected invalid config to fail")
			}
		})
	}
}

func TestMilvusSeederSeedCreatesCollectionAndInsertsRecords(t *testing.T) {
	db := &fakeMilvusSeedDB{}
	seeder := mustMilvusSeeder(t, db, MilvusSeederConfig{Dimension: 3})

	result, err := seeder.Seed(context.Background(), syntheticMilvusSeedDataset())
	if err != nil {
		t.Fatalf("Seed returned error: %v", err)
	}

	if result.Collection != "items" {
		t.Fatalf("unexpected collection: %s", result.Collection)
	}
	if result.Dimension != 3 {
		t.Fatalf("unexpected dimension: %d", result.Dimension)
	}
	if result.RecordsTotal != 2 || result.RecordsSeeded != 2 {
		t.Fatalf("unexpected result counts: %#v", result)
	}
	if len(db.createCalls) != 1 {
		t.Fatalf("expected one create collection call, got %d", len(db.createCalls))
	}
	create := db.createCalls[0]
	if create.Collection != "items" || create.IDField != "id" || create.VectorField != "embedding" {
		t.Fatalf("unexpected create request: %#v", create)
	}
	if create.Dimension != 3 || create.Metric != fixtures.MetricCosine {
		t.Fatalf("unexpected create dimension/metric: %#v", create)
	}
	if len(db.insertCalls) != 1 {
		t.Fatalf("expected one insert batch, got %d", len(db.insertCalls))
	}
	insert := db.insertCalls[0]
	if insert.Collection != "items" {
		t.Fatalf("unexpected insert collection: %s", insert.Collection)
	}
	if len(insert.Records) != 2 {
		t.Fatalf("expected two records, got %d", len(insert.Records))
	}
	if insert.Records[0].ID != "vec-000001" {
		t.Fatalf("unexpected first record id: %s", insert.Records[0].ID)
	}
	expectedVector := []float64{0.1, 0.2, 0.3}
	if !reflect.DeepEqual(insert.Records[0].Vector, expectedVector) {
		t.Fatalf("unexpected first record vector: %#v", insert.Records[0].Vector)
	}
}

func TestMilvusSeederSeedRejectsInvalidDataset(t *testing.T) {
	seeder := mustMilvusSeeder(t, &fakeMilvusSeedDB{}, MilvusSeederConfig{Dimension: 3})
	tests := []struct {
		name    string
		dataset fixtures.SyntheticDataset
	}{
		{
			name: "dimension mismatch",
			dataset: fixtures.SyntheticDataset{
				Dimension: 2,
				Records:   []fixtures.SyntheticVector{{ID: "vec-1", Vector: []float64{0.1, 0.2}}},
			},
		},
		{
			name: "empty record id",
			dataset: fixtures.SyntheticDataset{
				Dimension: 3,
				Records:   []fixtures.SyntheticVector{{Vector: []float64{0.1, 0.2, 0.3}}},
			},
		},
		{
			name: "record vector mismatch",
			dataset: fixtures.SyntheticDataset{
				Dimension: 3,
				Records:   []fixtures.SyntheticVector{{ID: "vec-1", Vector: []float64{0.1, 0.2}}},
			},
		},
		{
			name: "non finite vector value",
			dataset: fixtures.SyntheticDataset{
				Dimension: 3,
				Records:   []fixtures.SyntheticVector{{ID: "vec-1", Vector: []float64{0.1, 0.2, infForMilvusSeed()}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := seeder.Seed(context.Background(), tt.dataset)
			if err == nil {
				t.Fatalf("expected invalid dataset to fail")
			}
		})
	}
}

func TestMilvusSeederSeedPropagatesContextCancellation(t *testing.T) {
	seeder := mustMilvusSeeder(t, &fakeMilvusSeedDB{}, MilvusSeederConfig{Dimension: 3})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := seeder.Seed(ctx, syntheticMilvusSeedDataset())
	if err == nil {
		t.Fatalf("expected canceled context to fail")
	}
}

func TestMilvusSeederSeedAddsContextToCreateErrors(t *testing.T) {
	db := &fakeMilvusSeedDB{createErr: errMilvusSeedFake}
	seeder := mustMilvusSeeder(t, db, MilvusSeederConfig{Dimension: 3})

	_, err := seeder.Seed(context.Background(), syntheticMilvusSeedDataset())
	if !errors.Is(err, errMilvusSeedFake) {
		t.Fatalf("expected wrapped fake error, got %v", err)
	}
	if err.Error() == errMilvusSeedFake.Error() {
		t.Fatalf("expected contextual error, got %v", err)
	}
}

func TestMilvusSeederSeedAddsContextToInsertErrors(t *testing.T) {
	db := &fakeMilvusSeedDB{insertErr: errMilvusSeedFake}
	seeder := mustMilvusSeeder(t, db, MilvusSeederConfig{Dimension: 3})

	_, err := seeder.Seed(context.Background(), syntheticMilvusSeedDataset())
	if !errors.Is(err, errMilvusSeedFake) {
		t.Fatalf("expected wrapped fake error, got %v", err)
	}
	if err.Error() == errMilvusSeedFake.Error() {
		t.Fatalf("expected contextual error, got %v", err)
	}
}

func TestMilvusSeederSeedCopiesRecordVectors(t *testing.T) {
	db := &fakeMilvusSeedDB{}
	seeder := mustMilvusSeeder(t, db, MilvusSeederConfig{Dimension: 3})
	dataset := syntheticMilvusSeedDataset()

	_, err := seeder.Seed(context.Background(), dataset)
	if err != nil {
		t.Fatalf("Seed returned error: %v", err)
	}
	dataset.Records[0].Vector[0] = 99
	if got := db.insertCalls[0].Records[0].Vector[0]; got != 0.1 {
		t.Fatalf("expected inserted vectors to be copied, got %v", got)
	}
}

func mustMilvusSeeder(t *testing.T, db milvusSeedDB, config MilvusSeederConfig) MilvusSeeder {
	t.Helper()
	seeder, err := NewMilvusSeeder(config, db)
	if err != nil {
		t.Fatalf("NewMilvusSeeder returned error: %v", err)
	}
	return seeder
}

func syntheticMilvusSeedDataset() fixtures.SyntheticDataset {
	return fixtures.SyntheticDataset{
		Seed:        42,
		Dimension:   3,
		RecordCount: 2,
		QueryCount:  1,
		Metric:      fixtures.MetricCosine,
		Records: []fixtures.SyntheticVector{
			{ID: "vec-000001", Vector: []float64{0.1, 0.2, 0.3}},
			{ID: "vec-000002", Vector: []float64{0.4, 0.5, 0.6}},
		},
		Queries: []fixtures.SyntheticVector{
			{ID: "query-000001", Vector: []float64{0.7, 0.8, 0.9}},
		},
	}
}

func infForMilvusSeed() float64 {
	return math.Inf(1)
}

type fakeMilvusSeedDB struct {
	createCalls []milvusCreateCollectionRequest
	insertCalls []milvusInsertRecordsRequest
	createErr   error
	insertErr   error
}

func (db *fakeMilvusSeedDB) CreateCollection(ctx context.Context, req milvusCreateCollectionRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db.createCalls = append(db.createCalls, req)
	return db.createErr
}

func (db *fakeMilvusSeedDB) InsertRecords(ctx context.Context, req milvusInsertRecordsRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	records := make([]milvusSeedRecord, len(req.Records))
	for index, record := range req.Records {
		records[index] = milvusSeedRecord{ID: record.ID, Vector: append([]float64(nil), record.Vector...)}
	}
	req.Records = records
	db.insertCalls = append(db.insertCalls, req)
	return db.insertErr
}

var errMilvusSeedFake = errors.New("fake milvus seed error")
