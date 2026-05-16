package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/migration"
)

func TestParseCompareFullRecordsOptionsRequiresPaths(t *testing.T) {
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
			_, err := parseCompareFullRecordsOptions(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestRunCompareFullRecordsWritesReport0600(t *testing.T) {
	dir := t.TempDir()
	sourcePath := writeFullRecordArtifactFixture(t, dir, "source.json", "milvus", []migration.FullRecordArtifactRecord{{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8}})
	targetPath := writeFullRecordArtifactFixture(t, dir, "target.json", "pgvector", []migration.FullRecordArtifactRecord{{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8}})
	outputPath := filepath.Join(dir, "nested", "report.json")

	err := runCompareFullRecords([]string{"--source", sourcePath, "--target", targetPath, "--output", outputPath})
	if err != nil {
		t.Fatalf("runCompareFullRecords returned error: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("output mode = %#o", got)
	}
	var report migration.FullRecordCompareReport
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != migration.FullRecordCompareStatusPass || report.Summary.MatchedRecords != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestRunCompareFullRecordsReturnsErrorOnMismatch(t *testing.T) {
	dir := t.TempDir()
	sourcePath := writeFullRecordArtifactFixture(t, dir, "source.json", "milvus", []migration.FullRecordArtifactRecord{{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8}})
	targetPath := writeFullRecordArtifactFixture(t, dir, "target.json", "pgvector", []migration.FullRecordArtifactRecord{{ID: "sku-1", VectorHash: "sha256:222", VectorDimension: 8}})
	outputPath := filepath.Join(dir, "report.json")

	err := runCompareFullRecords([]string{"--source", sourcePath, "--target", targetPath, "--output", outputPath})
	if err == nil || !strings.Contains(err.Error(), "full-record comparison failed") {
		t.Fatalf("expected comparison failure, got %v", err)
	}
	var report migration.FullRecordCompareReport
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read output after failure: %v", readErr)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != migration.FullRecordCompareStatusFail || report.Summary.VectorMismatches != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func writeFullRecordArtifactFixture(t *testing.T, dir, name, system string, records []migration.FullRecordArtifactRecord) string {
	t.Helper()
	path := filepath.Join(dir, name)
	artifact := migration.FullRecordArtifact{
		SchemaVersion: migration.FullRecordArtifactVersion,
		System:        system,
		Collection:    "items",
		Records:       records,
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("marshal artifact: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return path
}
