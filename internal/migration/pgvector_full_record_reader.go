package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PGVectorFullRecordReadRequest describes a read-only projection of mapped
// pgvector columns back into neutral full-record migration records.
type PGVectorFullRecordReadRequest struct {
	Table           string
	IDColumn        string
	VectorColumn    string
	ScalarColumns   []PGVectorMigrationScalarColumn
	DynamicColumn   string
	PartitionColumn string
}

// PGVectorFullRecordReadRequestFromMapping converts a record mapping into a
// pgvector full-record read request, preserving source scalar field names for
// cross-system comparison compatibility.
func PGVectorFullRecordReadRequestFromMapping(mapping CollectionRecordMapping) PGVectorFullRecordReadRequest {
	request := PGVectorFullRecordReadRequest{Table: mapping.TargetTable}
	if mapping.PrimaryKey != nil {
		request.IDColumn = mapping.PrimaryKey.TargetColumn
	}
	if mapping.Vector != nil {
		request.VectorColumn = mapping.Vector.TargetColumn
	}
	for _, scalar := range mapping.Scalars {
		request.ScalarColumns = append(request.ScalarColumns, PGVectorMigrationScalarColumn{SourceField: scalar.SourceField, TargetColumn: scalar.TargetColumn})
	}
	if mapping.DynamicMetadata != nil {
		request.DynamicColumn = mapping.DynamicMetadata.TargetColumn
	}
	if mapping.PartitionMetadata != nil {
		request.PartitionColumn = mapping.PartitionMetadata.TargetColumn
	}
	return request
}

type pgvectorFullRecordDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgvectorFullRecordRows, error)
}

type pgvectorFullRecordConnector func(ctx context.Context, connectionURL string) (pgvectorFullRecordDB, func(), error)

type pgvectorFullRecordRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// PGXPGVectorFullRecordReader reads mapped full records from a pgvector target.
type PGXPGVectorFullRecordReader struct {
	connectionURL string
	db            pgvectorFullRecordDB
	connect       pgvectorFullRecordConnector
}

// NewPGXPGVectorFullRecordReader creates a read-only pgvector full-record reader.
func NewPGXPGVectorFullRecordReader(connectionURL string) PGXPGVectorFullRecordReader {
	return PGXPGVectorFullRecordReader{connectionURL: connectionURL}
}

func newPGXPGVectorFullRecordReaderWithDB(db pgvectorFullRecordDB) PGXPGVectorFullRecordReader {
	return PGXPGVectorFullRecordReader{db: db}
}

func newPGXPGVectorFullRecordReaderWithConnector(connectionURL string, connect pgvectorFullRecordConnector) PGXPGVectorFullRecordReader {
	return PGXPGVectorFullRecordReader{connectionURL: connectionURL, connect: connect}
}

// ReadPGVectorFullRecords reads mapped pgvector full records.
func (r PGXPGVectorFullRecordReader) ReadPGVectorFullRecords(ctx context.Context, request PGVectorFullRecordReadRequest) ([]VectorMigrationRecord, error) {
	if err := validatePGVectorFullRecordReadRequest(request); err != nil {
		return nil, err
	}
	db, closeDB, err := r.database(ctx)
	if err != nil {
		return nil, err
	}
	if closeDB != nil {
		defer closeDB()
	}
	rows, err := db.Query(ctx, pgvectorFullRecordSelectSQL(request))
	if err != nil {
		return nil, fmt.Errorf("query pgvector full records: %w", err)
	}
	defer rows.Close()
	records := []VectorMigrationRecord{}
	for rows.Next() {
		record, err := scanPGVectorFullRecord(rows, request)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pgvector full records: %w", err)
	}
	return records, nil
}

func (r PGXPGVectorFullRecordReader) database(ctx context.Context) (pgvectorFullRecordDB, func(), error) {
	if r.db != nil {
		return r.db, nil, nil
	}
	if r.connect != nil {
		return r.connect(ctx, r.connectionURL)
	}
	return connectPGVectorFullRecordDB(ctx, r.connectionURL)
}

func connectPGVectorFullRecordDB(ctx context.Context, connectionURL string) (pgvectorFullRecordDB, func(), error) {
	conn, err := pgx.Connect(ctx, connectionURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect pgvector full-record database: %w", err)
	}
	return pgxFullRecordDB{conn: conn}, func() { _ = conn.Close(context.Background()) }, nil
}

type pgxFullRecordDB struct {
	conn *pgx.Conn
}

func (db pgxFullRecordDB) Query(ctx context.Context, sql string, args ...any) (pgvectorFullRecordRows, error) {
	return db.conn.Query(ctx, sql, args...)
}

func validatePGVectorFullRecordReadRequest(request PGVectorFullRecordReadRequest) error {
	for label, value := range map[string]string{"target table": request.Table, "pgvector id column": request.IDColumn, "pgvector vector column": request.VectorColumn, "pgvector dynamic column": request.DynamicColumn, "pgvector partition column": request.PartitionColumn} {
		if value != "" {
			if err := validateMigrationAdapterIdentifier(label, value); err != nil {
				return err
			}
		}
	}
	if request.Table == "" || request.IDColumn == "" || request.VectorColumn == "" {
		return fmt.Errorf("pgvector full-record table, id column, and vector column are required")
	}
	for _, scalar := range request.ScalarColumns {
		if err := validateMigrationAdapterIdentifier("pgvector scalar source field", scalar.SourceField); err != nil {
			return err
		}
		if err := validateMigrationAdapterIdentifier("pgvector scalar column", scalar.TargetColumn); err != nil {
			return err
		}
	}
	return nil
}

func pgvectorFullRecordSelectSQL(request PGVectorFullRecordReadRequest) string {
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
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = quotePGVectorSeedIdentifier(column)
	}
	return fmt.Sprintf("SELECT %s FROM %s ORDER BY %s", strings.Join(quoted, ", "), quotePGVectorSeedIdentifier(request.Table), quotePGVectorSeedIdentifier(request.IDColumn))
}

func scanPGVectorFullRecord(rows pgvectorFullRecordRows, request PGVectorFullRecordReadRequest) (VectorMigrationRecord, error) {
	var id string
	var vectorValue any
	scalarValues := make([]any, len(request.ScalarColumns))
	dest := []any{&id, &vectorValue}
	for index := range scalarValues {
		dest = append(dest, &scalarValues[index])
	}
	var dynamicValue any
	if request.DynamicColumn != "" {
		dest = append(dest, &dynamicValue)
	}
	var partition string
	if request.PartitionColumn != "" {
		dest = append(dest, &partition)
	}
	if err := rows.Scan(dest...); err != nil {
		return VectorMigrationRecord{}, fmt.Errorf("scan pgvector full record: %w", err)
	}
	vector, err := parsePGVectorFullRecordVector(vectorValue)
	if err != nil {
		return VectorMigrationRecord{}, fmt.Errorf("parse pgvector vector for %q: %w", id, err)
	}
	record := VectorMigrationRecord{ID: id, Vector: vector}
	if len(request.ScalarColumns) > 0 {
		record.Scalars = make(map[string]any, len(request.ScalarColumns))
		for index, scalar := range request.ScalarColumns {
			record.Scalars[scalar.SourceField] = scalarValues[index]
		}
	}
	if request.DynamicColumn != "" {
		metadata, err := parsePGVectorFullRecordMetadata(dynamicValue)
		if err != nil {
			return VectorMigrationRecord{}, fmt.Errorf("parse pgvector dynamic metadata for %q: %w", id, err)
		}
		record.DynamicMetadata = metadata
	}
	if request.PartitionColumn != "" {
		record.Partition = partition
	}
	return record, nil
}

func parsePGVectorFullRecordVector(value any) ([]float64, error) {
	switch typed := value.(type) {
	case []float64:
		return append([]float64(nil), typed...), nil
	case []float32:
		return float32VectorToFloat64(typed), nil
	case string:
		return parsePGVectorTextVector(typed)
	case []byte:
		return parsePGVectorTextVector(string(typed))
	default:
		return nil, fmt.Errorf("unsupported vector type %T", value)
	}
}

func parsePGVectorTextVector(value string) ([]float64, error) {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	vector := make([]float64, len(parts))
	for index, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, err
		}
		vector[index] = parsed
	}
	return vector, nil
}

func parsePGVectorFullRecordMetadata(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return copyMigrationValueMap(typed), nil
	case []byte:
		return parsePGVectorFullRecordMetadataJSON(typed)
	case string:
		return parsePGVectorFullRecordMetadataJSON([]byte(typed))
	default:
		return map[string]any{"value": typed}, nil
	}
}

func parsePGVectorFullRecordMetadataJSON(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}
