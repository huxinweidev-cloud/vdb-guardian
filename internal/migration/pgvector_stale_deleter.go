package migration

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const defaultPGVectorStaleTargetIDColumn = "id"

type pgvectorStaleTargetDeleteDB interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
}

type pgvectorStaleTargetConnector func(ctx context.Context, connectionURL string) (pgvectorStaleTargetDeleteDB, func(), error)

// PGXPGVectorStaleTargetDeleter deletes stale records from a pgvector target.
type PGXPGVectorStaleTargetDeleter struct {
	connectionURL string
	idColumn      string
	db            pgvectorStaleTargetDeleteDB
	connect       pgvectorStaleTargetConnector
}

// NewPGXPGVectorStaleTargetDeleter creates a pgx-backed stale target deleter
// that deletes by the default text primary key column "id".
func NewPGXPGVectorStaleTargetDeleter(connectionURL string) *PGXPGVectorStaleTargetDeleter {
	return NewPGXPGVectorStaleTargetDeleterWithIDColumn(connectionURL, defaultPGVectorStaleTargetIDColumn)
}

// NewPGXPGVectorStaleTargetDeleterWithIDColumn creates a pgx-backed stale target
// deleter for a custom primary-key/id column.
func NewPGXPGVectorStaleTargetDeleterWithIDColumn(connectionURL string, idColumn string) *PGXPGVectorStaleTargetDeleter {
	return &PGXPGVectorStaleTargetDeleter{connectionURL: connectionURL, idColumn: idColumn}
}

func newPGXPGVectorStaleTargetDeleterWithConnector(connectionURL string, idColumn string, connect pgvectorStaleTargetConnector) *PGXPGVectorStaleTargetDeleter {
	return &PGXPGVectorStaleTargetDeleter{connectionURL: connectionURL, idColumn: idColumn, connect: connect}
}

// DeleteTargetRecords deletes stale target rows by ID using bound parameters.
func (d *PGXPGVectorStaleTargetDeleter) DeleteTargetRecords(ctx context.Context, table string, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if d.idColumn == "" {
		return 0, fmt.Errorf("invalid pgvector stale target id column identifier %q", d.idColumn)
	}
	if err := validatePGVectorSeedIdentifier("target table", table); err != nil {
		return 0, err
	}
	if err := validatePGVectorSeedIdentifier("id column", d.idColumn); err != nil {
		return 0, err
	}
	db, closeDB, err := d.database(ctx)
	if err != nil {
		return 0, fmt.Errorf("connect pgvector stale target deleter")
	}
	if closeDB != nil {
		defer closeDB()
	}
	deleted, err := db.Exec(ctx, pgvectorStaleTargetDeleteSQL(table, d.idColumn), ids)
	if err != nil {
		return 0, fmt.Errorf("delete stale pgvector target records")
	}
	return deleted, nil
}

func (d *PGXPGVectorStaleTargetDeleter) database(ctx context.Context) (pgvectorStaleTargetDeleteDB, func(), error) {
	if d.db != nil {
		return d.db, nil, nil
	}
	if d.connect != nil {
		return d.connect(ctx, d.connectionURL)
	}
	return connectPGVectorStaleTargetDeleteDB(ctx, d.connectionURL)
}

func pgvectorStaleTargetDeleteSQL(table string, idColumn string) string {
	return fmt.Sprintf(`DELETE FROM %s WHERE %s = ANY($1)`, quotePGVectorSeedIdentifier(table), quotePGVectorSeedIdentifier(idColumn))
}

func connectPGVectorStaleTargetDeleteDB(ctx context.Context, connectionURL string) (pgvectorStaleTargetDeleteDB, func(), error) {
	conn, err := pgx.Connect(ctx, connectionURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect pgvector stale target deleter: %w", err)
	}
	return pgxPGVectorStaleTargetDeleteDB{conn: conn}, func() { _ = conn.Close(context.Background()) }, nil
}

type pgxPGVectorStaleTargetDeleteDB struct {
	conn *pgx.Conn
}

func (db pgxPGVectorStaleTargetDeleteDB) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := db.conn.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
