package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// TargetReconciliationReportVersion is the current schema version for target
	// reconciliation reports that summarize whether the migrated target artifact
	// still matches the source artifact.
	TargetReconciliationReportVersion = "v1"
	// TargetReconciliationStatusPass means the target artifact has no stale,
	// missing, or changed records relative to the source artifact.
	TargetReconciliationStatusPass = "pass"
	// TargetReconciliationStatusFail means the target artifact contains at least
	// one stale, missing, or changed record relative to the source artifact.
	TargetReconciliationStatusFail = "fail"
)

// TargetReconciliationReport is the stable machine-readable output for deciding
// whether a target collection is reconciled with its source full-record artifact.
//
// StaleTargetIDs are records present only in the target, MissingTargetIDs are
// source records absent from the target, and ChangedRecordIDs are matched record
// IDs whose scalar, metadata, partition, or vector fields differ.
type TargetReconciliationReport struct {
	SchemaVersion    string                      `json:"schema_version"`
	Status           string                      `json:"status"`
	Source           FullRecordCompareEndpoint   `json:"source"`
	Target           FullRecordCompareEndpoint   `json:"target"`
	Summary          TargetReconciliationSummary `json:"summary"`
	StaleTargetIDs   []string                    `json:"stale_target_ids"`
	MissingTargetIDs []string                    `json:"missing_target_ids"`
	ChangedRecordIDs []string                    `json:"changed_record_ids"`
	Mismatches       []FullRecordMismatch        `json:"mismatches,omitempty"`
}

// TargetReconciliationSummary contains aggregate counters for a target
// reconciliation report.
//
// SourceRecordCount and TargetRecordCount come from the compared artifacts,
// MatchedRecordCount counts IDs present on both sides, StaleTargetCount counts
// target-only IDs, MissingTargetCount counts source-only IDs, and
// ChangedRecordCount counts matched IDs with one or more field mismatches.
type TargetReconciliationSummary struct {
	SourceRecordCount  int `json:"source_record_count"`
	TargetRecordCount  int `json:"target_record_count"`
	MatchedRecordCount int `json:"matched_record_count"`
	StaleTargetCount   int `json:"stale_target_count"`
	MissingTargetCount int `json:"missing_target_count"`
	ChangedRecordCount int `json:"changed_record_count"`
}

// BuildTargetReconciliationReport compares source and target full-record
// artifacts and maps the detailed comparison report into target reconciliation
// terminology.
//
// It returns an error when CompareFullRecordArtifacts rejects either artifact,
// such as unsupported schema versions or duplicate record IDs. The report status
// is pass only when there are no stale target records, missing target records, or
// changed matched records.
func BuildTargetReconciliationReport(source, target FullRecordArtifact) (TargetReconciliationReport, error) {
	compareReport, err := CompareFullRecordArtifacts(source, target)
	if err != nil {
		return TargetReconciliationReport{}, err
	}

	changedRecordIDs := changedRecordIDsFromMismatches(compareReport.Mismatches)
	report := TargetReconciliationReport{
		SchemaVersion: TargetReconciliationReportVersion,
		Status:        TargetReconciliationStatusPass,
		Source:        compareReport.Source,
		Target:        compareReport.Target,
		Summary: TargetReconciliationSummary{
			SourceRecordCount:  compareReport.Source.RecordCount,
			TargetRecordCount:  compareReport.Target.RecordCount,
			MatchedRecordCount: compareReport.Summary.MatchedRecords,
			StaleTargetCount:   len(compareReport.MissingSourceIDs),
			MissingTargetCount: len(compareReport.MissingTargetIDs),
			ChangedRecordCount: len(changedRecordIDs),
		},
		StaleTargetIDs:   append([]string(nil), compareReport.MissingSourceIDs...),
		MissingTargetIDs: append([]string(nil), compareReport.MissingTargetIDs...),
		ChangedRecordIDs: changedRecordIDs,
		Mismatches:       append([]FullRecordMismatch(nil), compareReport.Mismatches...),
	}
	if report.Summary.StaleTargetCount > 0 || report.Summary.MissingTargetCount > 0 || report.Summary.ChangedRecordCount > 0 {
		report.Status = TargetReconciliationStatusFail
	}
	return report, nil
}

// MarshalTargetReconciliationReport renders a target reconciliation report as
// stable, indented JSON suitable for persisting as a migration artifact.
//
// It returns the encoded JSON with a trailing newline. An error is returned when
// any report field cannot be represented as JSON.
func MarshalTargetReconciliationReport(report TargetReconciliationReport) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal target reconciliation report: %w", err)
	}
	return append(data, '\n'), nil
}

// WriteTargetReconciliationReport writes a target reconciliation report to path
// using restrictive file permissions for safe local artifact persistence.
//
// It creates missing parent directories with 0755 permissions and writes the
// report via a same-directory 0600 temporary file that is atomically renamed into
// place. Existing non-regular paths, including symlinks and directories, are
// rejected. An error is returned if marshaling, parent directory creation,
// writing, or permission tightening fails.
func WriteTargetReconciliationReport(path string, report TargetReconciliationReport) error {
	data, err := MarshalTargetReconciliationReport(report)
	if err != nil {
		return err
	}
	return WriteSensitiveJSONFile0600(path, data, "target reconciliation report")
}

// WriteSensitiveJSONFile0600 writes sensitive JSON bytes via an atomic
// same-directory temporary file and leaves the destination with 0600
// permissions. Existing non-regular paths, including symlinks and directories,
// are rejected before write.
func WriteSensitiveJSONFile0600(path string, data []byte, label string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s directory: %w", label, err)
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s path is non-regular: %s", label, path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", label, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-sensitive-json-*")
	if err != nil {
		return fmt.Errorf("create temporary %s: %w", label, err)
	}
	tmpPath := tmp.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary %s: %w", label, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary %s: %w", label, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary %s: %w", label, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", label, err)
	}
	cleanupTemp = false
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", label, err)
	}
	return nil
}

func changedRecordIDsFromMismatches(mismatches []FullRecordMismatch) []string {
	seen := make(map[string]struct{}, len(mismatches))
	ids := make([]string, 0, len(mismatches))
	for _, mismatch := range mismatches {
		if _, ok := seen[mismatch.ID]; ok {
			continue
		}
		seen[mismatch.ID] = struct{}{}
		ids = append(ids, mismatch.ID)
	}
	return ids
}
