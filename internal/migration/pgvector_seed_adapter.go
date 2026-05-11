package migration

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type pgxPGVectorSeedDB struct {
	connectionURL string
	conn          *pgx.Conn
}

// NewPGXPGVectorSeedDB creates a pgx-backed adapter for pgvector fixture seeding.
//
// The adapter opens the PostgreSQL connection lazily on the first Exec call so
// CLI option parsing and fixture validation can fail before any network side
// effects occur. Callers must close the adapter after use.
//
// NewPGXPGVectorSeedDB 创建一个由 pgx 驱动的 pgvector 数据灌入适配器。
// 该适配器会在第一次调用 Exec 方法时采用延迟加载 (lazy-loading) 的策略建立连接，
// 这样在 CLI 选项解析或固件合法性校验失败时，系统就能在任何网络副作用产生前
// 迅速终止。调用方必须在操作完成后主动关闭该适配器。
func NewPGXPGVectorSeedDB(connectionURL string) *pgxPGVectorSeedDB {
	return &pgxPGVectorSeedDB{connectionURL: connectionURL}
}

func (db *pgxPGVectorSeedDB) Exec(ctx context.Context, sql string, args ...any) error {
	conn, err := db.connection(ctx)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("exec pgvector seed sql: %w", err)
	}
	return nil
}

// Close releases the underlying PostgreSQL connection when it was opened.
//
// Close 如果之前开启了底层的 PostgreSQL 连接，则负责将其释放。
func (db *pgxPGVectorSeedDB) Close() error {
	if db.conn == nil {
		return nil
	}
	return db.conn.Close(context.Background())
}

func (db *pgxPGVectorSeedDB) connection(ctx context.Context) (*pgx.Conn, error) {
	if db.conn != nil {
		return db.conn, nil
	}
	conn, err := pgx.Connect(ctx, db.connectionURL)
	if err != nil {
		return nil, fmt.Errorf("connect pgvector seed database: %w", err)
	}
	db.conn = conn
	return db.conn, nil
}
