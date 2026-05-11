package migration

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/fixtures"
)

func TestNewPGVectorSeederAppliesDefaults(t *testing.T) {
	seeder, err := NewPGVectorSeeder(PGVectorSeederConfig{Dimension: 3}, &fakePGVectorSeedDB{})
	if err != nil {
		t.Fatalf("NewPGVectorSeeder returned error: %v", err)
	}

	if seeder.config.Table != "items" {
		t.Fatalf("unexpected table: %s", seeder.config.Table)
	}
	if seeder.config.IDColumn != "id" {
		t.Fatalf("unexpected id column: %s", seeder.config.IDColumn)
	}
	if seeder.config.VectorColumn != "embedding" {
		t.Fatalf("unexpected vector column: %s", seeder.config.VectorColumn)
	}
	if seeder.config.Dimension != 3 {
		t.Fatalf("unexpected dimension: %d", seeder.config.Dimension)
	}
}

func TestNewPGVectorSeederRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config PGVectorSeederConfig
	}{
		{name: "missing db", config: PGVectorSeederConfig{Dimension: 3}},
		{name: "zero dimension", config: PGVectorSeederConfig{}},
		{name: "dimension too large", config: PGVectorSeederConfig{Dimension: 2001}},
		{name: "unsafe table", config: PGVectorSeederConfig{Table: "items;drop", Dimension: 3}},
		{name: "unsafe id column", config: PGVectorSeederConfig{IDColumn: "id-name", Dimension: 3}},
		{name: "unsafe vector column", config: PGVectorSeederConfig{VectorColumn: "public.embedding", Dimension: 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPGVectorSeeder(tt.config, nil)
			if err == nil {
				t.Fatalf("expected invalid config to fail")
			}
		})
	}
}

func TestPGVectorSeederSeedCreatesExtensionTableAndUpsertsRecords(t *testing.T) {
	db := &fakePGVectorSeedDB{}
	seeder := mustPGVectorSeeder(t, db, PGVectorSeederConfig{Dimension: 3})

	result, err := seeder.Seed(context.Background(), syntheticSeedDataset())
	if err != nil {
		t.Fatalf("Seed returned error: %v", err)
	}

	if result.Table != "items" {
		t.Fatalf("unexpected result table: %s", result.Table)
	}
	if result.Dimension != 3 {
		t.Fatalf("unexpected result dimension: %d", result.Dimension)
	}
	if result.RecordsTotal != 2 || result.RecordsSeeded != 2 {
		t.Fatalf("unexpected result counts: %#v", result)
	}
	if len(db.calls) != 4 {
		t.Fatalf("expected extension, table, and two upserts; got %d calls", len(db.calls))
	}
	if !strings.Contains(db.calls[0].sql, "CREATE EXTENSION IF NOT EXISTS vector") {
		t.Fatalf("expected extension SQL, got %s", db.calls[0].sql)
	}
	if !strings.Contains(db.calls[1].sql, "CREATE TABLE IF NOT EXISTS") {
		t.Fatalf("expected table SQL, got %s", db.calls[1].sql)
	}
	if !strings.Contains(db.calls[1].sql, `"embedding" vector(3) NOT NULL`) {
		t.Fatalf("expected vector dimension in table SQL, got %s", db.calls[1].sql)
	}
	if !strings.Contains(db.calls[2].sql, "ON CONFLICT") {
		t.Fatalf("expected upsert SQL, got %s", db.calls[2].sql)
	}
	if got := db.calls[2].args[0]; got != "vec-000001" {
		t.Fatalf("unexpected first record id arg: %#v", got)
	}
	if got := db.calls[2].args[1]; got != "[0.1,0.2,0.3]" {
		t.Fatalf("unexpected first vector literal arg: %#v", got)
	}
}

func TestPGVectorSeederSeedRejectsInvalidDataset(t *testing.T) {
	seeder := mustPGVectorSeeder(t, &fakePGVectorSeedDB{}, PGVectorSeederConfig{Dimension: 3})
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

func TestPGVectorSeederSeedPropagatesContextCancellation(t *testing.T) {
	seeder := mustPGVectorSeeder(t, &fakePGVectorSeedDB{}, PGVectorSeederConfig{Dimension: 3})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := seeder.Seed(ctx, syntheticSeedDataset())
	if err == nil {
		t.Fatalf("expected canceled context to fail")
	}
}

func TestPGVectorSeederSeedAddsContextToExecErrors(t *testing.T) {
	db := &fakePGVectorSeedDB{errOnCall: 3, err: errPGVectorSeedFake}
	seeder := mustPGVectorSeeder(t, db, PGVectorSeederConfig{Dimension: 3})

	_, err := seeder.Seed(context.Background(), syntheticSeedDataset())
	if !errors.Is(err, errPGVectorSeedFake) {
		t.Fatalf("expected wrapped fake error, got %v", err)
	}
	if !strings.Contains(err.Error(), "upsert pgvector record") {
		t.Fatalf("expected contextual error, got %v", err)
	}
}

func TestFormatPGVectorSeedLiteralRejectsInvalidValues(t *testing.T) {
	tests := [][]float64{
		{},
		{math.Inf(1)},
	}
	for _, vector := range tests {
		if _, err := formatPGVectorSeedLiteral(vector); err == nil {
			t.Fatalf("expected invalid vector %#v to fail", vector)
		}
	}
}

func mustPGVectorSeeder(t *testing.T, db pgvectorSeedDB, config PGVectorSeederConfig) PGVectorSeeder {
	t.Helper()
	seeder, err := NewPGVectorSeeder(config, db)
	if err != nil {
		t.Fatalf("NewPGVectorSeeder returned error: %v", err)
	}
	return seeder
}

func syntheticSeedDataset() fixtures.SyntheticDataset {
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

type fakePGVectorSeedDB struct {
	calls     []pgvectorSeedExecCall
	errOnCall int
	err       error
}

type pgvectorSeedExecCall struct {
	sql  string
	args []any
}

func (db *fakePGVectorSeedDB) Exec(ctx context.Context, sql string, args ...any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db.calls = append(db.calls, pgvectorSeedExecCall{sql: sql, args: append([]any(nil), args...)})
	if db.errOnCall > 0 && len(db.calls) == db.errOnCall {
		return db.err
	}
	return nil
}

var errPGVectorSeedFake = errors.New("fake pgvector seed error")
