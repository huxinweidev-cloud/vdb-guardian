package migration

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
)

func TestNewMilvusVectorMigrationSourceCreatesDefaultSDKReader(t *testing.T) {
	source, err := NewMilvusVectorMigrationSource(connectors.MilvusConfig{Address: "localhost:19530"}, nil)
	if err != nil {
		t.Fatalf("NewMilvusVectorMigrationSource() error = %v", err)
	}
	if source.reader == nil {
		t.Fatal("expected default Milvus migration reader")
	}
	reader, ok := source.reader.(*milvusSDKMigrationReader)
	if !ok {
		t.Fatalf("expected *milvusSDKMigrationReader, got %T", source.reader)
	}
	if reader.batchSize != 1 {
		t.Fatalf("expected conservative Milvus iterator batch size 1 to avoid dropping small collections, got %d", reader.batchSize)
	}
}

func TestNewPGVectorMigrationTargetCreatesDefaultPGXWriter(t *testing.T) {
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{ConnectionURL: "postgres://local/test"}, nil)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	if target.writer == nil {
		t.Fatal("expected default pgvector migration writer")
	}
	if _, ok := target.writer.(*pgxPGVectorMigrationWriter); !ok {
		t.Fatalf("expected *pgxPGVectorMigrationWriter, got %T", target.writer)
	}
}

func TestMilvusSDKMigrationReaderReadsRecordsUntilEOF(t *testing.T) {
	ctx := context.Background()
	client := &fakeMilvusMigrationSDKClient{
		count: 3,
		batches: []milvusMigrationQueryBatch{
			{
				Records: []VectorMigrationRecord{
					{ID: "vec-1", Vector: []float64{0.1, 0.2}},
					{ID: "vec-2", Vector: []float64{0.3, 0.4}},
				},
			},
			{
				Records: []VectorMigrationRecord{
					{ID: "vec-3", Vector: []float64{0.5, 0.6}},
				},
			},
		},
	}
	reader := newMilvusSDKMigrationReaderWithClientFactory("localhost:19530", 2, func(context.Context, string) (milvusMigrationSDKClient, error) {
		return client, nil
	})

	records, err := reader.ReadMilvusMigrationRecords(ctx, "items", "id", "embedding")
	if err != nil {
		t.Fatalf("ReadMilvusMigrationRecords() error = %v", err)
	}
	want := []VectorMigrationRecord{
		{ID: "vec-1", Vector: []float64{0.1, 0.2}},
		{ID: "vec-2", Vector: []float64{0.3, 0.4}},
		{ID: "vec-3", Vector: []float64{0.5, 0.6}},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("records mismatch\nwant: %#v\n got: %#v", want, records)
	}
	if client.requests[0].Collection != "items" || client.requests[0].IDField != "id" || client.requests[0].VectorField != "embedding" || client.requests[0].BatchSize != 3 || !client.requests[0].AllFields {
		t.Fatalf("unexpected request: %#v", client.requests[0])
	}
	if !client.closed {
		t.Fatal("expected Milvus migration query reader to be closed")
	}
}

func TestMilvusSDKMigrationReaderReadsMappedFullRecords(t *testing.T) {
	ctx := context.Background()
	client := &fakeMilvusMigrationSDKClient{
		count: 1,
		batches: []milvusMigrationQueryBatch{{Records: []VectorMigrationRecord{{
			ID: "sku-1", Vector: []float64{0.1, 0.2}, Scalars: map[string]any{"title": "First", "price": 9.5}, DynamicMetadata: map[string]any{"brand": "acme"}, Partition: "tenant_a",
		}}}},
	}
	reader := newMilvusSDKMigrationReaderWithClientFactory("localhost:19530", 2, func(context.Context, string) (milvusMigrationSDKClient, error) {
		return client, nil
	})
	request := MilvusMigrationReadRequest{Collection: "products", IDField: "sku", VectorField: "embedding", ScalarFields: []string{"title", "price"}, DynamicField: "_milvus_dynamic", PartitionField: "_milvus_partition"}

	records, err := reader.ReadMilvusMigrationRecordsWithMapping(ctx, request)
	if err != nil {
		t.Fatalf("ReadMilvusMigrationRecordsWithMapping() error = %v", err)
	}
	if len(records) != 1 || records[0].Scalars["title"] != "First" || records[0].DynamicMetadata["brand"] != "acme" || records[0].Partition != "tenant_a" {
		t.Fatalf("unexpected records: %#v", records)
	}
	if len(client.requests) != 1 || !reflect.DeepEqual(client.requests[0].ScalarFields, []string{"title", "price"}) || client.requests[0].DynamicField != "_milvus_dynamic" || client.requests[0].PartitionField != "_milvus_partition" {
		t.Fatalf("unexpected request: %#v", client.requests)
	}
}

func TestPGXPGVectorMigrationWriterWritesMappedFullRecords(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorMigrationDB{}
	writer := newPGXPGVectorMigrationWriterWithDB(db)
	request := PGVectorMigrationWriteRequest{
		Table:        "products",
		IDColumn:     "sku",
		VectorColumn: "embedding",
		ScalarColumns: []PGVectorMigrationScalarColumn{
			{SourceField: "product_title", TargetColumn: "title"},
			{SourceField: "price", TargetColumn: "price"},
		},
		DynamicColumn:   "milvus_dynamic",
		PartitionColumn: "milvus_partition",
		Records: []VectorMigrationRecord{{
			ID: "sku-1", Vector: []float64{0.1, 0.2}, Scalars: map[string]any{"product_title": "First", "price": 9.5}, DynamicMetadata: map[string]any{"brand": "acme"}, Partition: "tenant_a",
		}},
	}

	if err := writer.WritePGVectorMigrationRecordsWithMapping(ctx, request); err != nil {
		t.Fatalf("WritePGVectorMigrationRecordsWithMapping() error = %v", err)
	}
	if len(db.calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(db.calls))
	}
	call := db.calls[0]
	if !strings.Contains(call.sql, `INSERT INTO "products" ("sku", "embedding", "title", "price", "milvus_dynamic", "milvus_partition")`) {
		t.Fatalf("unexpected SQL: %s", call.sql)
	}
	if strings.Contains(call.sql, "First") || strings.Contains(call.sql, "acme") || strings.Contains(call.sql, "tenant_a") {
		t.Fatalf("SQL should use bound values, got: %s", call.sql)
	}
	if len(call.args) != 6 || call.args[0] != "sku-1" || call.args[1] != "[0.1,0.2]" || call.args[2] != "First" || call.args[3] != 9.5 || call.args[5] != "tenant_a" {
		t.Fatalf("unexpected args: %#v", call.args)
	}
	if got, ok := call.args[4].([]byte); !ok || string(got) != `{"brand":"acme"}` {
		t.Fatalf("unexpected dynamic metadata arg: %#v", call.args[4])
	}
}

func TestMilvusSDKMigrationReaderWrapsErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		factory milvusMigrationSDKClientFactory
		want    string
	}{
		{name: "connect", factory: func(context.Context, string) (milvusMigrationSDKClient, error) {
			return nil, errors.New("dial failed")
		}, want: "connect milvus migration reader"},
		{name: "query", factory: func(context.Context, string) (milvusMigrationSDKClient, error) {
			return &fakeMilvusMigrationSDKClient{count: 1, queryErr: errors.New("query failed")}, nil
		}, want: "create milvus migration query"},
		{name: "next", factory: func(context.Context, string) (milvusMigrationSDKClient, error) {
			return &fakeMilvusMigrationSDKClient{count: 1, nextErr: errors.New("next failed")}, nil
		}, want: "read milvus query batch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newMilvusSDKMigrationReaderWithClientFactory("localhost:19530", 100, tt.factory)
			_, err := reader.ReadMilvusMigrationRecords(ctx, "items", "id", "embedding")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestPGXPGVectorMigrationWriterWritesRecords(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorMigrationDB{}
	writer := newPGXPGVectorMigrationWriterWithDB(db)
	records := []VectorMigrationRecord{
		{ID: "vec-1", Vector: []float64{0.1, 0.2}},
		{ID: "vec-2", Vector: []float64{0.3, 0.4}},
	}

	if err := writer.WritePGVectorMigrationRecords(ctx, "items", "id", "embedding", records); err != nil {
		t.Fatalf("WritePGVectorMigrationRecords() error = %v", err)
	}
	if len(db.calls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(db.calls))
	}
	if !strings.Contains(db.calls[0].sql, `INSERT INTO "items" ("id", "embedding")`) {
		t.Fatalf("unexpected SQL: %s", db.calls[0].sql)
	}
	if db.calls[0].args[0] != "vec-1" || db.calls[0].args[1] != "[0.1,0.2]" {
		t.Fatalf("unexpected first args: %#v", db.calls[0].args)
	}
	if db.calls[1].args[0] != "vec-2" || db.calls[1].args[1] != "[0.3,0.4]" {
		t.Fatalf("unexpected second args: %#v", db.calls[1].args)
	}
}

func TestPGXPGVectorMigrationWriterValidatesVectorsAndWrapsExecError(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorMigrationDB{err: errors.New("write failed")}
	writer := newPGXPGVectorMigrationWriterWithDB(db)
	err := writer.WritePGVectorMigrationRecords(ctx, "items", "id", "embedding", []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{0.1}}})
	if err == nil || !strings.Contains(err.Error(), "upsert pgvector migration record") {
		t.Fatalf("expected wrapped exec error, got %v", err)
	}

	err = writer.WritePGVectorMigrationRecords(ctx, "items", "id", "embedding", []VectorMigrationRecord{{ID: "bad", Vector: nil}})
	if err == nil || !strings.Contains(err.Error(), "format pgvector migration vector") {
		t.Fatalf("expected vector format error, got %v", err)
	}
}

func TestPGXPGVectorMigrationWriterResetsRecords(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorMigrationDB{}
	writer := newPGXPGVectorMigrationWriterWithDB(db)

	if err := writer.ResetPGVectorMigrationRecords(ctx, "items"); err != nil {
		t.Fatalf("ResetPGVectorMigrationRecords() error = %v", err)
	}
	if len(db.calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(db.calls))
	}
	if db.calls[0].sql != `TRUNCATE TABLE "items"` {
		t.Fatalf("unexpected SQL: %s", db.calls[0].sql)
	}
	if len(db.calls[0].args) != 0 {
		t.Fatalf("expected no args, got %#v", db.calls[0].args)
	}
}

func TestPGXPGVectorMigrationWriterWrapsResetError(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorMigrationDB{err: errors.New("truncate failed")}
	writer := newPGXPGVectorMigrationWriterWithDB(db)

	err := writer.ResetPGVectorMigrationRecords(ctx, "items")
	if err == nil || !strings.Contains(err.Error(), "truncate pgvector migration table") {
		t.Fatalf("expected wrapped reset error, got %v", err)
	}
}

type fakeMilvusMigrationSDKClient struct {
	requests []milvusMigrationQueryRequest
	batches  []milvusMigrationQueryBatch
	count    int
	countErr error
	queryErr error
	nextErr  error
	closed   bool
}

func (c *fakeMilvusMigrationSDKClient) Count(ctx context.Context, collection string) (int, error) {
	if c.countErr != nil {
		return 0, c.countErr
	}
	return c.count, nil
}

func (c *fakeMilvusMigrationSDKClient) Query(ctx context.Context, req milvusMigrationQueryRequest) (milvusMigrationQueryIterator, error) {
	if c.queryErr != nil {
		return nil, c.queryErr
	}
	c.requests = append(c.requests, req)
	return &fakeMilvusMigrationQueryIterator{client: c}, nil
}

func (c *fakeMilvusMigrationSDKClient) Close(ctx context.Context) error { return nil }

type fakeMilvusMigrationQueryIterator struct {
	client *fakeMilvusMigrationSDKClient
	index  int
}

func (i *fakeMilvusMigrationQueryIterator) Next(ctx context.Context) (milvusMigrationQueryBatch, error) {
	if i.client.nextErr != nil {
		return milvusMigrationQueryBatch{}, i.client.nextErr
	}
	if i.index >= len(i.client.batches) {
		return milvusMigrationQueryBatch{}, io.EOF
	}
	batch := i.client.batches[i.index]
	i.index++
	return batch, nil
}

func (i *fakeMilvusMigrationQueryIterator) Close() { i.client.closed = true }

type fakePGVectorMigrationDB struct {
	calls []fakePGVectorMigrationExecCall
	err   error
}

type fakePGVectorMigrationExecCall struct {
	sql  string
	args []any
}

func (db *fakePGVectorMigrationDB) Exec(ctx context.Context, sql string, args ...any) error {
	db.calls = append(db.calls, fakePGVectorMigrationExecCall{sql: sql, args: append([]any(nil), args...)})
	return db.err
}
