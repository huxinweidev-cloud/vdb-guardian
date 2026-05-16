package migration

import (
	"context"
	"errors"
	"testing"
)

func TestPGVectorFullRecordReaderBuildsQuotedSelectAndMapsRows(t *testing.T) {
	db := &fakePGVectorFullRecordDB{rows: &fakePGVectorFullRecordRows{values: [][]any{{
		"sku-1",
		"[0.1,0.2]",
		"First",
		[]byte(`{"brand":"acme","score":1}`),
		"tenant_a",
	}}}}
	reader := newPGXPGVectorFullRecordReaderWithDB(db)
	records, err := reader.ReadPGVectorFullRecords(context.Background(), PGVectorFullRecordReadRequest{
		Table:           "items",
		IDColumn:        "id",
		VectorColumn:    "embedding",
		ScalarColumns:   []PGVectorMigrationScalarColumn{{SourceField: "product_title", TargetColumn: "title"}},
		DynamicColumn:   "metadata",
		PartitionColumn: "partition",
	})
	if err != nil {
		t.Fatalf("ReadPGVectorFullRecords returned error: %v", err)
	}
	wantSQL := `SELECT "id", "embedding", "title", "metadata", "partition" FROM "items" ORDER BY "id"`
	if db.sql != wantSQL {
		t.Fatalf("sql = %q, want %q", db.sql, wantSQL)
	}
	if len(db.args) != 0 {
		t.Fatalf("expected no query args, got %#v", db.args)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	record := records[0]
	if record.ID != "sku-1" || len(record.Vector) != 2 || record.Vector[1] != 0.2 || record.Scalars["product_title"] != "First" || record.DynamicMetadata["brand"] != "acme" || record.Partition != "tenant_a" {
		t.Fatalf("record = %#v", record)
	}
}

func TestPGVectorFullRecordReaderRejectsUnsafeIdentifiers(t *testing.T) {
	reader := newPGXPGVectorFullRecordReaderWithDB(&fakePGVectorFullRecordDB{})
	_, err := reader.ReadPGVectorFullRecords(context.Background(), PGVectorFullRecordReadRequest{Table: "items;drop", IDColumn: "id", VectorColumn: "embedding"})
	if err == nil || !containsString(err.Error(), "invalid target table identifier") {
		t.Fatalf("expected invalid identifier error, got %v", err)
	}
}

func TestPGVectorFullRecordReaderReturnsRowErrors(t *testing.T) {
	reader := newPGXPGVectorFullRecordReaderWithDB(&fakePGVectorFullRecordDB{rows: &fakePGVectorFullRecordRows{err: errors.New("boom")}})
	_, err := reader.ReadPGVectorFullRecords(context.Background(), PGVectorFullRecordReadRequest{Table: "items", IDColumn: "id", VectorColumn: "embedding"})
	if err == nil || !containsString(err.Error(), "iterate pgvector full records") {
		t.Fatalf("expected row iteration error, got %v", err)
	}
}

func TestPGVectorFullRecordReaderClosesOwnedConnections(t *testing.T) {
	closed := false
	db := &fakePGVectorFullRecordDB{}
	reader := newPGXPGVectorFullRecordReaderWithConnector("postgres://[REDACTED]", func(ctx context.Context, connectionURL string) (pgvectorFullRecordDB, func(), error) {
		if connectionURL != "postgres://[REDACTED]" {
			t.Fatalf("connectionURL = %q", connectionURL)
		}
		return db, func() { closed = true }, nil
	})
	_, err := reader.ReadPGVectorFullRecords(context.Background(), PGVectorFullRecordReadRequest{Table: "items", IDColumn: "id", VectorColumn: "embedding"})
	if err != nil {
		t.Fatalf("ReadPGVectorFullRecords returned error: %v", err)
	}
	if !closed {
		t.Fatal("expected owned pgvector connection to be closed")
	}
}

type fakePGVectorFullRecordDB struct {
	sql  string
	args []any
	rows *fakePGVectorFullRecordRows
	err  error
}

func (db *fakePGVectorFullRecordDB) Query(ctx context.Context, sql string, args ...any) (pgvectorFullRecordRows, error) {
	db.sql = sql
	db.args = append([]any(nil), args...)
	if db.err != nil {
		return nil, db.err
	}
	if db.rows == nil {
		db.rows = &fakePGVectorFullRecordRows{}
	}
	return db.rows, nil
}

type fakePGVectorFullRecordRows struct {
	values [][]any
	index  int
	err    error
}

func (r *fakePGVectorFullRecordRows) Next() bool {
	return r.index < len(r.values)
}

func (r *fakePGVectorFullRecordRows) Scan(dest ...any) error {
	row := r.values[r.index]
	r.index++
	for index := range dest {
		switch pointer := dest[index].(type) {
		case *string:
			*pointer = row[index].(string)
		case *any:
			*pointer = row[index]
		default:
			return errors.New("unsupported destination")
		}
	}
	return nil
}

func (r *fakePGVectorFullRecordRows) Err() error { return r.err }

func (r *fakePGVectorFullRecordRows) Close() {}
