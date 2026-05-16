package migration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTargetReconciliationReportFailsForStaleMissingAndChangedRecords(t *testing.T) {
	source := fullRecordArtifactFixture("milvus", []FullRecordArtifactRecord{
		{ID: "sku-3", VectorHash: "sha256:333", VectorDimension: 8},
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
		{ID: "sku-2", VectorHash: "sha256:222", VectorDimension: 8},
	})
	target := fullRecordArtifactFixture("pgvector", []FullRecordArtifactRecord{
		{ID: "sku-stale", VectorHash: "sha256:stale", VectorDimension: 8},
		{ID: "sku-3", VectorHash: "sha256:changed", VectorDimension: 8},
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
	})

	report, err := BuildTargetReconciliationReport(source, target)
	if err != nil {
		t.Fatalf("BuildTargetReconciliationReport returned error: %v", err)
	}
	if report.Status != TargetReconciliationStatusFail {
		t.Fatalf("status = %q", report.Status)
	}
	if report.Summary.SourceRecordCount != 3 || report.Summary.TargetRecordCount != 3 || report.Summary.MatchedRecordCount != 2 || report.Summary.StaleTargetCount != 1 || report.Summary.MissingTargetCount != 1 || report.Summary.ChangedRecordCount != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if report.Source.System != "milvus" || report.Source.RecordCount != 3 {
		t.Fatalf("source endpoint = %#v", report.Source)
	}
	if report.Target.System != "pgvector" || report.Target.RecordCount != 3 {
		t.Fatalf("target endpoint = %#v", report.Target)
	}
	if !equalStringSlices(report.StaleTargetIDs, []string{"sku-stale"}) {
		t.Fatalf("stale target ids = %#v", report.StaleTargetIDs)
	}
	if !equalStringSlices(report.MissingTargetIDs, []string{"sku-2"}) {
		t.Fatalf("missing target ids = %#v", report.MissingTargetIDs)
	}
	if !equalStringSlices(report.ChangedRecordIDs, []string{"sku-3"}) {
		t.Fatalf("changed record ids = %#v", report.ChangedRecordIDs)
	}
	if len(report.Mismatches) != 1 || report.Mismatches[0].ID != "sku-3" || report.Mismatches[0].FieldPath != "vector_hash" {
		t.Fatalf("mismatches = %#v", report.Mismatches)
	}
}

func TestBuildTargetReconciliationReportPassesForMatchingArtifacts(t *testing.T) {
	source := fullRecordArtifactFixture("milvus", []FullRecordArtifactRecord{
		{ID: "sku-2", VectorHash: "sha256:222", VectorDimension: 8, Scalars: map[string]any{"title": "Second"}},
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8, Scalars: map[string]any{"title": "First"}},
	})
	target := fullRecordArtifactFixture("pgvector", []FullRecordArtifactRecord{
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8, Scalars: map[string]any{"title": "First"}},
		{ID: "sku-2", VectorHash: "sha256:222", VectorDimension: 8, Scalars: map[string]any{"title": "Second"}},
	})

	report, err := BuildTargetReconciliationReport(source, target)
	if err != nil {
		t.Fatalf("BuildTargetReconciliationReport returned error: %v", err)
	}
	if report.SchemaVersion != TargetReconciliationReportVersion {
		t.Fatalf("schema version = %q", report.SchemaVersion)
	}
	if report.Status != TargetReconciliationStatusPass {
		t.Fatalf("status = %q", report.Status)
	}
	if report.Summary.SourceRecordCount != 2 || report.Summary.TargetRecordCount != 2 || report.Summary.MatchedRecordCount != 2 || report.Summary.StaleTargetCount != 0 || report.Summary.MissingTargetCount != 0 || report.Summary.ChangedRecordCount != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if len(report.StaleTargetIDs) != 0 || len(report.MissingTargetIDs) != 0 || len(report.ChangedRecordIDs) != 0 {
		t.Fatalf("unexpected reconciliation differences: %#v", report)
	}
	if len(report.Mismatches) != 0 {
		t.Fatalf("mismatches = %#v", report.Mismatches)
	}
}

func TestMarshalTargetReconciliationReportEmitsIndentedJSONWithTrailingNewline(t *testing.T) {
	report := targetReconciliationReportFixture()

	data, err := MarshalTargetReconciliationReport(report)
	if err != nil {
		t.Fatalf("MarshalTargetReconciliationReport returned error: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("marshaled report does not end with newline: %q", data)
	}
	if !bytes.Contains(data, []byte("\n  \"schema_version\": \"v1\",")) {
		t.Fatalf("marshaled report is not indented JSON:\n%s", data)
	}
	var decoded TargetReconciliationReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if decoded.SchemaVersion != TargetReconciliationReportVersion || decoded.Status != TargetReconciliationStatusFail || decoded.Summary.ChangedRecordCount != 1 {
		t.Fatalf("decoded report = %#v", decoded)
	}
}

func TestMarshalTargetReconciliationReportExcludesConnectionStringFields(t *testing.T) {
	data, err := MarshalTargetReconciliationReport(targetReconciliationReportFixture())
	if err != nil {
		t.Fatalf("MarshalTargetReconciliationReport returned error: %v", err)
	}
	jsonText := string(data)
	for _, forbidden := range []string{"connection_url", "pgvector_connection_url"} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("marshaled report contains forbidden connection-string field %q:\n%s", forbidden, jsonText)
		}
	}
}

func TestWriteTargetReconciliationReportCreatesParentsAndWrites0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "reports", "target-reconciliation.json")

	if err := WriteTargetReconciliationReport(path, targetReconciliationReportFixture()); err != nil {
		t.Fatalf("WriteTargetReconciliationReport returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written report: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("written report permissions = %o, want 0600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written report: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("written report does not end with newline: %q", data)
	}
}

func TestWriteTargetReconciliationReportTightensExistingBroadPermissionFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target-reconciliation.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o666); err != nil {
		t.Fatalf("seed broad-permission file: %v", err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("chmod broad-permission file: %v", err)
	}

	if err := WriteTargetReconciliationReport(path, targetReconciliationReportFixture()); err != nil {
		t.Fatalf("WriteTargetReconciliationReport returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written report: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("written report permissions = %o, want 0600", got)
	}
}

func TestWriteTargetReconciliationReportRejectsNonRegularExistingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target-reconciliation.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("seed directory at report path: %v", err)
	}

	err := WriteTargetReconciliationReport(path, targetReconciliationReportFixture())
	if err == nil {
		t.Fatal("WriteTargetReconciliationReport returned nil error for directory path")
	}
	if !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("error = %v", err)
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat directory path: %v", statErr)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("directory permissions = %o, want unchanged 0755", got)
	}
}

func TestWriteTargetReconciliationReportRejectsSymlinkPath(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(targetPath, []byte("do not overwrite"), 0o644); err != nil {
		t.Fatalf("seed symlink target: %v", err)
	}
	symlinkPath := filepath.Join(dir, "target-reconciliation.json")
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink not permitted: %v", err)
		}
		t.Fatalf("seed symlink: %v", err)
	}

	err := WriteTargetReconciliationReport(symlinkPath, targetReconciliationReportFixture())
	if err == nil {
		t.Fatal("WriteTargetReconciliationReport returned nil error for symlink path")
	}
	if !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("error = %v", err)
	}
	data, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("read symlink target: %v", readErr)
	}
	if string(data) != "do not overwrite" {
		t.Fatalf("symlink target was overwritten: %q", data)
	}
}

func targetReconciliationReportFixture() TargetReconciliationReport {
	return TargetReconciliationReport{
		SchemaVersion: TargetReconciliationReportVersion,
		Status:        TargetReconciliationStatusFail,
		Source:        FullRecordCompareEndpoint{System: "milvus", Collection: "items", RecordCount: 2},
		Target:        FullRecordCompareEndpoint{System: "pgvector", Collection: "items", RecordCount: 2},
		Summary: TargetReconciliationSummary{
			SourceRecordCount:  2,
			TargetRecordCount:  2,
			MatchedRecordCount: 1,
			StaleTargetCount:   1,
			MissingTargetCount: 1,
			ChangedRecordCount: 1,
		},
		StaleTargetIDs:   []string{"sku-stale"},
		MissingTargetIDs: []string{"sku-missing"},
		ChangedRecordIDs: []string{"sku-changed"},
		Mismatches: []FullRecordMismatch{{
			ID:          "sku-changed",
			FieldPath:   "vector_hash",
			SourceValue: "sha256:source",
			TargetValue: "sha256:target",
		}},
	}
}
