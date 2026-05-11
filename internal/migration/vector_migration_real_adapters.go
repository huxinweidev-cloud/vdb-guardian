package migration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/jackc/pgx/v5"
	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
)

const defaultMilvusMigrationBatchSize = 1

type milvusMigrationQueryRequest struct {
	Collection  string
	IDField     string
	VectorField string
	BatchSize   int
	AllFields   bool
}

type milvusMigrationQueryBatch struct {
	Records []VectorMigrationRecord
}

type milvusMigrationQueryIterator interface {
	Next(ctx context.Context) (milvusMigrationQueryBatch, error)
	Close()
}

type milvusMigrationSDKClient interface {
	Count(ctx context.Context, collection string) (int, error)
	Query(ctx context.Context, req milvusMigrationQueryRequest) (milvusMigrationQueryIterator, error)
	Close(ctx context.Context) error
}

type milvusMigrationSDKClientFactory func(ctx context.Context, address string) (milvusMigrationSDKClient, error)

type milvusSDKMigrationReader struct {
	address   string
	batchSize int
	factory   milvusMigrationSDKClientFactory
}

func newMilvusSDKMigrationReader(address string) *milvusSDKMigrationReader {
	return newMilvusSDKMigrationReaderWithClientFactory(address, defaultMilvusMigrationBatchSize, newRealMilvusMigrationSDKClient)
}

func newMilvusSDKMigrationReaderWithClientFactory(address string, batchSize int, factory milvusMigrationSDKClientFactory) *milvusSDKMigrationReader {
	if batchSize <= 0 {
		batchSize = defaultMilvusMigrationBatchSize
	}
	return &milvusSDKMigrationReader{address: address, batchSize: batchSize, factory: factory}
}

func (r *milvusSDKMigrationReader) ReadMilvusMigrationRecords(ctx context.Context, collection, idField, vectorField string) ([]VectorMigrationRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.address == "" {
		return nil, errors.New("milvus address is required")
	}
	if r.factory == nil {
		r.factory = newRealMilvusMigrationSDKClient
	}
	client, err := r.factory(ctx, r.address)
	if err != nil {
		return nil, fmt.Errorf("connect milvus migration reader: %w", err)
	}
	defer func() { _ = client.Close(context.Background()) }()
	count, err := client.Count(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("count milvus migration records: %w", err)
	}
	if count == 0 {
		count = r.batchSize
	}
	iterator, err := client.Query(ctx, milvusMigrationQueryRequest{Collection: collection, IDField: idField, VectorField: vectorField, BatchSize: count, AllFields: true})
	if err != nil {
		return nil, fmt.Errorf("create milvus migration query: %w", err)
	}
	defer iterator.Close()
	records := make([]VectorMigrationRecord, 0, count)
	for {
		batch, err := iterator.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read milvus query batch: %w", err)
		}
		if len(batch.Records) == 0 {
			break
		}
		records = append(records, copyVectorMigrationRecords(batch.Records)...)
	}
	return records, nil
}

type realMilvusMigrationSDKClient struct {
	client milvusclient.Client
}

func newRealMilvusMigrationSDKClient(ctx context.Context, address string) (milvusMigrationSDKClient, error) {
	client, err := milvusclient.NewClient(ctx, milvusclient.Config{Address: address})
	if err != nil {
		return nil, err
	}
	return realMilvusMigrationSDKClient{client: client}, nil
}

func (c realMilvusMigrationSDKClient) Count(ctx context.Context, collection string) (int, error) {
	stats, err := c.client.GetCollectionStatistics(ctx, collection)
	if err != nil {
		return 0, err
	}
	rowCount, ok := stats["row_count"]
	if !ok {
		return 0, errors.New("milvus stats missing row_count")
	}
	count, err := strconv.Atoi(rowCount)
	if err != nil {
		return 0, fmt.Errorf("parse milvus row_count %q: %w", rowCount, err)
	}
	return count, nil
}

func (c realMilvusMigrationSDKClient) Query(ctx context.Context, req milvusMigrationQueryRequest) (milvusMigrationQueryIterator, error) {
	resultSet, err := c.client.Query(ctx, req.Collection, nil, "", []string{req.IDField, req.VectorField}, milvusclient.WithLimit(int64(req.BatchSize)))
	if err != nil {
		return nil, err
	}
	return &realMilvusMigrationResultSetIterator{resultSet: resultSet, idField: req.IDField, vectorField: req.VectorField}, nil
}

func (c realMilvusMigrationSDKClient) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.client.Close()
}

type realMilvusMigrationResultSetIterator struct {
	resultSet   milvusclient.ResultSet
	idField     string
	vectorField string
	consumed    bool
}

func (i *realMilvusMigrationResultSetIterator) Next(ctx context.Context) (milvusMigrationQueryBatch, error) {
	if i.consumed || i.resultSet == nil || i.resultSet.Len() == 0 {
		return milvusMigrationQueryBatch{}, io.EOF
	}
	idColumn := i.resultSet.GetColumn(i.idField)
	if idColumn == nil {
		return milvusMigrationQueryBatch{}, fmt.Errorf("milvus query result missing id field %q", i.idField)
	}
	vectorColumn := i.resultSet.GetColumn(i.vectorField)
	if vectorColumn == nil {
		return milvusMigrationQueryBatch{}, fmt.Errorf("milvus query result missing vector field %q", i.vectorField)
	}
	records := make([]VectorMigrationRecord, i.resultSet.Len())
	for index := 0; index < i.resultSet.Len(); index++ {
		id, err := idColumn.GetAsString(index)
		if err != nil {
			return milvusMigrationQueryBatch{}, fmt.Errorf("read milvus id at index %d: %w", index, err)
		}
		value, err := vectorColumn.Get(index)
		if err != nil {
			return milvusMigrationQueryBatch{}, fmt.Errorf("read milvus vector at index %d: %w", index, err)
		}
		vector32, ok := value.([]float32)
		if !ok {
			return milvusMigrationQueryBatch{}, fmt.Errorf("milvus vector field %q at index %d has type %T", i.vectorField, index, value)
		}
		records[index] = VectorMigrationRecord{ID: id, Vector: float32VectorToFloat64(vector32)}
	}
	i.consumed = true
	return milvusMigrationQueryBatch{Records: records}, nil
}

func (i *realMilvusMigrationResultSetIterator) Close() {}

type pgvectorMigrationDB interface {
	Exec(ctx context.Context, sql string, args ...any) error
}

type pgxPGVectorMigrationWriter struct {
	connectionURL string
	db            pgvectorMigrationDB
}

func newPGXPGVectorMigrationWriter(connectionURL string) *pgxPGVectorMigrationWriter {
	return &pgxPGVectorMigrationWriter{connectionURL: connectionURL}
}

func newPGXPGVectorMigrationWriterWithDB(db pgvectorMigrationDB) *pgxPGVectorMigrationWriter {
	return &pgxPGVectorMigrationWriter{db: db}
}

func (w *pgxPGVectorMigrationWriter) WritePGVectorMigrationRecords(ctx context.Context, table, idColumn, vectorColumn string, records []VectorMigrationRecord) error {
	db, err := w.database(ctx)
	if err != nil {
		return err
	}
	sql := pgvectorMigrationUpsertSQL(table, idColumn, vectorColumn)
	for _, record := range records {
		literal, err := formatPGVectorMigrationLiteral(record.Vector)
		if err != nil {
			return fmt.Errorf("format pgvector migration vector for %q: %w", record.ID, err)
		}
		if err := db.Exec(ctx, sql, record.ID, literal); err != nil {
			return fmt.Errorf("upsert pgvector migration record %q: %w", record.ID, err)
		}
	}
	return nil
}

func (w *pgxPGVectorMigrationWriter) database(ctx context.Context) (pgvectorMigrationDB, error) {
	if w.db != nil {
		return w.db, nil
	}
	conn, err := pgx.Connect(ctx, w.connectionURL)
	if err != nil {
		return nil, fmt.Errorf("connect pgvector migration database: %w", err)
	}
	w.db = pgxPGVectorMigrationDB{conn: conn}
	return w.db, nil
}

type pgxPGVectorMigrationDB struct {
	conn *pgx.Conn
}

func (db pgxPGVectorMigrationDB) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := db.conn.Exec(ctx, sql, args...)
	return err
}

func pgvectorMigrationUpsertSQL(table, idColumn, vectorColumn string) string {
	return fmt.Sprintf(
		`INSERT INTO %s (%s, %s) VALUES ($1, $2::vector) ON CONFLICT (%s) DO UPDATE SET %s = EXCLUDED.%s`,
		quotePGVectorSeedIdentifier(table),
		quotePGVectorSeedIdentifier(idColumn),
		quotePGVectorSeedIdentifier(vectorColumn),
		quotePGVectorSeedIdentifier(idColumn),
		quotePGVectorSeedIdentifier(vectorColumn),
		quotePGVectorSeedIdentifier(vectorColumn),
	)
}

func formatPGVectorMigrationLiteral(vector []float64) (string, error) {
	return formatPGVectorSeedLiteral(vector)
}

func float32VectorToFloat64(vector []float32) []float64 {
	converted := make([]float64, len(vector))
	for index, value := range vector {
		converted[index] = float64(value)
	}
	return converted
}
