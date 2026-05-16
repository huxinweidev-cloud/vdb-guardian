package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/migration"
)

func TestParseReconcileTargetOptionsRequiresPaths(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "source", args: []string{"--target", "target.json", "--output", "report.json"}, want: "source full-record artifact path is required"},
		{name: "target", args: []string{"--source", "source.json", "--output", "report.json"}, want: "target full-record artifact path is required"},
		{name: "output", args: []string{"--source", "source.json", "--target", "target.json"}, want: "output path is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseReconcileTargetOptions(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestRunReconcileTargetReturnsErrorOnFailAndWritesReport0600(t *testing.T) {
	dir := t.TempDir()
	sourcePath := writeFullRecordArtifactFixture(t, dir, "source.json", "milvus", []migration.FullRecordArtifactRecord{
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
		{ID: "sku-2", VectorHash: "sha256:222", VectorDimension: 8},
		{ID: "sku-3", VectorHash: "sha256:333", VectorDimension: 8},
		{ID: "sku-4", VectorHash: "sha256:444", VectorDimension: 8},
	})
	targetPath := writeFullRecordArtifactFixture(t, dir, "target.json", "pgvector", []migration.FullRecordArtifactRecord{
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
		{ID: "sku-2", VectorHash: "sha256:changed", VectorDimension: 8},
		{ID: "sku-stale-1", VectorHash: "sha256:stale1", VectorDimension: 8},
		{ID: "sku-stale-2", VectorHash: "sha256:stale2", VectorDimension: 8},
		{ID: "sku-stale-3", VectorHash: "sha256:stale3", VectorDimension: 8},
	})
	outputPath := filepath.Join(dir, "nested", "reconcile.json")

	err := runReconcileTargetCommand([]string{"--source", sourcePath, "--target", targetPath, "--output", outputPath})
	if err == nil || !strings.Contains(err.Error(), "target reconciliation failed") {
		t.Fatalf("expected target reconciliation failure, got %v", err)
	}
	info, statErr := os.Stat(outputPath)
	if statErr != nil {
		t.Fatalf("stat output after failure: %v", statErr)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("output mode = %#o", got)
	}
	var report migration.TargetReconciliationReport
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read output after failure: %v", readErr)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != migration.TargetReconciliationStatusFail {
		t.Fatalf("status = %q", report.Status)
	}
	if report.Summary.StaleTargetCount != 3 || report.Summary.MissingTargetCount != 2 || report.Summary.ChangedRecordCount != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestRunReconcileTargetReturnsNilForMatchingArtifacts(t *testing.T) {
	dir := t.TempDir()
	records := []migration.FullRecordArtifactRecord{
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
		{ID: "sku-2", VectorHash: "sha256:222", VectorDimension: 8},
	}
	sourcePath := writeFullRecordArtifactFixture(t, dir, "source.json", "milvus", records)
	targetPath := writeFullRecordArtifactFixture(t, dir, "target.json", "pgvector", records)
	outputPath := filepath.Join(dir, "reconcile.json")

	if err := runReconcileTargetCommand([]string{"--source", sourcePath, "--target", targetPath, "--output", outputPath}); err != nil {
		t.Fatalf("runReconcileTargetCommand returned error: %v", err)
	}
	var report migration.TargetReconciliationReport
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != migration.TargetReconciliationStatusPass || report.Summary.StaleTargetCount != 0 || report.Summary.MissingTargetCount != 0 || report.Summary.ChangedRecordCount != 0 {
		t.Fatalf("report = %#v", report)
	}
}
