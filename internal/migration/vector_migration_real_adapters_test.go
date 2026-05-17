package migration

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/jackc/pgx/v5"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func TestValidatePGVectorMigrationWriteModeAcceptsSupportedModes(t *testing.T) {
	for _, mode := range []PGVectorMigrationWriteMode{"", "batch-upsert", "copy", "auto"} {
		if err := validatePGVectorMigrationWriteMode(mode); err != nil {
			t.Fatalf("validatePGVectorMigrationWriteMode(%q) error = %v", mode, err)
		}
	}
}

func TestValidatePGVectorMigrationWriteModeRejectsUnsupportedMode(t *testing.T) {
	err := validatePGVectorMigrationWriteMode("truncate-and-load")
	if err == nil || !strings.Contains(err.Error(), "write mode") {
		t.Fatalf("expected write mode validation error, got %v", err)
	}
}

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

func TestNewPGVectorMigrationTargetRejectsInvalidWriteMode(t *testing.T) {
	_, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{WriteMode: "truncate-and-load"}, &fakePGVectorMappingMigrationWriter{})
	if err == nil || !strings.Contains(err.Error(), "write mode") {
		t.Fatalf("expected write mode validation error, got %v", err)
	}
}

func TestPGVectorMigrationTargetUsesCopyMode(t *testing.T) {
	ctx := context.Background()
	writer := &fakePGVectorMappingMigrationWriter{}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{WriteMode: "copy"}, writer)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	target = target.WithRecordMapping(CollectionRecordMapping{
		SourceCollection: "products",
		TargetTable:      "products_copy",
		PrimaryKey:       &RecordFieldMapping{SourceField: "sku", TargetColumn: "sku"},
		Vector:           &RecordFieldMapping{SourceField: "embedding", TargetColumn: "embedding"},
	})

	result, err := target.WriteRecordsWithResult(ctx, "", []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}}})
	if err != nil {
		t.Fatalf("WriteRecordsWithResult() error = %v", err)
	}
	if len(writer.requests) != 0 {
		t.Fatalf("expected copy mode to skip mapped upsert seam, got requests %#v", writer.requests)
	}
	if len(writer.copyRequests) != 1 {
		t.Fatalf("expected 1 mapped copy request, got %d", len(writer.copyRequests))
	}
	if writer.copyRequests[0].WriteMode != PGVectorMigrationWriteModeCopy {
		t.Fatalf("WriteMode = %q, want %q", writer.copyRequests[0].WriteMode, PGVectorMigrationWriteModeCopy)
	}
	if result.WriteModeUsed != string(PGVectorMigrationWriteModeCopy) || result.CopyBatches != 1 || result.BatchUpsertBatches != 0 {
		t.Fatalf("unexpected write result: %#v", result)
	}
}

func TestPGVectorMigrationTargetDefaultRemainsBatchUpsert(t *testing.T) {
	ctx := context.Background()
	writer := &fakePGVectorMappingMigrationWriter{}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{}, writer)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	target = target.WithRecordMapping(CollectionRecordMapping{
		SourceCollection: "products",
		TargetTable:      "products_copy",
		PrimaryKey:       &RecordFieldMapping{SourceField: "sku", TargetColumn: "sku"},
		Vector:           &RecordFieldMapping{SourceField: "embedding", TargetColumn: "embedding"},
	})

	result, err := target.WriteRecordsWithResult(ctx, "", []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}}})
	if err != nil {
		t.Fatalf("WriteRecordsWithResult() error = %v", err)
	}
	if len(writer.requests) != 1 {
		t.Fatalf("expected 1 mapped write request, got %d", len(writer.requests))
	}
	if writer.requests[0].WriteMode != PGVectorMigrationWriteModeBatchUpsert {
		t.Fatalf("WriteMode = %q, want %q", writer.requests[0].WriteMode, PGVectorMigrationWriteModeBatchUpsert)
	}
	if len(writer.copyRequests) != 0 {
		t.Fatalf("expected default mode to avoid copy seam, got copy requests %#v", writer.copyRequests)
	}
	if result.WriteModeUsed != string(PGVectorMigrationWriteModeBatchUpsert) || result.BatchUpsertBatches != 1 || result.CopyBatches != 0 {
		t.Fatalf("unexpected write result: %#v", result)
	}
}

func TestPGVectorMigrationTargetCopyModePropagatesCopyError(t *testing.T) {
	ctx := context.Background()
	copyErr := errors.New("copy unavailable")
	writer := &fakePGVectorMappingMigrationWriter{err: copyErr}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{WriteMode: "copy"}, writer)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	target = target.WithRecordMapping(CollectionRecordMapping{
		SourceCollection: "products",
		TargetTable:      "products_copy",
		PrimaryKey:       &RecordFieldMapping{SourceField: "sku", TargetColumn: "sku"},
		Vector:           &RecordFieldMapping{SourceField: "embedding", TargetColumn: "embedding"},
	})

	result, err := target.WriteRecordsWithResult(ctx, "", []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}}})
	if !errors.Is(err, copyErr) {
		t.Fatalf("expected copy error to propagate, got result %#v error %v", result, err)
	}
	if len(writer.requests) != 0 {
		t.Fatalf("copy error path should not fall back to mapped upsert, got requests %#v", writer.requests)
	}
	if len(writer.copyRequests) != 1 {
		t.Fatalf("expected 1 mapped copy request, got %d", len(writer.copyRequests))
	}
	if result != (VectorMigrationWriteResult{}) {
		t.Fatalf("expected zero result on copy error, got %#v", result)
	}
}

func TestPGVectorMigrationTargetAutoUsesCopyWhenSupported(t *testing.T) {
	ctx := context.Background()
	writer := &fakePGVectorMappingMigrationWriter{}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{WriteMode: "auto"}, writer)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	target = target.WithRecordMapping(CollectionRecordMapping{
		SourceCollection: "products",
		TargetTable:      "products_copy",
		PrimaryKey:       &RecordFieldMapping{SourceField: "sku", TargetColumn: "sku"},
		Vector:           &RecordFieldMapping{SourceField: "embedding", TargetColumn: "embedding"},
	})

	result, err := target.WriteRecordsWithResult(ctx, "", []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}}})
	if err != nil {
		t.Fatalf("WriteRecordsWithResult() error = %v", err)
	}
	if len(writer.copyRequests) != 1 {
		t.Fatalf("expected auto mode to try COPY once, got %d copy requests", len(writer.copyRequests))
	}
	if len(writer.requests) != 0 {
		t.Fatalf("expected successful auto COPY to avoid batch upsert, got requests %#v", writer.requests)
	}
	if result.WriteModeUsed != string(PGVectorMigrationWriteModeCopy) || result.CopyBatches != 1 || result.BatchUpsertBatches != 0 || result.CopyFallbacks != 0 {
		t.Fatalf("unexpected write result: %#v", result)
	}
}

func TestPGVectorMigrationTargetAutoFallbacksAfterRecoverableCopyFailure(t *testing.T) {
	ctx := context.Background()
	copyErr := newPGVectorCopyExecutionError(errors.New("copy stream reset"))
	writer := &fakePGVectorMappingMigrationWriter{copyErr: copyErr}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{WriteMode: "auto"}, writer)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	target = target.WithRecordMapping(CollectionRecordMapping{
		SourceCollection: "products",
		TargetTable:      "products_copy",
		PrimaryKey:       &RecordFieldMapping{SourceField: "sku", TargetColumn: "sku"},
		Vector:           &RecordFieldMapping{SourceField: "embedding", TargetColumn: "embedding"},
	})

	result, err := target.WriteRecordsWithResult(ctx, "", []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}}})
	if err != nil {
		t.Fatalf("WriteRecordsWithResult() error = %v", err)
	}
	if len(writer.copyRequests) != 1 || len(writer.requests) != 1 {
		t.Fatalf("expected one COPY attempt and one batch-upsert fallback, got copy=%d upsert=%d", len(writer.copyRequests), len(writer.requests))
	}
	if result.WriteModeUsed != string(PGVectorMigrationWriteModeBatchUpsert) || result.CopyBatches != 0 || result.BatchUpsertBatches != 1 || result.CopyFallbacks != 1 {
		t.Fatalf("unexpected fallback write result: %#v", result)
	}
}

func TestPGVectorMigrationTargetAutoDoesNotFallbackAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer := &fakePGVectorMappingMigrationWriter{copyErr: ctx.Err()}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{WriteMode: "auto"}, writer)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	target = target.WithRecordMapping(CollectionRecordMapping{
		SourceCollection: "products",
		TargetTable:      "products_copy",
		PrimaryKey:       &RecordFieldMapping{SourceField: "sku", TargetColumn: "sku"},
		Vector:           &RecordFieldMapping{SourceField: "embedding", TargetColumn: "embedding"},
	})

	result, err := target.WriteRecordsWithResult(ctx, "", []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation to propagate, got result %#v error %v", result, err)
	}
	if len(writer.copyRequests) != 1 || len(writer.requests) != 0 {
		t.Fatalf("expected cancellation to avoid fallback, got copy=%d upsert=%d", len(writer.copyRequests), len(writer.requests))
	}
}

func TestPGVectorMigrationTargetAutoDoesNotFallbackAfterValidationError(t *testing.T) {
	ctx := context.Background()
	writer := &fakePGVectorMappingMigrationWriter{}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{WriteMode: "auto"}, writer)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	target = target.WithRecordMapping(CollectionRecordMapping{
		SourceCollection: "products",
		TargetTable:      "bad-name",
		PrimaryKey:       &RecordFieldMapping{SourceField: "sku", TargetColumn: "sku"},
		Vector:           &RecordFieldMapping{SourceField: "embedding", TargetColumn: "embedding"},
	})

	result, err := target.WriteRecordsWithResult(ctx, "", []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}}})
	if err == nil || !strings.Contains(err.Error(), "invalid target table identifier") {
		t.Fatalf("expected validation error, got result %#v error %v", result, err)
	}
	if len(writer.copyRequests) != 0 || len(writer.requests) != 0 {
		t.Fatalf("validation error should not attempt copy or fallback, got copy=%d upsert=%d", len(writer.copyRequests), len(writer.requests))
	}
}

func TestPGVectorMigrationTargetCopyModeRejectsWriterWithoutCopySupport(t *testing.T) {
	ctx := context.Background()
	writer := &fakePGVectorMappingOnlyMigrationWriter{}
	target, err := NewPGVectorMigrationTarget(connectors.PGVectorConfig{WriteMode: "copy"}, writer)
	if err != nil {
		t.Fatalf("NewPGVectorMigrationTarget() error = %v", err)
	}
	target = target.WithRecordMapping(CollectionRecordMapping{
		SourceCollection: "products",
		TargetTable:      "products_copy",
		PrimaryKey:       &RecordFieldMapping{SourceField: "sku", TargetColumn: "sku"},
		Vector:           &RecordFieldMapping{SourceField: "embedding", TargetColumn: "embedding"},
	})

	_, err = target.WriteRecordsWithResult(ctx, "", []VectorMigrationRecord{{ID: "sku-1", Vector: []float64{0.1, 0.2}}})
	if err == nil || !strings.Contains(err.Error(), "does not support pgvector copy migration write mode") {
		t.Fatalf("expected clear copy support error, got %v", err)
	}
	if len(writer.requests) != 0 {
		t.Fatalf("copy mode should not fall back to mapped upsert, got requests %#v", writer.requests)
	}
}

func TestValidateMigrationWriteRequestRejectsInvalidWriteMode(t *testing.T) {
	err := validateMigrationWriteRequest(PGVectorMigrationWriteRequest{WriteMode: "truncate-and-load"})
	if err == nil || !strings.Contains(err.Error(), "write mode") {
		t.Fatalf("expected write mode validation error, got %v", err)
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

func TestMilvusSDKMigrationReaderReadsSeededDynamicAndPartitionContract(t *testing.T) {
	columns := milvusRecordRequestColumns{
		idColumn:        entity.NewColumnVarChar("sku", []string{"sku-1"}),
		vectorColumn:    entity.NewColumnFloatVector("embedding", 2, [][]float32{{0.1, 0.2}}),
		dynamicColumn:   entity.NewColumnJSONBytes(milvusSeedDynamicMetadataField, [][]byte{[]byte(`{"brand":"acme","_milvus_partition":"tenant_a"}`)}),
		partitionColumn: entity.NewColumnJSONBytes(milvusSeedDynamicMetadataField, [][]byte{[]byte(`{"brand":"acme","_milvus_partition":"tenant_a"}`)}),
		idField:         "sku",
		vectorField:     "embedding",
		dynamicField:    milvusSeedDynamicMetadataField,
		partitionField:  milvusSeedPartitionMetadataField,
	}

	record, err := readMilvusMigrationRecord(columns, 0)
	if err != nil {
		t.Fatalf("readMilvusMigrationRecord() error = %v", err)
	}
	if record.Partition != "tenant_a" {
		t.Fatalf("partition = %q, want tenant_a", record.Partition)
	}
	if !reflect.DeepEqual(record.DynamicMetadata, map[string]any{"brand": "acme"}) {
		t.Fatalf("dynamic metadata should exclude partition contract field, got %#v", record.DynamicMetadata)
	}
}

func TestMilvusSDKMigrationReaderRejectsNonStringSeededPartitionMetadata(t *testing.T) {
	column := entity.NewColumnJSONBytes(milvusSeedDynamicMetadataField, [][]byte{[]byte(`{"_milvus_partition":7}`)})

	_, err := readMilvusPartitionMetadata(column, milvusSeedPartitionMetadataField, 0)
	if err == nil || !strings.Contains(err.Error(), "has type") {
		t.Fatalf("expected partition metadata type error, got %v", err)
	}
}

func TestPGXPGVectorMigrationWriterCopyUsesStagingAndMerge(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorMigrationCopyDB{}
	writer := newPGXPGVectorMigrationWriterWithCopyDB(db)
	request := PGVectorMigrationWriteRequest{
		Table:        "products",
		IDColumn:     "sku",
		VectorColumn: "embedding",
		WriteMode:    PGVectorMigrationWriteModeCopy,
		ScalarColumns: []PGVectorMigrationScalarColumn{
			{SourceField: "product_title", TargetColumn: "title"},
		},
		DynamicColumn:   "milvus_dynamic",
		PartitionColumn: "milvus_partition",
		Records: []VectorMigrationRecord{{
			ID: "sku-1", Vector: []float64{0.1, 0.2}, Scalars: map[string]any{"product_title": "First"}, DynamicMetadata: map[string]any{"brand": "acme"}, Partition: "tenant_a",
		}},
	}

	if err := writer.WritePGVectorMigrationRecordsWithMappingCopy(ctx, request); err != nil {
		t.Fatalf("WritePGVectorMigrationRecordsWithMappingCopy() error = %v", err)
	}
	wantEvents := []string{"begin", "exec", "copy", "exec", "commit"}
	if !reflect.DeepEqual(db.events, wantEvents) {
		t.Fatalf("events mismatch\nwant: %#v\n got: %#v", wantEvents, db.events)
	}
	if len(db.tx.execSQL) != 2 {
		t.Fatalf("expected staging DDL and merge DML, got %#v", db.tx.execSQL)
	}
	if !strings.Contains(db.tx.execSQL[0], `CREATE TEMP TABLE "products_migration_staging"`) || !strings.Contains(db.tx.execSQL[0], `ON COMMIT DROP`) {
		t.Fatalf("unexpected staging DDL: %s", db.tx.execSQL[0])
	}
	if !strings.Contains(db.tx.execSQL[1], `INSERT INTO "products" ("sku", "embedding", "title", "milvus_dynamic", "milvus_partition") SELECT "sku", "embedding"::vector, "title", "milvus_dynamic"::jsonb, "milvus_partition" FROM "products_migration_staging" ON CONFLICT ("sku") DO UPDATE SET`) {
		t.Fatalf("unexpected merge DML: %s", db.tx.execSQL[1])
	}
	if db.tx.copyTable.Sanitize() != `"products_migration_staging"` {
		t.Fatalf("copy table = %s", db.tx.copyTable.Sanitize())
	}
	wantColumns := []string{"sku", "embedding", "title", "milvus_dynamic", "milvus_partition"}
	if !reflect.DeepEqual(db.tx.copyColumns, wantColumns) {
		t.Fatalf("copy columns mismatch\nwant: %#v\n got: %#v", wantColumns, db.tx.copyColumns)
	}
	if len(db.tx.copyRows) != 1 || db.tx.copyRows[0][0] != "sku-1" || db.tx.copyRows[0][1] != "[0.1,0.2]" || db.tx.copyRows[0][2] != "First" || db.tx.copyRows[0][4] != "tenant_a" {
		t.Fatalf("unexpected copy rows: %#v", db.tx.copyRows)
	}
	if got, ok := db.tx.copyRows[0][3].([]byte); !ok || string(got) != `{"brand":"acme"}` {
		t.Fatalf("unexpected dynamic metadata copy value: %#v", db.tx.copyRows[0][3])
	}
}

func TestPGVectorMigrationCopyRowsBuildsMappedRowsConsistentWithUpsertArgs(t *testing.T) {
	request := PGVectorMigrationWriteRequest{
		Table:        "products",
		IDColumn:     "sku",
		VectorColumn: "embedding",
		ScalarColumns: []PGVectorMigrationScalarColumn{
			{SourceField: "product_title", TargetColumn: "title"},
			{SourceField: "source_price", TargetColumn: "price"},
		},
		DynamicColumn:   "milvus_dynamic",
		PartitionColumn: "milvus_partition",
		Records: []VectorMigrationRecord{{
			ID:              "sku-1",
			Vector:          []float64{0.1, 0.2},
			Scalars:         map[string]any{"product_title": "First", "source_price": 9.5, "title": "wrong-target-value"},
			DynamicMetadata: map[string]any{"brand": "acme"},
			Partition:       "tenant_a",
		}},
	}

	rows, err := pgvectorMigrationCopyRows(request)
	if err != nil {
		t.Fatalf("pgvectorMigrationCopyRows() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 copy row, got %d", len(rows))
	}
	wantArgs, err := pgvectorMigrationMappedArgs(request.Records[0], request)
	if err != nil {
		t.Fatalf("pgvectorMigrationMappedArgs() error = %v", err)
	}
	if !reflect.DeepEqual(rows[0], wantArgs) {
		t.Fatalf("copy row should match mapped upsert args\nwant: %#v\n got: %#v", wantArgs, rows[0])
	}
	if rows[0][1] != "[0.1,0.2]" {
		t.Fatalf("vector literal = %#v, want pgvector upsert literal", rows[0][1])
	}
	if rows[0][2] != "First" || rows[0][3] != 9.5 {
		t.Fatalf("scalar values should come from source fields, got %#v", rows[0][2:4])
	}
	if got, ok := rows[0][4].([]byte); !ok || string(got) != `{"brand":"acme"}` {
		t.Fatalf("dynamic metadata should be JSONB-compatible bytes, got %#v", rows[0][4])
	}
	if rows[0][5] != "tenant_a" {
		t.Fatalf("partition value = %#v, want tenant_a", rows[0][5])
	}
}

func TestPGVectorMigrationCopyRowsEmptyBatchIsNoOp(t *testing.T) {
	rows, err := pgvectorMigrationCopyRows(PGVectorMigrationWriteRequest{Table: "products", IDColumn: "sku", VectorColumn: "embedding"})
	if err != nil {
		t.Fatalf("pgvectorMigrationCopyRows() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no copy rows for empty batch, got %#v", rows)
	}

	ctx := context.Background()
	db := &fakePGVectorMigrationCopyDB{}
	writer := newPGXPGVectorMigrationWriterWithCopyDB(db)
	if err := writer.WritePGVectorMigrationRecordsWithMappingCopy(ctx, PGVectorMigrationWriteRequest{Table: "products", IDColumn: "sku", VectorColumn: "embedding"}); err != nil {
		t.Fatalf("WritePGVectorMigrationRecordsWithMappingCopy() error = %v", err)
	}
	if len(db.events) != 0 {
		t.Fatalf("empty batch should not begin transaction or COPY, got events %#v", db.events)
	}
}

func TestPGVectorMigrationCopyRowsRejectsInvalidVectorBeforeDML(t *testing.T) {
	request := PGVectorMigrationWriteRequest{Table: "products", IDColumn: "sku", VectorColumn: "embedding", Records: []VectorMigrationRecord{{ID: "bad", Vector: nil}}}

	_, err := pgvectorMigrationCopyRows(request)
	if err == nil || !strings.Contains(err.Error(), "format pgvector migration vector") {
		t.Fatalf("expected vector format error, got %v", err)
	}

	ctx := context.Background()
	db := &fakePGVectorMigrationCopyDB{}
	writer := newPGXPGVectorMigrationWriterWithCopyDB(db)
	err = writer.WritePGVectorMigrationRecordsWithMappingCopy(ctx, request)
	if err == nil || !strings.Contains(err.Error(), "format pgvector migration vector") {
		t.Fatalf("expected vector format error before DML, got %v", err)
	}
	if len(db.events) != 0 {
		t.Fatalf("invalid vector should fail before transaction/DML, got events %#v", db.events)
	}
}

func TestPGXPGVectorMigrationWriterCopyRollsBackOnCopyError(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorMigrationCopyDB{copyErr: errors.New("copy failed")}
	writer := newPGXPGVectorMigrationWriterWithCopyDB(db)
	request := PGVectorMigrationWriteRequest{Table: "items", IDColumn: "id", VectorColumn: "embedding", Records: []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{0.1}}}}

	err := writer.WritePGVectorMigrationRecordsWithMappingCopy(ctx, request)
	if err == nil || !strings.Contains(err.Error(), "copy pgvector migration records") {
		t.Fatalf("expected wrapped copy error, got %v", err)
	}
	wantEvents := []string{"begin", "exec", "copy", "rollback"}
	if !reflect.DeepEqual(db.events, wantEvents) {
		t.Fatalf("events mismatch\nwant: %#v\n got: %#v", wantEvents, db.events)
	}
}

func TestPGXPGVectorMigrationWriterCopyRejectsUnsafeIdentifier(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorMigrationCopyDB{}
	writer := newPGXPGVectorMigrationWriterWithCopyDB(db)
	request := PGVectorMigrationWriteRequest{Table: "items;drop", IDColumn: "id", VectorColumn: "embedding", Records: []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{0.1}}}}

	err := writer.WritePGVectorMigrationRecordsWithMappingCopy(ctx, request)
	if err == nil || !strings.Contains(err.Error(), "invalid target table identifier") {
		t.Fatalf("expected unsafe identifier error, got %v", err)
	}
	if len(db.events) != 0 {
		t.Fatalf("expected no transaction for unsafe identifier, got events %#v", db.events)
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

type fakePGVectorMappingMigrationWriter struct {
	requests     []PGVectorMigrationWriteRequest
	copyRequests []PGVectorMigrationWriteRequest
	err          error
	copyErr      error
}

func (w *fakePGVectorMappingMigrationWriter) WritePGVectorMigrationRecords(ctx context.Context, table, idColumn, vectorColumn string, records []VectorMigrationRecord) error {
	return w.err
}

func (w *fakePGVectorMappingMigrationWriter) ResetPGVectorMigrationRecords(ctx context.Context, table string) error {
	return w.err
}

func (w *fakePGVectorMappingMigrationWriter) WritePGVectorMigrationRecordsWithMapping(ctx context.Context, request PGVectorMigrationWriteRequest) error {
	w.requests = append(w.requests, request)
	return w.err
}

func (w *fakePGVectorMappingMigrationWriter) WritePGVectorMigrationRecordsWithMappingCopy(ctx context.Context, request PGVectorMigrationWriteRequest) error {
	w.copyRequests = append(w.copyRequests, request)
	if w.copyErr != nil {
		return w.copyErr
	}
	return w.err
}

type fakePGVectorMappingOnlyMigrationWriter struct {
	requests []PGVectorMigrationWriteRequest
	err      error
}

func (w *fakePGVectorMappingOnlyMigrationWriter) WritePGVectorMigrationRecords(ctx context.Context, table, idColumn, vectorColumn string, records []VectorMigrationRecord) error {
	return w.err
}

func (w *fakePGVectorMappingOnlyMigrationWriter) ResetPGVectorMigrationRecords(ctx context.Context, table string) error {
	return w.err
}

func (w *fakePGVectorMappingOnlyMigrationWriter) WritePGVectorMigrationRecordsWithMapping(ctx context.Context, request PGVectorMigrationWriteRequest) error {
	w.requests = append(w.requests, request)
	return w.err
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

type fakePGVectorMigrationCopyDB struct {
	events   []string
	tx       *fakePGVectorMigrationCopyTx
	beginErr error
	copyErr  error
}

func (db *fakePGVectorMigrationCopyDB) Begin(ctx context.Context) (pgvectorMigrationCopyTx, error) {
	db.events = append(db.events, "begin")
	if db.beginErr != nil {
		return nil, db.beginErr
	}
	db.tx = &fakePGVectorMigrationCopyTx{db: db}
	return db.tx, nil
}

type fakePGVectorMigrationCopyTx struct {
	db          *fakePGVectorMigrationCopyDB
	execSQL     []string
	copyTable   pgx.Identifier
	copyColumns []string
	copyRows    [][]any
}

func (tx *fakePGVectorMigrationCopyTx) Exec(ctx context.Context, sql string, args ...any) error {
	tx.db.events = append(tx.db.events, "exec")
	tx.execSQL = append(tx.execSQL, sql)
	return nil
}

func (tx *fakePGVectorMigrationCopyTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	tx.db.events = append(tx.db.events, "copy")
	tx.copyTable = append(pgx.Identifier(nil), tableName...)
	tx.copyColumns = append([]string(nil), columnNames...)
	for rowSrc.Next() {
		values, err := rowSrc.Values()
		if err != nil {
			return 0, err
		}
		tx.copyRows = append(tx.copyRows, append([]any(nil), values...))
	}
	if err := rowSrc.Err(); err != nil {
		return 0, err
	}
	if tx.db.copyErr != nil {
		return 0, tx.db.copyErr
	}
	return int64(len(tx.copyRows)), nil
}

func (tx *fakePGVectorMigrationCopyTx) Commit(ctx context.Context) error {
	tx.db.events = append(tx.db.events, "commit")
	return nil
}

func (tx *fakePGVectorMigrationCopyTx) Rollback(ctx context.Context) error {
	tx.db.events = append(tx.db.events, "rollback")
	return nil
}
