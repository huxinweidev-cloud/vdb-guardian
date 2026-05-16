package migration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCleanupStaleTargetRecordsRejectsWithoutConfirmationWhenStaleIDsExist(t *testing.T) {
	deleter := &recordingTargetStaleDeleter{}
	request := targetStaleCleanupRequestFixture([]string{"sku-stale"})
	request.ConfirmDeleteStale = false

	result, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
	if err == nil {
		t.Fatal("CleanupStaleTargetRecords returned nil error without confirmation")
	}
	if !strings.Contains(err.Error(), "confirm") {
		t.Fatalf("error = %v", err)
	}
	if deleter.called {
		t.Fatal("deleter was called without confirmation")
	}
	if result.DeletedCount != 0 || result.RequestedDeleteCount != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCleanupStaleTargetRecordsNoopsWhenStaleIDListEmpty(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		t.Run(confirmName(confirm), func(t *testing.T) {
			deleter := &recordingTargetStaleDeleter{}
			request := targetStaleCleanupRequestFixture(nil)
			request.ConfirmDeleteStale = confirm

			result, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
			if err != nil {
				t.Fatalf("CleanupStaleTargetRecords returned error: %v", err)
			}
			if deleter.called {
				t.Fatal("deleter was called for empty stale ID list")
			}
			want := TargetStaleCleanupResult{
				TargetTable:          "public.items",
				RequestedDeleteCount: 0,
				DeletedCount:         0,
				DryRun:               !confirm,
			}
			if !reflect.DeepEqual(result, want) {
				t.Fatalf("result = %#v, want %#v", result, want)
			}
		})
	}
}

func TestCleanupStaleTargetRecordsRejectsUnsupportedReportSchemaVersion(t *testing.T) {
	deleter := &recordingTargetStaleDeleter{}
	request := targetStaleCleanupRequestFixture([]string{"sku-stale"})
	request.Report.SchemaVersion = "v0"

	_, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
	if err == nil {
		t.Fatal("CleanupStaleTargetRecords returned nil error for unsupported schema version")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error = %v", err)
	}
	if deleter.called {
		t.Fatal("deleter was called for invalid report schema")
	}
}

func TestCleanupStaleTargetRecordsRejectsInconsistentPassReport(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TargetReconciliationReport)
	}{
		{
			name: "pass with stale ids",
			mutate: func(report *TargetReconciliationReport) {
				report.Summary.StaleTargetCount = 1
				report.StaleTargetIDs = []string{"sku-stale"}
			},
		},
		{
			name: "pass with missing ids",
			mutate: func(report *TargetReconciliationReport) {
				report.Summary.MissingTargetCount = 1
				report.MissingTargetIDs = []string{"sku-missing"}
			},
		},
		{
			name: "pass with changed ids",
			mutate: func(report *TargetReconciliationReport) {
				report.Summary.ChangedRecordCount = 1
				report.ChangedRecordIDs = []string{"sku-changed"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleter := &recordingTargetStaleDeleter{}
			request := targetStaleCleanupRequestFixture(nil)
			request.Report.Status = TargetReconciliationStatusPass
			tt.mutate(&request.Report)

			_, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
			if err == nil {
				t.Fatal("CleanupStaleTargetRecords returned nil error for inconsistent pass report")
			}
			if !strings.Contains(err.Error(), "pass") {
				t.Fatalf("error = %v", err)
			}
			if deleter.called {
				t.Fatal("deleter was called for inconsistent pass report")
			}
		})
	}
}

func TestCleanupStaleTargetRecordsRejectsStaleCountMismatch(t *testing.T) {
	deleter := &recordingTargetStaleDeleter{}
	request := targetStaleCleanupRequestFixture([]string{"sku-stale-1", "sku-stale-2"})
	request.Report.Summary.StaleTargetCount = 1

	_, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
	if err == nil {
		t.Fatal("CleanupStaleTargetRecords returned nil error for stale count mismatch")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("error = %v", err)
	}
	if deleter.called {
		t.Fatal("deleter was called for stale count mismatch")
	}
}

func TestCleanupStaleTargetRecordsRejectsMalformedReportsBeforeDeletion(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*TargetStaleCleanupRequest)
		wantError string
	}{
		{
			name: "unknown status",
			mutate: func(request *TargetStaleCleanupRequest) {
				request.Report.Status = "unknown"
			},
			wantError: "status",
		},
		{
			name: "fail with no differences",
			mutate: func(request *TargetStaleCleanupRequest) {
				request.Report = targetReconciliationPassReportFixture()
				request.Report.Status = TargetReconciliationStatusFail
			},
			wantError: "fail",
		},
		{
			name: "missing target count mismatch",
			mutate: func(request *TargetStaleCleanupRequest) {
				request.Report.MissingTargetIDs = []string{"sku-missing"}
				request.Report.Summary.MissingTargetCount = 2
				request.Report.Summary.SourceRecordCount = request.Report.Summary.MatchedRecordCount + request.Report.Summary.MissingTargetCount
			},
			wantError: "missing",
		},
		{
			name: "changed record count mismatch",
			mutate: func(request *TargetStaleCleanupRequest) {
				request.Report.ChangedRecordIDs = []string{"sku-changed"}
				request.Report.Summary.ChangedRecordCount = 2
			},
			wantError: "changed",
		},
		{
			name: "negative count",
			mutate: func(request *TargetStaleCleanupRequest) {
				request.Report.Summary.MatchedRecordCount = -1
			},
			wantError: "negative",
		},
		{
			name: "source aggregate mismatch",
			mutate: func(request *TargetStaleCleanupRequest) {
				request.Report.Summary.SourceRecordCount = 99
			},
			wantError: "source record count",
		},
		{
			name: "target aggregate mismatch",
			mutate: func(request *TargetStaleCleanupRequest) {
				request.Report.Summary.TargetRecordCount = 99
				request.Report.Summary.SourceRecordCount = request.Report.Summary.MatchedRecordCount + request.Report.Summary.MissingTargetCount
			},
			wantError: "target record count",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleter := &recordingTargetStaleDeleter{}
			request := targetStaleCleanupRequestFixture([]string{"sku-stale"})
			tt.mutate(&request)

			_, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
			if err == nil {
				t.Fatal("CleanupStaleTargetRecords returned nil error for malformed report")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantError)
			}
			if deleter.called {
				t.Fatal("deleter was called for malformed report")
			}
		})
	}
}

func TestCleanupStaleTargetRecordsRejectsMissingDeletionPrerequisites(t *testing.T) {
	tests := []struct {
		name      string
		deleter   TargetStaleDeleter
		mutate    func(*TargetStaleCleanupRequest)
		wantError string
	}{
		{
			name:      "nil deleter",
			deleter:   nil,
			mutate:    func(*TargetStaleCleanupRequest) {},
			wantError: "deleter",
		},
		{
			name:    "empty target table",
			deleter: &recordingTargetStaleDeleter{},
			mutate: func(request *TargetStaleCleanupRequest) {
				request.TargetTable = ""
			},
			wantError: "target table",
		},
		{
			name:    "whitespace target table",
			deleter: &recordingTargetStaleDeleter{},
			mutate: func(request *TargetStaleCleanupRequest) {
				request.TargetTable = "  \t"
			},
			wantError: "target table",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := targetStaleCleanupRequestFixture([]string{"sku-stale"})
			tt.mutate(&request)

			_, err := CleanupStaleTargetRecords(context.Background(), tt.deleter, request)
			if err == nil {
				t.Fatal("CleanupStaleTargetRecords returned nil error for missing deletion prerequisite")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestCleanupStaleTargetRecordsDeletesOnlyStaleTargetIDs(t *testing.T) {
	deleter := &recordingTargetStaleDeleter{deletedCount: 2}
	request := targetStaleCleanupRequestFixture([]string{"sku-stale-2", "sku-stale-1"})
	request.Report.MissingTargetIDs = []string{"sku-missing"}
	request.Report.ChangedRecordIDs = []string{"sku-changed"}
	request.Report.Summary.MissingTargetCount = 1
	request.Report.Summary.ChangedRecordCount = 1
	request.Report.Summary.SourceRecordCount = request.Report.Summary.MatchedRecordCount + request.Report.Summary.MissingTargetCount

	_, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
	if err != nil {
		t.Fatalf("CleanupStaleTargetRecords returned error: %v", err)
	}
	if !reflect.DeepEqual(deleter.ids, []string{"sku-stale-1", "sku-stale-2"}) {
		t.Fatalf("deleted ids = %#v", deleter.ids)
	}
	for _, forbidden := range []string{"sku-missing", "sku-changed"} {
		if containsCleanupString(deleter.ids, forbidden) {
			t.Fatalf("deleted non-stale id %q from ids %#v", forbidden, deleter.ids)
		}
	}
}

func TestCleanupStaleTargetRecordsPreservesStableSortedIDOrder(t *testing.T) {
	deleter := &recordingTargetStaleDeleter{deletedCount: 4}
	request := targetStaleCleanupRequestFixture([]string{"sku-2", "sku-10", "sku-1", "sku-2"})

	_, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
	if err != nil {
		t.Fatalf("CleanupStaleTargetRecords returned error: %v", err)
	}
	want := []string{"sku-1", "sku-10", "sku-2", "sku-2"}
	if !reflect.DeepEqual(deleter.ids, want) {
		t.Fatalf("deleted ids = %#v, want %#v", deleter.ids, want)
	}
}

func TestCleanupStaleTargetRecordsReturnsDeletedCount(t *testing.T) {
	deleter := &recordingTargetStaleDeleter{deletedCount: 2}
	request := targetStaleCleanupRequestFixture([]string{"sku-stale-2", "sku-stale-1"})

	result, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
	if err != nil {
		t.Fatalf("CleanupStaleTargetRecords returned error: %v", err)
	}
	want := TargetStaleCleanupResult{
		TargetTable:          "public.items",
		RequestedDeleteCount: 2,
		DeletedCount:         2,
		DeletedStaleIDs:      []string{"sku-stale-1", "sku-stale-2"},
		DryRun:               false,
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("result = %#v, want %#v", result, want)
	}
}

func TestCleanupStaleTargetRecordsPropagatesDeleteError(t *testing.T) {
	deleter := &recordingTargetStaleDeleter{err: errors.New("delete failed")}
	request := targetStaleCleanupRequestFixture([]string{"sku-stale"})

	_, err := CleanupStaleTargetRecords(context.Background(), deleter, request)
	if err == nil {
		t.Fatal("CleanupStaleTargetRecords returned nil error for deleter failure")
	}
	if !strings.Contains(err.Error(), "delete stale target records") {
		t.Fatalf("error = %v", err)
	}
}

func targetStaleCleanupRequestFixture(staleIDs []string) TargetStaleCleanupRequest {
	ids := append([]string(nil), staleIDs...)
	status := TargetReconciliationStatusPass
	if len(ids) > 0 {
		status = TargetReconciliationStatusFail
	}
	matchedRecords := 3
	return TargetStaleCleanupRequest{
		TargetTable:        "public.items",
		ConfirmDeleteStale: true,
		Report: TargetReconciliationReport{
			SchemaVersion: TargetReconciliationReportVersion,
			Status:        status,
			Summary: TargetReconciliationSummary{
				SourceRecordCount:  matchedRecords,
				TargetRecordCount:  matchedRecords + len(ids),
				MatchedRecordCount: matchedRecords,
				StaleTargetCount:   len(ids),
			},
			StaleTargetIDs: ids,
		},
	}
}

func targetReconciliationPassReportFixture() TargetReconciliationReport {
	return TargetReconciliationReport{
		SchemaVersion: TargetReconciliationReportVersion,
		Status:        TargetReconciliationStatusPass,
		Summary: TargetReconciliationSummary{
			SourceRecordCount:  3,
			TargetRecordCount:  3,
			MatchedRecordCount: 3,
		},
	}
}

func confirmName(confirm bool) string {
	if confirm {
		return "confirmed"
	}
	return "unconfirmed"
}

func containsCleanupString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type recordingTargetStaleDeleter struct {
	called       bool
	table        string
	ids          []string
	deletedCount int64
	err          error
}

func (d *recordingTargetStaleDeleter) DeleteTargetRecords(ctx context.Context, table string, ids []string) (int64, error) {
	d.called = true
	d.table = table
	d.ids = append([]string(nil), ids...)
	return d.deletedCount, d.err
}
