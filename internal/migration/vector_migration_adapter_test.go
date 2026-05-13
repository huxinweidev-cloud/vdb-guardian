package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
)

func TestNewMilvusVectorMigrationSourceRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config connectors.MilvusConfig
		want   string
	}{
		{name: "missing address", config: connectors.MilvusConfig{}, want: "milvus address is required"},
		{name: "bad source collection", config: connectors.MilvusConfig{Address: "localhost:19530", DefaultCollection: "bad-name"}, want: "invalid milvus default collection identifier"},
		{name: "bad id field", config: connectors.MilvusConfig{Address: "localhost:19530", IDField: "bad-name"}, want: "invalid milvus id field identifier"},
		{name: "bad vector field", config: connectors.MilvusConfig{Address: "localhost:19530", VectorField: "bad-name"}, want: "invalid milvus vector field identifier"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMilvusVectorMigrationSource(tc.config, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestNewPGVectorMigrationTargetRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config connectors.PGVectorConfig
		want   string
	}{
		{name: "missing url", config: connectors.PGVectorConfig{}, want: "pgvector connection URL is required"},
		{name: "bad table", config: connectors.PGVectorConfig{ConnectionURL: "postgres://example", DefaultTable: "bad-name"}, want: "invalid pgvector default table identifier"},
		{name: "bad id column", config: connectors.PGVectorConfig{ConnectionURL: "postgres://example", IDColumn: "bad-name"}, want: "invalid pgvector id column identifier"},
		{name: "bad vector column", config: connectors.PGVectorConfig{ConnectionURL: "postgres://example", VectorColumn: "bad-name"}, want: "invalid pgvector vector column identifier"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPGVectorMigrationTarget(tc.config, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestMilvusVectorMigrationSourceReadRecordsUsesAdapter(t *testing.T) {
	adapter := &fakeMilvusMigrationRecordReader{
		records: []VectorMigrationRecord{
			{ID: "vec-1", Vector: []float64{1, 2, 3}},
			{ID: "vec-2", Vector: []float64{4, 5, 6}},
		},
	}
	source, err := NewMilvusVectorMigrationSource(connectors.MilvusConfig{Address: "localhost:19530", DefaultCollection: "items", IDField: "id", VectorField: "embedding"}, adapter)
	if err != nil {
		t.Fatalf("NewMilvusVectorMigrationSource returned error: %v", err)
	}
	records, err := source.ReadRecords(context.Background(), "items")
	if err != nil {
		t.Fatalf("ReadRecords returned error: %v", err)
	}
	if adapter.collection != "items" || adapter.idField != "id" || adapter.vectorField != "embedding" {
		t.Fatalf("unexpected adapter request: collection=%q id=%q vector=%q", adapter.collection, adapter.idField, adapter.vectorField)
	}
	if len(records) != 2 || records[0].ID != "vec-1" || len(records[0].Vector) != 3 {
		t.Fatalf("unexpected records: %#v", records)
	}
	records[0].Vector[0] = 99
	if adapter.records[0].Vector[0] == 99 {
		t.Fatal("expected source to defensively copy adapter records")
	}
}

func TestMilvusVectorMigrationSourcePropagatesAdapterErrors(t *testing.T) {
	adapter := &fakeMilvusMigrationRecordReader{err: errors.New("boom")}
	source, err := NewMilvusVectorMigrationSource(connectors.MilvusConfig{Address: "localhost:19530", DefaultCollection: "items", IDField: "id", VectorField: "embedding"}, adapter)
	if err != nil {
		t.Fatalf("NewMilvusVectorMigrationSource returned error: %v", err)
	}
	_, err = source.ReadRecords(context.Background(), "items")
	if err == nil || !strings.Contains(err.Error(), "query milvus migration records") {
		t.Fatalf("expected wrapped adapter error, got %v", err)
	}
}

func TestPGVectorMigrationTargetWriteRecordsUsesAdapter(t *testing.T) {
	adapter := &fakePGVectorMigrationRecordWriter{}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{ConnectionURL: "postgres://example", DefaultTable: "items", IDColumn: "id", VectorColumn: "embedding"}, adapter)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget returned error: %v", err)
	}
	records := []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{1, 2, 3}}}
	if err := target.WriteRecords(context.Background(), "items", records); err != nil {
		t.Fatalf("WriteRecords returned error: %v", err)
	}
	if adapter.table != "items" || adapter.idColumn != "id" || adapter.vectorColumn != "embedding" {
		t.Fatalf("unexpected adapter request: table=%q id=%q vector=%q", adapter.table, adapter.idColumn, adapter.vectorColumn)
	}
	if len(adapter.writes) != 1 || adapter.writes[0].ID != "vec-1" {
		t.Fatalf("unexpected writes: %#v", adapter.writes)
	}
	records[0].Vector[0] = 99
	if adapter.writes[0].Vector[0] == 99 {
		t.Fatal("expected target to defensively copy records before writing")
	}
}

func TestPGVectorMigrationTargetPropagatesAdapterErrors(t *testing.T) {
	adapter := &fakePGVectorMigrationRecordWriter{err: errors.New("boom")}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{ConnectionURL: "postgres://example", DefaultTable: "items", IDColumn: "id", VectorColumn: "embedding"}, adapter)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget returned error: %v", err)
	}
	err = target.WriteRecords(context.Background(), "items", []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{1}}})
	if err == nil || !strings.Contains(err.Error(), "write pgvector migration records") {
		t.Fatalf("expected wrapped adapter error, got %v", err)
	}
}

func TestPGVectorMigrationTargetResetRecordsUsesAdapter(t *testing.T) {
	adapter := &fakePGVectorMigrationRecordWriter{}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{ConnectionURL: "postgres://example", DefaultTable: "items", IDColumn: "id", VectorColumn: "embedding"}, adapter)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget returned error: %v", err)
	}
	if err := target.ResetRecords(context.Background(), "items"); err != nil {
		t.Fatalf("ResetRecords returned error: %v", err)
	}
	if adapter.resetTable != "items" {
		t.Fatalf("unexpected reset table: %q", adapter.resetTable)
	}
}

func TestPGVectorMigrationTargetResetRecordsPropagatesAdapterErrors(t *testing.T) {
	adapter := &fakePGVectorMigrationRecordWriter{resetErr: errors.New("boom")}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{ConnectionURL: "postgres://example", DefaultTable: "items", IDColumn: "id", VectorColumn: "embedding"}, adapter)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget returned error: %v", err)
	}
	err = target.ResetRecords(context.Background(), "items")
	if err == nil || !strings.Contains(err.Error(), "reset pgvector migration records") {
		t.Fatalf("expected wrapped adapter error, got %v", err)
	}
}

type fakeMilvusMigrationRecordReader struct {
	collection  string
	idField     string
	vectorField string
	records     []VectorMigrationRecord
	err         error
}

func (f *fakeMilvusMigrationRecordReader) ReadMilvusMigrationRecords(ctx context.Context, collection, idField, vectorField string) ([]VectorMigrationRecord, error) {
	f.collection = collection
	f.idField = idField
	f.vectorField = vectorField
	if f.err != nil {
		return nil, f.err
	}
	return copyVectorMigrationRecords(f.records), nil
}

type fakePGVectorMigrationRecordWriter struct {
	table        string
	resetTable   string
	idColumn     string
	vectorColumn string
	writes       []VectorMigrationRecord
	err          error
	resetErr     error
}

func (f *fakePGVectorMigrationRecordWriter) WritePGVectorMigrationRecords(ctx context.Context, table, idColumn, vectorColumn string, records []VectorMigrationRecord) error {
	f.table = table
	f.idColumn = idColumn
	f.vectorColumn = vectorColumn
	if f.err != nil {
		return f.err
	}
	f.writes = copyVectorMigrationRecords(records)
	return nil
}

func (f *fakePGVectorMigrationRecordWriter) ResetPGVectorMigrationRecords(ctx context.Context, table string) error {
	if f.resetErr != nil {
		return f.resetErr
	}
	f.resetTable = table
	return nil
}
