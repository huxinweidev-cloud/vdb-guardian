package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
)

const defaultMilvusMigrationBatchSize = 1

// milvusMigrationQueryRequest defines the parameters for querying records from Milvus.
//
// milvusMigrationQueryRequest 定义了从 Milvus 查询记录的参数。
type milvusMigrationQueryRequest struct {
	Collection     string
	IDField        string
	VectorField    string
	ScalarFields   []string
	DynamicField   string
	PartitionField string
	BatchSize      int
	AllFields      bool
}

// milvusMigrationQueryBatch represents a batch of normalized records returned by a query.
//
// milvusMigrationQueryBatch 代表了查询返回的一批规范化记录。
type milvusMigrationQueryBatch struct {
	Records []VectorMigrationRecord
}

// milvusMigrationQueryIterator abstracts the iteration over batches of query results.
//
// milvusMigrationQueryIterator 抽象了对查询结果批次的迭代操作。
type milvusMigrationQueryIterator interface {
	Next(ctx context.Context) (milvusMigrationQueryBatch, error)
	Close()
}

// milvusMigrationSDKClient abstracts the underlying Milvus SDK operations needed for migration.
//
// milvusMigrationSDKClient 抽象了迁移所需的底层 Milvus SDK 操作。
type milvusMigrationSDKClient interface {
	Count(ctx context.Context, collection string) (int, error)
	Query(ctx context.Context, req milvusMigrationQueryRequest) (milvusMigrationQueryIterator, error)
	Close(ctx context.Context) error
}

type milvusMigrationSDKClientFactory func(ctx context.Context, address string) (milvusMigrationSDKClient, error)

// milvusSDKMigrationReader is a real Milvus SDK implementation of the migration reader.
//
// milvusSDKMigrationReader 是迁移读取器的真实 Milvus SDK 实现。
type milvusSDKMigrationReader struct {
	address   string
	batchSize int
	factory   milvusMigrationSDKClientFactory
}

// newMilvusSDKMigrationReader creates a new reader configured with the real Milvus client.
//
// newMilvusSDKMigrationReader 创建一个使用真实 Milvus 客户端配置的新读取器。
func newMilvusSDKMigrationReader(address string) *milvusSDKMigrationReader {
	return newMilvusSDKMigrationReaderWithClientFactory(address, defaultMilvusMigrationBatchSize, newRealMilvusMigrationSDKClient)
}

func newMilvusSDKMigrationReaderWithClientFactory(address string, batchSize int, factory milvusMigrationSDKClientFactory) *milvusSDKMigrationReader {
	if batchSize <= 0 {
		batchSize = defaultMilvusMigrationBatchSize
	}
	return &milvusSDKMigrationReader{address: address, batchSize: batchSize, factory: factory}
}

// ReadMilvusMigrationRecords fetches all records from the specified Milvus collection,
// automatically mapping them to the neutral VectorMigrationRecord format.
//
// ReadMilvusMigrationRecords 从指定的 Milvus 集合中拉取所有记录，
// 并自动将它们映射为中立的 VectorMigrationRecord 格式。
func (r *milvusSDKMigrationReader) ReadMilvusMigrationRecords(ctx context.Context, collection, idField, vectorField string) ([]VectorMigrationRecord, error) {
	return r.ReadMilvusMigrationRecordsWithMapping(ctx, MilvusMigrationReadRequest{Collection: collection, IDField: idField, VectorField: vectorField})
}

// ReadMilvusMigrationRecordsWithMapping fetches mapped full records from Milvus.
//
// ReadMilvusMigrationRecordsWithMapping 从 Milvus 拉取按 mapping 配置的完整记录。
func (r *milvusSDKMigrationReader) ReadMilvusMigrationRecordsWithMapping(ctx context.Context, request MilvusMigrationReadRequest) ([]VectorMigrationRecord, error) {
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
	count, err := client.Count(ctx, request.Collection)
	if err != nil {
		return nil, fmt.Errorf("count milvus migration records: %w", err)
	}
	if count == 0 {
		count = r.batchSize
	}
	iterator, err := client.Query(ctx, milvusMigrationQueryRequest{
		Collection:     request.Collection,
		IDField:        request.IDField,
		VectorField:    request.VectorField,
		ScalarFields:   append([]string(nil), request.ScalarFields...),
		DynamicField:   request.DynamicField,
		PartitionField: request.PartitionField,
		BatchSize:      count,
		AllFields:      true,
	})
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

// realMilvusMigrationSDKClient implements milvusMigrationSDKClient using the official Go SDK.
//
// realMilvusMigrationSDKClient 使用官方 Go SDK 实现了 milvusMigrationSDKClient 接口。
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
	outputFields := []string{req.IDField, req.VectorField}
	outputFields = append(outputFields, req.ScalarFields...)
	if req.DynamicField != "" {
		outputFields = append(outputFields, req.DynamicField)
	}
	if req.PartitionField != "" {
		outputFields = append(outputFields, req.PartitionField)
	}
	resultSet, err := c.client.Query(ctx, req.Collection, nil, "", outputFields, milvusclient.WithLimit(int64(req.BatchSize)))
	if err != nil {
		return nil, err
	}
	return &realMilvusMigrationResultSetIterator{resultSet: resultSet, request: req}, nil
}

func (c realMilvusMigrationSDKClient) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.client.Close()
}

// realMilvusMigrationResultSetIterator iterates over Milvus SDK query results.
//
// realMilvusMigrationResultSetIterator 迭代处理 Milvus SDK 查询结果。
type realMilvusMigrationResultSetIterator struct {
	resultSet milvusclient.ResultSet
	request   milvusMigrationQueryRequest
	consumed  bool
}

func (i *realMilvusMigrationResultSetIterator) Next(ctx context.Context) (milvusMigrationQueryBatch, error) {
	if i.consumed || i.resultSet == nil || i.resultSet.Len() == 0 {
		return milvusMigrationQueryBatch{}, io.EOF
	}
	idColumn := i.resultSet.GetColumn(i.request.IDField)
	if idColumn == nil {
		return milvusMigrationQueryBatch{}, fmt.Errorf("milvus query result missing id field %q", i.request.IDField)
	}
	vectorColumn := i.resultSet.GetColumn(i.request.VectorField)
	if vectorColumn == nil {
		return milvusMigrationQueryBatch{}, fmt.Errorf("milvus query result missing vector field %q", i.request.VectorField)
	}
	scalarColumns, err := i.scalarColumns()
	if err != nil {
		return milvusMigrationQueryBatch{}, err
	}
	requestColumns := milvusRecordRequestColumns{
		idColumn:        idColumn,
		vectorColumn:    vectorColumn,
		scalarColumns:   scalarColumns,
		dynamicColumn:   i.optionalColumn(i.request.DynamicField),
		partitionColumn: i.optionalColumn(i.request.PartitionField),
		idField:         i.request.IDField,
		vectorField:     i.request.VectorField,
		dynamicField:    i.request.DynamicField,
		partitionField:  i.request.PartitionField,
	}
	records := make([]VectorMigrationRecord, i.resultSet.Len())
	for index := 0; index < i.resultSet.Len(); index++ {
		record, err := readMilvusMigrationRecord(requestColumns, index)
		if err != nil {
			return milvusMigrationQueryBatch{}, err
		}
		records[index] = record
	}
	i.consumed = true
	return milvusMigrationQueryBatch{Records: records}, nil
}

func (i *realMilvusMigrationResultSetIterator) scalarColumns() (map[string]milvusResultColumn, error) {
	scalarColumns := map[string]milvusResultColumn{}
	for _, field := range i.request.ScalarFields {
		column := i.resultSet.GetColumn(field)
		if column == nil {
			return nil, fmt.Errorf("milvus query result missing scalar field %q", field)
		}
		scalarColumns[field] = column
	}
	return scalarColumns, nil
}

func (i *realMilvusMigrationResultSetIterator) optionalColumn(field string) milvusResultColumn {
	if field == "" {
		return nil
	}
	return i.resultSet.GetColumn(field)
}

type milvusRecordRequestColumns struct {
	idColumn        milvusResultColumn
	vectorColumn    milvusResultColumn
	scalarColumns   map[string]milvusResultColumn
	dynamicColumn   milvusResultColumn
	partitionColumn milvusResultColumn
	idField         string
	vectorField     string
	dynamicField    string
	partitionField  string
}

func readMilvusMigrationRecord(columns milvusRecordRequestColumns, index int) (VectorMigrationRecord, error) {
	id, err := columns.idColumn.GetAsString(index)
	if err != nil {
		return VectorMigrationRecord{}, fmt.Errorf("read milvus id at index %d: %w", index, err)
	}
	value, err := columns.vectorColumn.Get(index)
	if err != nil {
		return VectorMigrationRecord{}, fmt.Errorf("read milvus vector at index %d: %w", index, err)
	}
	vector32, ok := value.([]float32)
	if !ok {
		return VectorMigrationRecord{}, fmt.Errorf("milvus vector field %q at index %d has type %T", columns.vectorField, index, value)
	}
	record := VectorMigrationRecord{ID: id, Vector: float32VectorToFloat64(vector32)}
	if len(columns.scalarColumns) > 0 {
		scalars, err := readMilvusScalars(columns.scalarColumns, index)
		if err != nil {
			return VectorMigrationRecord{}, err
		}
		record.Scalars = scalars
	}
	if columns.dynamicColumn != nil {
		metadata, err := readMilvusDynamicMetadata(columns.dynamicColumn, index)
		if err != nil {
			return VectorMigrationRecord{}, err
		}
		record.DynamicMetadata = metadata
	}
	if columns.partitionColumn != nil {
		partition, err := columns.partitionColumn.GetAsString(index)
		if err != nil {
			return VectorMigrationRecord{}, fmt.Errorf("read milvus partition field %q at index %d: %w", columns.partitionField, index, err)
		}
		record.Partition = partition
	}
	return record, nil
}

func readMilvusScalars(columns map[string]milvusResultColumn, index int) (map[string]any, error) {
	scalars := make(map[string]any, len(columns))
	for field, column := range columns {
		value, err := column.Get(index)
		if err != nil {
			return nil, fmt.Errorf("read milvus scalar field %q at index %d: %w", field, index, err)
		}
		scalars[field] = value
	}
	return scalars, nil
}

type milvusResultColumn interface {
	Get(int) (any, error)
	GetAsString(int) (string, error)
}

func readMilvusDynamicMetadata(column milvusResultColumn, index int) (map[string]any, error) {
	value, err := column.Get(index)
	if err != nil {
		return nil, fmt.Errorf("read milvus dynamic metadata at index %d: %w", index, err)
	}
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return copyMigrationValueMap(typed), nil
	case []byte:
		return decodeMilvusDynamicMetadataJSON(typed, index)
	case string:
		return decodeMilvusDynamicMetadataJSON([]byte(typed), index)
	default:
		return map[string]any{"value": typed}, nil
	}
}

func decodeMilvusDynamicMetadataJSON(data []byte, index int) (map[string]any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode milvus dynamic metadata at index %d: %w", index, err)
	}
	return decoded, nil
}

func (i *realMilvusMigrationResultSetIterator) Close() {}

// pgvectorMigrationDB abstracts the execution of PostgreSQL queries.
//
// pgvectorMigrationDB 抽象了 PostgreSQL 查询的执行逻辑。
type pgvectorMigrationDB interface {
	Exec(ctx context.Context, sql string, args ...any) error
}

type pgvectorMigrationCopyDB interface {
	Begin(ctx context.Context) (pgvectorMigrationCopyTx, error)
}

type pgvectorMigrationCopyTx interface {
	Exec(ctx context.Context, sql string, args ...any) error
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// pgxPGVectorMigrationWriter is a real pgx-backed pgvector migration target writer.
//
// pgxPGVectorMigrationWriter 是一个使用 pgx 驱动的真实 pgvector 迁移目标端写入器。
type pgxPGVectorMigrationWriter struct {
	connectionURL string
	db            pgvectorMigrationDB
	copyDB        pgvectorMigrationCopyDB
}

func newPGXPGVectorMigrationWriter(connectionURL string) *pgxPGVectorMigrationWriter {
	return &pgxPGVectorMigrationWriter{connectionURL: connectionURL}
}

func newPGXPGVectorMigrationWriterWithDB(db pgvectorMigrationDB) *pgxPGVectorMigrationWriter {
	return &pgxPGVectorMigrationWriter{db: db}
}

func newPGXPGVectorMigrationWriterWithCopyDB(db pgvectorMigrationCopyDB) *pgxPGVectorMigrationWriter {
	return &pgxPGVectorMigrationWriter{copyDB: db}
}

// WritePGVectorMigrationRecords performs an upsert of the normalized records into the target pgvector table.
//
// WritePGVectorMigrationRecords 将规范化的记录以 upsert（插入或更新）语义写入目标 pgvector 表中。
func (w *pgxPGVectorMigrationWriter) WritePGVectorMigrationRecords(ctx context.Context, table, idColumn, vectorColumn string, records []VectorMigrationRecord) error {
	return w.WritePGVectorMigrationRecordsWithMapping(ctx, PGVectorMigrationWriteRequest{Table: table, IDColumn: idColumn, VectorColumn: vectorColumn, Records: records})
}

// WritePGVectorMigrationRecordsWithMapping performs a mapped full-record upsert into the target pgvector table.
//
// WritePGVectorMigrationRecordsWithMapping 按 mapping 将完整记录以 upsert 语义写入目标 pgvector 表。
func (w *pgxPGVectorMigrationWriter) WritePGVectorMigrationRecordsWithMapping(ctx context.Context, request PGVectorMigrationWriteRequest) error {
	db, err := w.database(ctx)
	if err != nil {
		return err
	}
	sql := pgvectorMigrationMappedUpsertSQL(request)
	for _, record := range request.Records {
		args, err := pgvectorMigrationMappedArgs(record, request)
		if err != nil {
			return err
		}
		if err := db.Exec(ctx, sql, args...); err != nil {
			return fmt.Errorf("upsert pgvector migration record %q: %w", record.ID, err)
		}
	}
	return nil
}

// WritePGVectorMigrationRecordsWithMappingCopy performs a mapped full-record COPY through a staging table.
//
// It exposes the internal COPY seam on the real writer so future migration target
// routing can select it explicitly while current production routing remains on
// WritePGVectorMigrationRecordsWithMapping.
func (w *pgxPGVectorMigrationWriter) WritePGVectorMigrationRecordsWithMappingCopy(ctx context.Context, request PGVectorMigrationWriteRequest) error {
	return w.copyPGVectorMigrationRecords(ctx, request)
}

func (w *pgxPGVectorMigrationWriter) copyPGVectorMigrationRecords(ctx context.Context, request PGVectorMigrationWriteRequest) error {
	if err := validateMigrationWriteRequest(request); err != nil {
		return err
	}
	if len(request.Records) == 0 {
		return nil
	}
	stagingTable := pgvectorMigrationStagingTableName(request.Table)
	if err := validateMigrationAdapterIdentifier("pgvector staging table", stagingTable); err != nil {
		return err
	}
	columns := pgvectorMigrationWriteColumns(request)
	rows, err := pgvectorMigrationCopyRows(request)
	if err != nil {
		return err
	}
	copyDB, err := w.copyDatabase(ctx)
	if err != nil {
		return err
	}
	tx, err := copyDB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin pgvector migration copy transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(context.Background()); rollbackErr != nil {
				return
			}
		}
	}()
	if err := tx.Exec(ctx, pgvectorMigrationStagingDDL(request, stagingTable)); err != nil {
		return fmt.Errorf("create pgvector migration staging table: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{stagingTable}, columns, pgx.CopyFromRows(rows)); err != nil {
		return newPGVectorCopyExecutionError(err)
	}
	if err := tx.Exec(ctx, pgvectorMigrationMergeSQL(request, stagingTable)); err != nil {
		return fmt.Errorf("merge pgvector migration staging records: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pgvector migration copy transaction: %w", err)
	}
	committed = true
	return nil
}

func (w *pgxPGVectorMigrationWriter) ResetPGVectorMigrationRecords(ctx context.Context, table string) error {
	db, err := w.database(ctx)
	if err != nil {
		return err
	}
	if err := db.Exec(ctx, pgvectorMigrationTruncateSQL(table)); err != nil {
		return fmt.Errorf("truncate pgvector migration table %q: %w", table, err)
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
	adapter := pgxPGVectorMigrationDB{conn: conn}
	w.db = adapter
	w.copyDB = adapter
	return w.db, nil
}

func (w *pgxPGVectorMigrationWriter) copyDatabase(ctx context.Context) (pgvectorMigrationCopyDB, error) {
	if w.copyDB != nil {
		return w.copyDB, nil
	}
	if db, ok := w.db.(pgvectorMigrationCopyDB); ok {
		w.copyDB = db
		return db, nil
	}
	conn, err := pgx.Connect(ctx, w.connectionURL)
	if err != nil {
		return nil, fmt.Errorf("connect pgvector migration database: %w", err)
	}
	adapter := pgxPGVectorMigrationDB{conn: conn}
	w.db = adapter
	w.copyDB = adapter
	return w.copyDB, nil
}

type pgxPGVectorMigrationDB struct {
	conn *pgx.Conn
}

func (db pgxPGVectorMigrationDB) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := db.conn.Exec(ctx, sql, args...)
	return err
}

func (db pgxPGVectorMigrationDB) Begin(ctx context.Context) (pgvectorMigrationCopyTx, error) {
	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return pgxPGVectorMigrationTx{tx: tx}, nil
}

type pgxPGVectorMigrationTx struct {
	tx pgx.Tx
}

func (tx pgxPGVectorMigrationTx) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := tx.tx.Exec(ctx, sql, args...)
	return err
}

func (tx pgxPGVectorMigrationTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return tx.tx.CopyFrom(ctx, tableName, columnNames, rowSrc)
}

func (tx pgxPGVectorMigrationTx) Commit(ctx context.Context) error {
	return tx.tx.Commit(ctx)
}

func (tx pgxPGVectorMigrationTx) Rollback(ctx context.Context) error {
	return tx.tx.Rollback(ctx)
}

func pgvectorMigrationMappedUpsertSQL(request PGVectorMigrationWriteRequest) string {
	columns := pgvectorMigrationWriteColumns(request)
	quotedColumns := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	assignments := make([]string, 0, len(columns)-1)
	for index, column := range columns {
		quoted := quotePGVectorSeedIdentifier(column)
		quotedColumns[index] = quoted
		placeholder := fmt.Sprintf("$%d", index+1)
		if column == request.VectorColumn {
			placeholder += "::vector"
		} else if column == request.DynamicColumn && request.DynamicColumn != "" {
			placeholder += "::jsonb"
		}
		placeholders[index] = placeholder
		if column != request.IDColumn {
			assignments = append(assignments, fmt.Sprintf("%s = EXCLUDED.%s", quoted, quoted))
		}
	}
	return fmt.Sprintf(
		`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s`,
		quotePGVectorSeedIdentifier(request.Table),
		joinSQLFragments(quotedColumns),
		joinSQLFragments(placeholders),
		quotePGVectorSeedIdentifier(request.IDColumn),
		joinSQLFragments(assignments),
	)
}

func pgvectorMigrationWriteColumns(request PGVectorMigrationWriteRequest) []string {
	columns := []string{request.IDColumn, request.VectorColumn}
	for _, scalar := range request.ScalarColumns {
		columns = append(columns, scalar.TargetColumn)
	}
	if request.DynamicColumn != "" {
		columns = append(columns, request.DynamicColumn)
	}
	if request.PartitionColumn != "" {
		columns = append(columns, request.PartitionColumn)
	}
	return columns
}

func pgvectorMigrationStagingTableName(table string) string {
	return table + "_migration_staging"
}

func pgvectorMigrationStagingDDL(request PGVectorMigrationWriteRequest, stagingTable string) string {
	columns := pgvectorMigrationWriteColumns(request)
	definitions := make([]string, len(columns))
	for index, column := range columns {
		definitions[index] = quotePGVectorSeedIdentifier(column) + " TEXT"
	}
	return fmt.Sprintf(`CREATE TEMP TABLE %s (%s) ON COMMIT DROP`, quotePGVectorSeedIdentifier(stagingTable), joinSQLFragments(definitions))
}

func pgvectorMigrationMergeSQL(request PGVectorMigrationWriteRequest, stagingTable string) string {
	columns := pgvectorMigrationWriteColumns(request)
	quotedColumns := make([]string, len(columns))
	selectColumns := make([]string, len(columns))
	assignments := make([]string, 0, len(columns)-1)
	for index, column := range columns {
		quoted := quotePGVectorSeedIdentifier(column)
		quotedColumns[index] = quoted
		selectColumn := quoted
		if column == request.VectorColumn {
			selectColumn += "::vector"
		} else if column == request.DynamicColumn && request.DynamicColumn != "" {
			selectColumn += "::jsonb"
		}
		selectColumns[index] = selectColumn
		if column != request.IDColumn {
			assignments = append(assignments, fmt.Sprintf("%s = EXCLUDED.%s", quoted, quoted))
		}
	}
	return fmt.Sprintf(
		`INSERT INTO %s (%s) SELECT %s FROM %s ON CONFLICT (%s) DO UPDATE SET %s`,
		quotePGVectorSeedIdentifier(request.Table),
		joinSQLFragments(quotedColumns),
		joinSQLFragments(selectColumns),
		quotePGVectorSeedIdentifier(stagingTable),
		quotePGVectorSeedIdentifier(request.IDColumn),
		joinSQLFragments(assignments),
	)
}

func pgvectorMigrationCopyRows(request PGVectorMigrationWriteRequest) ([][]any, error) {
	rows := make([][]any, len(request.Records))
	for index, record := range request.Records {
		args, err := pgvectorMigrationMappedArgs(record, request)
		if err != nil {
			return nil, err
		}
		rows[index] = args
	}
	return rows, nil
}

func pgvectorMigrationMappedArgs(record VectorMigrationRecord, request PGVectorMigrationWriteRequest) ([]any, error) {
	literal, err := formatPGVectorMigrationLiteral(record.Vector)
	if err != nil {
		return nil, fmt.Errorf("format pgvector migration vector for %q: %w", record.ID, err)
	}
	args := []any{record.ID, literal}
	for _, scalar := range request.ScalarColumns {
		args = append(args, record.Scalars[scalar.SourceField])
	}
	if request.DynamicColumn != "" {
		data, err := marshalPGVectorMigrationJSON(record.DynamicMetadata)
		if err != nil {
			return nil, fmt.Errorf("marshal pgvector migration dynamic metadata for %q: %w", record.ID, err)
		}
		args = append(args, data)
	}
	if request.PartitionColumn != "" {
		args = append(args, record.Partition)
	}
	return args, nil
}

func marshalPGVectorMigrationJSON(value any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(value)
}

func joinSQLFragments(parts []string) string {
	return strings.Join(parts, ", ")
}

func pgvectorMigrationTruncateSQL(table string) string {
	return fmt.Sprintf(`TRUNCATE TABLE %s`, quotePGVectorSeedIdentifier(table))
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
