package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// TargetStaleDeleter deletes stale target records from a migration target table.
//
// DeleteTargetRecords receives the validated target table and the sorted list of
// stale target record IDs that are safe to delete according to a target
// reconciliation report. It returns the number of records deleted by the storage
// implementation, or an error if the destructive operation cannot be completed.
type TargetStaleDeleter interface {
	DeleteTargetRecords(ctx context.Context, table string, ids []string) (int64, error)
}

// TargetStaleCleanupRequest describes a guarded cleanup operation for stale
// records found only in the migration target.
//
// TargetTable is the storage table to delete from, Report is the target
// reconciliation contract that identifies stale target IDs, and
// ConfirmDeleteStale must be true before any destructive delete is attempted.
type TargetStaleCleanupRequest struct {
	TargetTable        string
	Report             TargetReconciliationReport
	ConfirmDeleteStale bool
}

// TargetStaleCleanupResult summarizes a stale target cleanup decision.
//
// RequestedDeleteCount is the number of stale IDs submitted for deletion,
// DeletedCount is the count returned by the deleter, and DryRun is true when the
// request did not perform DML because deletion was not confirmed or no stale IDs
// were present.
type TargetStaleCleanupResult struct {
	TargetTable          string   `json:"target_table"`
	RequestedDeleteCount int      `json:"requested_delete_count"`
	DeletedCount         int64    `json:"deleted_count"`
	DeletedStaleIDs      []string `json:"deleted_stale_ids"`
	DryRun               bool     `json:"dry_run"`
}

// CleanupStaleTargetRecords validates a target reconciliation report and deletes
// only the report's stale target IDs through the supplied deleter.
//
// The helper rejects unsupported report schema versions, inconsistent pass
// reports, stale count mismatches, missing target table names for destructive
// cleanup, missing confirmation for non-empty stale ID lists, and nil deleters
// when deletion is required. It sorts stale IDs before calling the deleter and
// returns the deleter's deleted count in the cleanup result.
func CleanupStaleTargetRecords(ctx context.Context, deleter TargetStaleDeleter, request TargetStaleCleanupRequest) (TargetStaleCleanupResult, error) {
	result := TargetStaleCleanupResult{
		TargetTable: request.TargetTable,
		DryRun:      !request.ConfirmDeleteStale,
	}
	if err := validateTargetStaleCleanupReport(request.Report); err != nil {
		return TargetStaleCleanupResult{}, err
	}

	staleIDs := append([]string(nil), request.Report.StaleTargetIDs...)
	if len(staleIDs) == 0 {
		return result, nil
	}
	if !request.ConfirmDeleteStale {
		return TargetStaleCleanupResult{}, fmt.Errorf("confirm stale target deletion before deleting %d records", len(staleIDs))
	}
	if strings.TrimSpace(request.TargetTable) == "" {
		return TargetStaleCleanupResult{}, fmt.Errorf("target table is required for stale target deletion")
	}
	if deleter == nil {
		return TargetStaleCleanupResult{}, fmt.Errorf("target stale deleter is required for stale target deletion")
	}

	sort.Strings(staleIDs)
	deletedCount, err := deleter.DeleteTargetRecords(ctx, request.TargetTable, staleIDs)
	if err != nil {
		return TargetStaleCleanupResult{}, fmt.Errorf("delete stale target records: %w", err)
	}
	result.RequestedDeleteCount = len(staleIDs)
	result.DeletedCount = deletedCount
	result.DeletedStaleIDs = append([]string(nil), staleIDs...)
	result.DryRun = false
	return result, nil
}

func validateTargetStaleCleanupReport(report TargetReconciliationReport) error {
	if report.SchemaVersion != TargetReconciliationReportVersion {
		return fmt.Errorf("unsupported target reconciliation report schema version %q", report.SchemaVersion)
	}
	if report.Status != TargetReconciliationStatusPass && report.Status != TargetReconciliationStatusFail {
		return fmt.Errorf("unsupported target reconciliation report status %q", report.Status)
	}
	if err := validateNonNegativeTargetReconciliationSummary(report.Summary); err != nil {
		return err
	}
	if report.Summary.StaleTargetCount != len(report.StaleTargetIDs) {
		return fmt.Errorf("stale target count %d does not match stale target ID list length %d", report.Summary.StaleTargetCount, len(report.StaleTargetIDs))
	}
	if report.Summary.MissingTargetCount != len(report.MissingTargetIDs) {
		return fmt.Errorf("missing target count %d does not match missing target ID list length %d", report.Summary.MissingTargetCount, len(report.MissingTargetIDs))
	}
	if report.Summary.ChangedRecordCount != len(report.ChangedRecordIDs) {
		return fmt.Errorf("changed record count %d does not match changed record ID list length %d", report.Summary.ChangedRecordCount, len(report.ChangedRecordIDs))
	}
	if report.Status == TargetReconciliationStatusPass && reportHasAnyDifferences(report) {
		return fmt.Errorf("target reconciliation pass report cannot contain stale, missing, or changed records")
	}
	if report.Status == TargetReconciliationStatusFail && !reportHasAnyDifferences(report) {
		return fmt.Errorf("target reconciliation fail report must contain stale, missing, or changed records")
	}
	if report.Summary.MatchedRecordCount+report.Summary.MissingTargetCount != report.Summary.SourceRecordCount {
		return fmt.Errorf("source record count does not match matched plus missing target records")
	}
	if report.Summary.MatchedRecordCount+report.Summary.StaleTargetCount != report.Summary.TargetRecordCount {
		return fmt.Errorf("target record count does not match matched plus stale target records")
	}
	return nil
}

func validateNonNegativeTargetReconciliationSummary(summary TargetReconciliationSummary) error {
	counts := map[string]int{
		"source record count":  summary.SourceRecordCount,
		"target record count":  summary.TargetRecordCount,
		"matched record count": summary.MatchedRecordCount,
		"stale target count":   summary.StaleTargetCount,
		"missing target count": summary.MissingTargetCount,
		"changed record count": summary.ChangedRecordCount,
	}
	for name, count := range counts {
		if count < 0 {
			return fmt.Errorf("%s cannot be negative", name)
		}
	}
	return nil
}

func reportHasAnyDifferences(report TargetReconciliationReport) bool {
	return report.Summary.StaleTargetCount != 0 ||
		report.Summary.MissingTargetCount != 0 ||
		report.Summary.ChangedRecordCount != 0 ||
		len(report.StaleTargetIDs) != 0 ||
		len(report.MissingTargetIDs) != 0 ||
		len(report.ChangedRecordIDs) != 0
}
