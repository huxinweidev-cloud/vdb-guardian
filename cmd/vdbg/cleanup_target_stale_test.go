package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/migration"
)

func TestParseCleanupTargetStaleOptionsRequiresExplicitConfirmation(t *testing.T) {
	_, err := parseCleanupTargetStaleOptions([]string{
		"--reconcile-report", "report.json",
		"--pgvector-connection-url", "postgres://[REDACTED]",
		"--target-table", "items",
		"--output", "cleanup-result.json",
	})
	if err == nil || !strings.Contains(err.Error(), "confirm-delete-stale") {
		t.Fatalf("expected confirm-delete-stale error, got %v", err)
	}
}

func TestParseCleanupTargetStaleOptionsRequiresInputs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "report", args: []string{"--pgvector-connection-url", "postgres://[REDACTED]", "--target-table", "items", "--output", "out.json", "--confirm-delete-stale"}, want: "reconcile-report"},
		{name: "connection", args: []string{"--reconcile-report", "report.json", "--target-table", "items", "--output", "out.json", "--confirm-delete-stale"}, want: "pgvector-connection-url"},
		{name: "table", args: []string{"--reconcile-report", "report.json", "--pgvector-connection-url", "postgres://[REDACTED]", "--output", "out.json", "--confirm-delete-stale"}, want: "target-table"},
		{name: "output", args: []string{"--reconcile-report", "report.json", "--pgvector-connection-url", "postgres://[REDACTED]", "--target-table", "items", "--confirm-delete-stale"}, want: "output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCleanupTargetStaleOptions(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestRunCleanupTargetStaleCommandDeletesOnlyReportStaleIDsAndWrites0600Result(t *testing.T) {
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "reconcile.json")
	writeJSONFixture(t, reportPath, cleanupCLIReconciliationReportFixture([]string{"sku-2", "sku-1"}))
	resultPath := filepath.Join(dir, "cleanup-result.json")
	deleter := &recordingCleanupCLIDeleter{deleted: 2}

	err := runCleanupTargetStaleCommandWithDeleter([]string{
		"--reconcile-report", reportPath,
		"--pgvector-connection-url", "postgres://user:***@localhost/db",
		"--target-table", "items",
		"--target-id-column", "sku",
		"--output", resultPath,
		"--confirm-delete-stale",
	}, deleter)
	if err != nil {
		t.Fatalf("runCleanupTargetStaleCommandWithDeleter returned error: %v", err)
	}
	if deleter.connectionURL != "postgres://user:***@localhost/db" {
		t.Fatalf("deleter connectionURL = %q", deleter.connectionURL)
	}
	if deleter.idColumn != "sku" {
		t.Fatalf("deleter idColumn = %q", deleter.idColumn)
	}
	if deleter.table != "items" {
		t.Fatalf("deleter table = %q", deleter.table)
	}
	if got := strings.Join(deleter.ids, ","); got != "sku-1,sku-2" {
		t.Fatalf("deleter ids = %#v", deleter.ids)
	}
	info, err := os.Stat(resultPath)
	if err != nil {
		t.Fatalf("stat cleanup result: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("cleanup result mode = %#o", got)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read cleanup result: %v", err)
	}
	if strings.Contains(string(data), "postgres://") || strings.Contains(string(data), "pass") {
		t.Fatalf("cleanup result leaked connection URL: %s", data)
	}
	var result migration.TargetStaleCleanupResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode cleanup result: %v", err)
	}
	if result.RequestedDeleteCount != 2 || result.DeletedCount != 2 || strings.Join(result.DeletedStaleIDs, ",") != "sku-1,sku-2" {
		t.Fatalf("cleanup result = %#v", result)
	}
}

func TestRunCleanupTargetStaleCommandDoesNotCreateDeleterForInvalidReport(t *testing.T) {
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "reconcile.json")
	writeJSONFixture(t, reportPath, migration.TargetReconciliationReport{SchemaVersion: "unsupported"})
	deleter := &recordingCleanupCLIDeleter{}

	err := runCleanupTargetStaleCommandWithDeleter([]string{
		"--reconcile-report", reportPath,
		"--pgvector-connection-url", "postgres://user:***@localhost/db",
		"--target-table", "items",
		"--output", filepath.Join(dir, "cleanup.json"),
		"--confirm-delete-stale",
	}, deleter)
	if err == nil {
		t.Fatal("expected invalid report error")
	}
	if deleter.called {
		t.Fatal("deleter was called for invalid report")
	}
	if strings.Contains(err.Error(), "postgres://") || strings.Contains(err.Error(), "pass") {
		t.Fatalf("error leaked connection URL: %v", err)
	}
}

func cleanupCLIReconciliationReportFixture(staleIDs []string) migration.TargetReconciliationReport {
	matched := 3
	return migration.TargetReconciliationReport{
		SchemaVersion: migration.TargetReconciliationReportVersion,
		Status:        migration.TargetReconciliationStatusFail,
		Summary: migration.TargetReconciliationSummary{
			SourceRecordCount:  matched,
			TargetRecordCount:  matched + len(staleIDs),
			MatchedRecordCount: matched,
			StaleTargetCount:   len(staleIDs),
		},
		StaleTargetIDs: append([]string(nil), staleIDs...),
	}
}

type recordingCleanupCLIDeleter struct {
	connectionURL string
	idColumn      string
	called        bool
	table         string
	ids           []string
	deleted       int64
}

func (d *recordingCleanupCLIDeleter) New(connectionURL string, idColumn string) migration.TargetStaleDeleter {
	d.connectionURL = connectionURL
	d.idColumn = idColumn
	return d
}

func (d *recordingCleanupCLIDeleter) DeleteTargetRecords(ctx context.Context, table string, ids []string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	d.called = true
	d.table = table
	d.ids = append([]string(nil), ids...)
	return d.deleted, nil
}
