package migration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompareFullRecordArtifactsPassesForEqualRecords(t *testing.T) {
	source := fullRecordArtifactFixture("milvus", []FullRecordArtifactRecord{
		{
			ID:              "sku-2",
			VectorHash:      "sha256:222",
			VectorDimension: 8,
			Scalars:         map[string]any{"title": "Second", "price": 12.5, "active": true},
			DynamicMetadata: map[string]any{"brand": "acme", "tags": []any{"sale", "featured"}},
			Partition:       "tenant_b",
		},
		{
			ID:              "sku-1",
			VectorHash:      "sha256:111",
			VectorDimension: 8,
			Scalars:         map[string]any{"title": "First", "price": 9.5, "active": true},
			DynamicMetadata: map[string]any{"brand": "acme", "tags": []any{"sale"}},
			Partition:       "tenant_a",
		},
	})
	target := fullRecordArtifactFixture("pgvector", []FullRecordArtifactRecord{
		{
			ID:              "sku-1",
			VectorHash:      "sha256:111",
			VectorDimension: 8,
			Scalars:         map[string]any{"title": "First", "price": 9.5, "active": true},
			DynamicMetadata: map[string]any{"brand": "acme", "tags": []any{"sale"}},
			Partition:       "tenant_a",
		},
		{
			ID:              "sku-2",
			VectorHash:      "sha256:222",
			VectorDimension: 8,
			Scalars:         map[string]any{"title": "Second", "price": 12.5, "active": true},
			DynamicMetadata: map[string]any{"brand": "acme", "tags": []any{"sale", "featured"}},
			Partition:       "tenant_b",
		},
	})

	report, err := CompareFullRecordArtifacts(source, target)
	if err != nil {
		t.Fatalf("CompareFullRecordArtifacts returned error: %v", err)
	}
	if report.Status != FullRecordCompareStatusPass {
		t.Fatalf("status = %q", report.Status)
	}
	if report.Summary.MatchedRecords != 2 || report.Summary.MismatchedRecords != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if len(report.Mismatches) != 0 || len(report.MissingSourceIDs) != 0 || len(report.MissingTargetIDs) != 0 {
		t.Fatalf("unexpected differences: %#v", report)
	}
}

func TestCompareFullRecordArtifactsReportsMissingAndExtraRows(t *testing.T) {
	source := fullRecordArtifactFixture("milvus", []FullRecordArtifactRecord{
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
		{ID: "sku-2", VectorHash: "sha256:222", VectorDimension: 8},
	})
	target := fullRecordArtifactFixture("pgvector", []FullRecordArtifactRecord{
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
		{ID: "sku-3", VectorHash: "sha256:333", VectorDimension: 8},
	})

	report, err := CompareFullRecordArtifacts(source, target)
	if err != nil {
		t.Fatalf("CompareFullRecordArtifacts returned error: %v", err)
	}
	if report.Status != FullRecordCompareStatusFail {
		t.Fatalf("status = %q", report.Status)
	}
	if !equalStringSlices(report.MissingTargetIDs, []string{"sku-2"}) {
		t.Fatalf("missing target ids = %#v", report.MissingTargetIDs)
	}
	if !equalStringSlices(report.MissingSourceIDs, []string{"sku-3"}) {
		t.Fatalf("missing source ids = %#v", report.MissingSourceIDs)
	}
	if report.Summary.MissingTargetRecords != 1 || report.Summary.MissingSourceRecords != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestCompareFullRecordArtifactsReportsFieldMismatches(t *testing.T) {
	source := fullRecordArtifactFixture("milvus", []FullRecordArtifactRecord{{
		ID:              "sku-1",
		VectorHash:      "sha256:111",
		VectorDimension: 8,
		Scalars:         map[string]any{"title": "First", "price": 9.5},
		DynamicMetadata: map[string]any{"brand": "acme"},
		Partition:       "tenant_a",
	}})
	target := fullRecordArtifactFixture("pgvector", []FullRecordArtifactRecord{{
		ID:              "sku-1",
		VectorHash:      "sha256:222",
		VectorDimension: 7,
		Scalars:         map[string]any{"title": "Changed", "price": 9.5},
		DynamicMetadata: map[string]any{"brand": "other"},
		Partition:       "tenant_b",
	}})

	report, err := CompareFullRecordArtifacts(source, target)
	if err != nil {
		t.Fatalf("CompareFullRecordArtifacts returned error: %v", err)
	}
	if report.Status != FullRecordCompareStatusFail {
		t.Fatalf("status = %q", report.Status)
	}
	paths := mismatchPaths(report.Mismatches)
	expectedPaths := []string{"dynamic_metadata.brand", "partition", "scalars.title", "vector_dimension", "vector_hash"}
	if !equalStringSlices(paths, expectedPaths) {
		t.Fatalf("mismatch paths = %#v", paths)
	}
	if report.Summary.MismatchedRecords != 1 || report.Summary.ScalarMismatches != 1 || report.Summary.DynamicMetadataMismatches != 1 || report.Summary.PartitionMismatches != 1 || report.Summary.VectorMismatches != 2 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestCompareFullRecordArtifactsRejectsDuplicateIDs(t *testing.T) {
	source := fullRecordArtifactFixture("milvus", []FullRecordArtifactRecord{
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
		{ID: "sku-1", VectorHash: "sha256:222", VectorDimension: 8},
	})
	target := fullRecordArtifactFixture("pgvector", []FullRecordArtifactRecord{
		{ID: "sku-1", VectorHash: "sha256:111", VectorDimension: 8},
	})

	_, err := CompareFullRecordArtifacts(source, target)
	if err == nil || !strings.Contains(err.Error(), "duplicate record id") {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
}

func TestMarshalFullRecordCompareReportKeepsStableJSONShape(t *testing.T) {
	report := FullRecordCompareReport{
		SchemaVersion: FullRecordCompareReportVersion,
		Status:        FullRecordCompareStatusFail,
		Source:        FullRecordCompareEndpoint{System: "milvus", Collection: "items", RecordCount: 2},
		Target:        FullRecordCompareEndpoint{System: "pgvector", Collection: "items", RecordCount: 2},
		Summary:       FullRecordCompareSummary{MatchedRecords: 1, MismatchedRecords: 1},
		Mismatches: []FullRecordMismatch{{
			ID:          "sku-1",
			FieldPath:   "scalars.title",
			SourceValue: "First",
			TargetValue: "Changed",
		}},
	}

	data, err := MarshalFullRecordCompareReport(report)
	if err != nil {
		t.Fatalf("MarshalFullRecordCompareReport returned error: %v", err)
	}
	var decoded struct {
		SchemaVersion string `json:"schema_version"`
		Status        string `json:"status"`
		Source        struct {
			System      string `json:"system"`
			Collection  string `json:"collection"`
			RecordCount int    `json:"record_count"`
		} `json:"source"`
		Summary struct {
			MatchedRecords    int `json:"matched_records"`
			MismatchedRecords int `json:"mismatched_records"`
		} `json:"summary"`
		Mismatches []struct {
			ID        string `json:"id"`
			FieldPath string `json:"field_path"`
		} `json:"mismatches"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if decoded.SchemaVersion != "v1" || decoded.Status != "fail" || decoded.Source.RecordCount != 2 || decoded.Summary.MismatchedRecords != 1 || decoded.Mismatches[0].FieldPath != "scalars.title" {
		t.Fatalf("decoded report = %#v", decoded)
	}
}

func TestMarshalFullRecordCompareReportPreservesZeroMismatchValues(t *testing.T) {
	report := FullRecordCompareReport{
		SchemaVersion: FullRecordCompareReportVersion,
		Status:        FullRecordCompareStatusFail,
		Mismatches: []FullRecordMismatch{{
			ID:          "sku-1",
			FieldPath:   "scalars.count",
			SourceValue: 0,
			TargetValue: nil,
		}},
	}

	data, err := MarshalFullRecordCompareReport(report)
	if err != nil {
		t.Fatalf("MarshalFullRecordCompareReport returned error: %v", err)
	}
	var decoded struct {
		Mismatches []map[string]any `json:"mismatches"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	mismatch := decoded.Mismatches[0]
	if _, ok := mismatch["source_value"]; !ok {
		t.Fatalf("source_value was omitted from %#v", mismatch)
	}
	if _, ok := mismatch["target_value"]; !ok {
		t.Fatalf("target_value was omitted from %#v", mismatch)
	}
}

func fullRecordArtifactFixture(system string, records []FullRecordArtifactRecord) FullRecordArtifact {
	return FullRecordArtifact{
		SchemaVersion:     FullRecordArtifactVersion,
		System:            system,
		Collection:        "items",
		RecordMappingPath: "/tmp/vdb-guardian-record-mapping.json",
		Records:           records,
	}
}

func mismatchPaths(mismatches []FullRecordMismatch) []string {
	paths := make([]string, len(mismatches))
	for index, mismatch := range mismatches {
		paths[index] = mismatch.FieldPath
	}
	return paths
}
