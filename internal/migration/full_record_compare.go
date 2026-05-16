package migration

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const (
	// FullRecordArtifactVersion is the current schema version for local full-record artifacts.
	FullRecordArtifactVersion = "v1"
	// FullRecordCompareReportVersion is the current schema version for full-record comparison reports.
	FullRecordCompareReportVersion = "v1"
	// FullRecordCompareStatusPass means source and target full-record artifacts are equivalent.
	FullRecordCompareStatusPass = "pass"
	// FullRecordCompareStatusFail means at least one full-record row or field mismatch was detected.
	FullRecordCompareStatusFail = "fail"
)

// FullRecordArtifact is a local, machine-readable snapshot of migrated records.
//
// It intentionally stores vector hashes and dimensions instead of raw vector
// payloads so equality checks remain deterministic and artifacts stay compact.
type FullRecordArtifact struct {
	SchemaVersion     string                     `json:"schema_version"`
	System            string                     `json:"system"`
	Collection        string                     `json:"collection"`
	RecordMappingPath string                     `json:"record_mapping_path,omitempty"`
	Records           []FullRecordArtifactRecord `json:"records"`
}

// FullRecordArtifactRecord captures one migrated record for artifact-only
// equality comparison across source and target systems.
type FullRecordArtifactRecord struct {
	ID              string         `json:"id"`
	VectorHash      string         `json:"vector_hash"`
	VectorDimension int            `json:"vector_dimension"`
	Scalars         map[string]any `json:"scalars,omitempty"`
	DynamicMetadata map[string]any `json:"dynamic_metadata,omitempty"`
	Partition       string         `json:"partition,omitempty"`
}

// FullRecordCompareEndpoint summarizes one side of a full-record comparison.
type FullRecordCompareEndpoint struct {
	System      string `json:"system"`
	Collection  string `json:"collection"`
	RecordCount int    `json:"record_count"`
}

// FullRecordCompareSummary contains aggregate full-record comparison counters.
type FullRecordCompareSummary struct {
	MatchedRecords            int `json:"matched_records"`
	MissingSourceRecords      int `json:"missing_source_records"`
	MissingTargetRecords      int `json:"missing_target_records"`
	MismatchedRecords         int `json:"mismatched_records"`
	ScalarMismatches          int `json:"scalar_mismatches"`
	DynamicMetadataMismatches int `json:"dynamic_metadata_mismatches"`
	PartitionMismatches       int `json:"partition_mismatches"`
	VectorMismatches          int `json:"vector_mismatches"`
}

// FullRecordMismatch describes one field-level difference for a matched record ID.
type FullRecordMismatch struct {
	ID          string `json:"id"`
	FieldPath   string `json:"field_path"`
	SourceValue any    `json:"source_value"`
	TargetValue any    `json:"target_value"`
}

// FullRecordCompareReport is the stable machine-readable output from comparing
// two full-record artifacts.
type FullRecordCompareReport struct {
	SchemaVersion    string                    `json:"schema_version"`
	Status           string                    `json:"status"`
	Source           FullRecordCompareEndpoint `json:"source"`
	Target           FullRecordCompareEndpoint `json:"target"`
	Summary          FullRecordCompareSummary  `json:"summary"`
	MissingSourceIDs []string                  `json:"missing_source_ids"`
	MissingTargetIDs []string                  `json:"missing_target_ids"`
	Mismatches       []FullRecordMismatch      `json:"mismatches"`
}

// CompareFullRecordArtifacts compares source and target full-record artifacts and
// returns a deterministic report. It validates artifact versions and rejects
// duplicate record IDs before comparing row presence and field values.
func CompareFullRecordArtifacts(source, target FullRecordArtifact) (FullRecordCompareReport, error) {
	if err := validateFullRecordArtifact("source", source); err != nil {
		return FullRecordCompareReport{}, err
	}
	if err := validateFullRecordArtifact("target", target); err != nil {
		return FullRecordCompareReport{}, err
	}
	sourceRecords, err := fullRecordIndex("source", source.Records)
	if err != nil {
		return FullRecordCompareReport{}, err
	}
	targetRecords, err := fullRecordIndex("target", target.Records)
	if err != nil {
		return FullRecordCompareReport{}, err
	}

	report := newFullRecordCompareReport(source, target)
	mismatchedIDs := compareFullRecordRows(&report, sourceRecords, targetRecords)
	finalizeFullRecordCompareReport(&report, mismatchedIDs)
	return report, nil
}

// MarshalFullRecordCompareReport renders a full-record comparison report as
// deterministic indented JSON for CLI artifact output.
func MarshalFullRecordCompareReport(report FullRecordCompareReport) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal full-record compare report: %w", err)
	}
	return append(data, '\n'), nil
}

func newFullRecordCompareReport(source, target FullRecordArtifact) FullRecordCompareReport {
	report := FullRecordCompareReport{
		SchemaVersion:    FullRecordCompareReportVersion,
		Status:           FullRecordCompareStatusPass,
		Source:           FullRecordCompareEndpoint{System: source.System, Collection: source.Collection, RecordCount: len(source.Records)},
		Target:           FullRecordCompareEndpoint{System: target.System, Collection: target.Collection, RecordCount: len(target.Records)},
		MissingSourceIDs: []string{},
		MissingTargetIDs: []string{},
		Mismatches:       []FullRecordMismatch{},
	}
	return report
}

func compareFullRecordRows(report *FullRecordCompareReport, sourceRecords, targetRecords map[string]FullRecordArtifactRecord) map[string]struct{} {
	mismatchedIDs := make(map[string]struct{})
	for _, id := range sortedFullRecordIDs(sourceRecords, targetRecords) {
		sourceRecord, sourceOK := sourceRecords[id]
		targetRecord, targetOK := targetRecords[id]
		compareFullRecordRow(report, mismatchedIDs, id, sourceRecord, sourceOK, targetRecord, targetOK)
	}
	return mismatchedIDs
}

func compareFullRecordRow(report *FullRecordCompareReport, mismatchedIDs map[string]struct{}, id string, sourceRecord FullRecordArtifactRecord, sourceOK bool, targetRecord FullRecordArtifactRecord, targetOK bool) {
	switch {
	case !sourceOK:
		report.MissingSourceIDs = append(report.MissingSourceIDs, id)
	case !targetOK:
		report.MissingTargetIDs = append(report.MissingTargetIDs, id)
	default:
		report.Summary.MatchedRecords++
		before := len(report.Mismatches)
		report.Mismatches = append(report.Mismatches, compareFullRecordFields(sourceRecord, targetRecord)...)
		if len(report.Mismatches) > before {
			mismatchedIDs[id] = struct{}{}
		}
	}
}

func finalizeFullRecordCompareReport(report *FullRecordCompareReport, mismatchedIDs map[string]struct{}) {
	report.Summary.MissingSourceRecords = len(report.MissingSourceIDs)
	report.Summary.MissingTargetRecords = len(report.MissingTargetIDs)
	report.Summary.MismatchedRecords = len(mismatchedIDs)
	for _, mismatch := range report.Mismatches {
		addFullRecordMismatchSummary(&report.Summary, mismatch)
	}
	if report.Summary.MissingSourceRecords > 0 || report.Summary.MissingTargetRecords > 0 || report.Summary.MismatchedRecords > 0 {
		report.Status = FullRecordCompareStatusFail
	}
}

func addFullRecordMismatchSummary(summary *FullRecordCompareSummary, mismatch FullRecordMismatch) {
	switch {
	case strings.HasPrefix(mismatch.FieldPath, "scalars."):
		summary.ScalarMismatches++
	case strings.HasPrefix(mismatch.FieldPath, "dynamic_metadata."):
		summary.DynamicMetadataMismatches++
	case mismatch.FieldPath == "partition":
		summary.PartitionMismatches++
	case strings.HasPrefix(mismatch.FieldPath, "vector_"):
		summary.VectorMismatches++
	}
}

func validateFullRecordArtifact(label string, artifact FullRecordArtifact) error {
	if artifact.SchemaVersion != FullRecordArtifactVersion {
		return fmt.Errorf("%s full-record artifact has unsupported schema_version %q", label, artifact.SchemaVersion)
	}
	return nil
}

func fullRecordIndex(label string, records []FullRecordArtifactRecord) (map[string]FullRecordArtifactRecord, error) {
	indexed := make(map[string]FullRecordArtifactRecord, len(records))
	for _, record := range records {
		if record.ID == "" {
			return nil, fmt.Errorf("%s full-record artifact contains empty record id", label)
		}
		if _, exists := indexed[record.ID]; exists {
			return nil, fmt.Errorf("%s full-record artifact contains duplicate record id %q", label, record.ID)
		}
		indexed[record.ID] = record
	}
	return indexed, nil
}

func sortedFullRecordIDs(left, right map[string]FullRecordArtifactRecord) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	ids := make([]string, 0, len(left)+len(right))
	for id := range left {
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for id := range right {
		if _, ok := seen[id]; ok {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func compareFullRecordFields(source, target FullRecordArtifactRecord) []FullRecordMismatch {
	var mismatches []FullRecordMismatch
	if source.VectorHash != target.VectorHash {
		mismatches = append(mismatches, FullRecordMismatch{ID: source.ID, FieldPath: "vector_hash", SourceValue: source.VectorHash, TargetValue: target.VectorHash})
	}
	if source.VectorDimension != target.VectorDimension {
		mismatches = append(mismatches, FullRecordMismatch{ID: source.ID, FieldPath: "vector_dimension", SourceValue: source.VectorDimension, TargetValue: target.VectorDimension})
	}
	mismatches = append(mismatches, compareMapFields(source.ID, "scalars", source.Scalars, target.Scalars)...)
	mismatches = append(mismatches, compareMapFields(source.ID, "dynamic_metadata", source.DynamicMetadata, target.DynamicMetadata)...)
	if source.Partition != target.Partition {
		mismatches = append(mismatches, FullRecordMismatch{ID: source.ID, FieldPath: "partition", SourceValue: source.Partition, TargetValue: target.Partition})
	}
	sort.SliceStable(mismatches, func(i, j int) bool {
		if mismatches[i].ID != mismatches[j].ID {
			return mismatches[i].ID < mismatches[j].ID
		}
		return mismatches[i].FieldPath < mismatches[j].FieldPath
	})
	return mismatches
}

func compareMapFields(id, prefix string, source, target map[string]any) []FullRecordMismatch {
	keys := sortedMapKeys(source, target)
	mismatches := make([]FullRecordMismatch, 0)
	for _, key := range keys {
		sourceValue, sourceOK := source[key]
		targetValue, targetOK := target[key]
		if sourceOK != targetOK || !reflect.DeepEqual(sourceValue, targetValue) {
			mismatches = append(mismatches, FullRecordMismatch{ID: id, FieldPath: prefix + "." + key, SourceValue: sourceValue, TargetValue: targetValue})
		}
	}
	return mismatches
}

func sortedMapKeys(left, right map[string]any) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	keys := make([]string, 0, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range right {
		if _, ok := seen[key]; ok {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
