package migration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPGXPGVectorStaleTargetDeleterDeletesIDsWithBoundArray(t *testing.T) {
	ctx := context.Background()
	db := &fakePGVectorStaleTargetDeleteDB{deleted: 2}
	deleter := NewPGXPGVectorStaleTargetDeleter("postgres://[REDACTED]/db")
	deleter.db = db

	deleted, err := deleter.DeleteTargetRecords(ctx, "items", []string{"sku-2", "sku-1"})
	if err != nil {
		t.Fatalf("DeleteTargetRecords returned error: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}
	if len(db.calls) != 1 {
		t.Fatalf("expected one exec call, got %d", len(db.calls))
	}
	call := db.calls[0]
	if call.sql != `DELETE FROM "items" WHERE "id" = ANY($1)` {
		t.Fatalf("unexpected SQL: %s", call.sql)
	}
	if strings.Contains(call.sql, "sku-1") || strings.Contains(call.sql, "sku-2") {
		t.Fatalf("SQL should not inline stale IDs: %s", call.sql)
	}
	if !reflect.DeepEqual(call.args, []any{[]string{"sku-2", "sku-1"}}) {
		t.Fatalf("args = %#v", call.args)
	}
}

func TestPGXPGVectorStaleTargetDeleterSupportsCustomIDColumn(t *testing.T) {
	db := &fakePGVectorStaleTargetDeleteDB{deleted: 1}
	deleter := NewPGXPGVectorStaleTargetDeleterWithIDColumn("postgres://[REDACTED]/db", "sku")
	deleter.db = db

	_, err := deleter.DeleteTargetRecords(context.Background(), "products", []string{"sku-1"})
	if err != nil {
		t.Fatalf("DeleteTargetRecords returned error: %v", err)
	}
	if got := db.calls[0].sql; got != `DELETE FROM "products" WHERE "sku" = ANY($1)` {
		t.Fatalf("unexpected SQL: %s", got)
	}
}

func TestPGXPGVectorStaleTargetDeleterRejectsUnsafeIdentifiers(t *testing.T) {
	tests := []struct {
		name     string
		table    string
		idColumn string
	}{
		{name: "unsafe table", table: "items;drop", idColumn: "id"},
		{name: "empty table", table: "", idColumn: "id"},
		{name: "unsafe id column", table: "items", idColumn: "id;drop"},
		{name: "empty id column", table: "items", idColumn: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleter := NewPGXPGVectorStaleTargetDeleterWithIDColumn("postgres://[REDACTED]/db", tt.idColumn)
			deleter.db = &fakePGVectorStaleTargetDeleteDB{}

			_, err := deleter.DeleteTargetRecords(context.Background(), tt.table, []string{"sku-1"})
			if err == nil {
				t.Fatal("DeleteTargetRecords returned nil error for unsafe identifier")
			}
			if !strings.Contains(err.Error(), "identifier") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestPGXPGVectorStaleTargetDeleterNoopsForEmptyIDs(t *testing.T) {
	db := &fakePGVectorStaleTargetDeleteDB{}
	deleter := NewPGXPGVectorStaleTargetDeleter("postgres://[REDACTED]/db")
	deleter.db = db

	deleted, err := deleter.DeleteTargetRecords(context.Background(), "items", nil)
	if err != nil {
		t.Fatalf("DeleteTargetRecords returned error: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
	if len(db.calls) != 0 {
		t.Fatalf("expected no exec calls, got %d", len(db.calls))
	}
}

func TestPGXPGVectorStaleTargetDeleterWrapsExecErrorsWithoutLeakingIDs(t *testing.T) {
	db := &fakePGVectorStaleTargetDeleteDB{err: errors.New("delete failed for id secret-id using postgres://user:pass@localhost/db")}
	deleter := NewPGXPGVectorStaleTargetDeleter("postgres://[REDACTED]/db")
	deleter.db = db

	_, err := deleter.DeleteTargetRecords(context.Background(), "items", []string{"secret-id"})
	if err == nil {
		t.Fatal("DeleteTargetRecords returned nil error for exec failure")
	}
	if !strings.Contains(err.Error(), "delete stale pgvector target records") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "secret-id") || strings.Contains(err.Error(), "postgres://") || strings.Contains(err.Error(), "pass") {
		t.Fatalf("error leaked sensitive value: %v", err)
	}
}

func TestPGXPGVectorStaleTargetDeleterWrapsConnectErrorsWithoutLeakingConnectionURL(t *testing.T) {
	deleter := newPGXPGVectorStaleTargetDeleterWithConnector("postgres://user:pass@localhost/db", "id", func(context.Context, string) (pgvectorStaleTargetDeleteDB, func(), error) {
		return nil, nil, errors.New("dial postgres://user:pass@localhost/db failed")
	})

	_, err := deleter.DeleteTargetRecords(context.Background(), "items", []string{"sku-1"})
	if err == nil {
		t.Fatal("DeleteTargetRecords returned nil error for connect failure")
	}
	if !strings.Contains(err.Error(), "connect pgvector stale target deleter") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "postgres://") || strings.Contains(err.Error(), "pass") {
		t.Fatalf("error leaked connection URL: %v", err)
	}
}

type fakePGVectorStaleTargetDeleteDB struct {
	calls   []fakePGVectorStaleTargetDeleteCall
	deleted int64
	err     error
}

type fakePGVectorStaleTargetDeleteCall struct {
	sql  string
	args []any
}

func (db *fakePGVectorStaleTargetDeleteDB) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	db.calls = append(db.calls, fakePGVectorStaleTargetDeleteCall{sql: sql, args: append([]any(nil), args...)})
	return db.deleted, db.err
}
