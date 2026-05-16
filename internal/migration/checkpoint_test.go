package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildVectorMigrationCheckpoint(t *testing.T) {
	checkpoint := BuildVectorMigrationCheckpoint(VectorMigrationCheckpointInput{
		JobID:            "migration-smoke",
		Status:           VectorMigrationCheckpointStatusFailed,
		SourceCollection: "items",
		TargetTable:      "items_copy",
		Dimension:        8,
		BatchSize:        100,
		RecordsRead:      250,
		RecordsWritten:   200,
		CompletedBatches: []VectorMigrationCheckpointBatch{{Index: 0, Start: 0, End: 100, RecordsWritten: 100}},
		FailedBatches:    []VectorMigrationCheckpointBatch{{Index: 2, Start: 200, End: 250, Error: "write target records: boom"}},
		Resume: VectorMigrationCheckpointResume{
			NextBatchIndex:           2,
			NextRecordOffset:         200,
			RecordMappingPath:        "/tmp/record-mapping.json",
			SchemaPlanPath:           "/tmp/schema-plan.json",
			RecordMappingFingerprint: "sha256:mapping",
			SchemaPlanFingerprint:    "sha256:schema",
		},
	})

	if checkpoint.SchemaVersion != VectorMigrationCheckpointVersion {
		t.Fatalf("schema version = %q", checkpoint.SchemaVersion)
	}
	if checkpoint.JobID != "migration-smoke" || checkpoint.Status != VectorMigrationCheckpointStatusFailed {
		t.Fatalf("unexpected job/status: %+v", checkpoint)
	}
	if checkpoint.Source.Type != "milvus" || checkpoint.Source.Collection != "items" {
		t.Fatalf("unexpected source: %+v", checkpoint.Source)
	}
	if checkpoint.Target.Type != "pgvector" || checkpoint.Target.Table != "items_copy" {
		t.Fatalf("unexpected target: %+v", checkpoint.Target)
	}
	if checkpoint.Dimension != 8 || checkpoint.BatchSize != 100 || checkpoint.RecordsRead != 250 || checkpoint.RecordsWritten != 200 {
		t.Fatalf("unexpected counters: %+v", checkpoint)
	}
	if len(checkpoint.CompletedBatches) != 1 || checkpoint.CompletedBatches[0].RecordsWritten != 100 {
		t.Fatalf("unexpected completed batches: %+v", checkpoint.CompletedBatches)
	}
	if len(checkpoint.FailedBatches) != 1 || checkpoint.FailedBatches[0].Error == "" {
		t.Fatalf("unexpected failed batches: %+v", checkpoint.FailedBatches)
	}
	if checkpoint.Resume.NextBatchIndex != 2 || checkpoint.Resume.NextRecordOffset != 200 || checkpoint.Resume.RecordMappingFingerprint != "sha256:mapping" {
		t.Fatalf("unexpected resume: %+v", checkpoint.Resume)
	}
}

func TestWriteVectorMigrationCheckpointWrites0600JSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "checkpoint.json")
	checkpoint := BuildVectorMigrationCheckpoint(VectorMigrationCheckpointInput{
		JobID:            "migration-smoke",
		Status:           VectorMigrationCheckpointStatusRunning,
		SourceCollection: "items",
		TargetTable:      "items",
		Dimension:        3,
		BatchSize:        2,
	})

	if err := WriteVectorMigrationCheckpoint(path, checkpoint); err != nil {
		t.Fatalf("WriteVectorMigrationCheckpoint returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat checkpoint: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("checkpoint permissions = %o, want 0600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	var decoded VectorMigrationCheckpoint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}
	if decoded.JobID != "migration-smoke" || decoded.Source.Collection != "items" {
		t.Fatalf("unexpected decoded checkpoint: %+v", decoded)
	}
}

func TestReadVectorMigrationCheckpointAndValidateResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	checkpoint := BuildVectorMigrationCheckpoint(VectorMigrationCheckpointInput{
		Status:           VectorMigrationCheckpointStatusFailed,
		SourceCollection: "items",
		TargetTable:      "items",
		Dimension:        8,
		BatchSize:        100,
		RecordsRead:      500,
		RecordsWritten:   200,
		CompletedBatches: []VectorMigrationCheckpointBatch{{Index: 0, Start: 0, End: 100, RecordsWritten: 100}, {Index: 1, Start: 100, End: 200, RecordsWritten: 100}},
		Resume: VectorMigrationCheckpointResume{
			NextBatchIndex:           2,
			NextRecordOffset:         200,
			RecordMappingFingerprint: "sha256:mapping",
			SchemaPlanFingerprint:    "sha256:schema",
		},
	})
	if err := WriteVectorMigrationCheckpoint(path, checkpoint); err != nil {
		t.Fatalf("write checkpoint fixture: %v", err)
	}

	loaded, err := ReadVectorMigrationCheckpoint(path)
	if err != nil {
		t.Fatalf("ReadVectorMigrationCheckpoint returned error: %v", err)
	}
	if loaded.Resume.NextRecordOffset != 200 {
		t.Fatalf("loaded resume offset = %d", loaded.Resume.NextRecordOffset)
	}
	if err := ValidateVectorMigrationResume(loaded, VectorMigrationResumeExpectation{
		SourceCollection:         "items",
		TargetTable:              "items",
		Dimension:                8,
		BatchSize:                100,
		RecordMappingFingerprint: "sha256:mapping",
		SchemaPlanFingerprint:    "sha256:schema",
	}); err != nil {
		t.Fatalf("ValidateVectorMigrationResume returned error: %v", err)
	}
}

func TestValidateVectorMigrationResumeRejectsUnsafeMismatches(t *testing.T) {
	base := BuildVectorMigrationCheckpoint(VectorMigrationCheckpointInput{
		Status:           VectorMigrationCheckpointStatusFailed,
		SourceCollection: "items",
		TargetTable:      "items_copy",
		Dimension:        8,
		BatchSize:        100,
		RecordsRead:      200,
		RecordsWritten:   100,
		CompletedBatches: []VectorMigrationCheckpointBatch{{Index: 0, Start: 0, End: 100, RecordsWritten: 100}},
		Resume: VectorMigrationCheckpointResume{
			NextBatchIndex:           1,
			NextRecordOffset:         100,
			RecordMappingFingerprint: "sha256:mapping",
			SchemaPlanFingerprint:    "sha256:schema",
		},
	})
	expectation := VectorMigrationResumeExpectation{
		SourceCollection:         "items",
		TargetTable:              "items_copy",
		Dimension:                8,
		BatchSize:                100,
		RecordMappingFingerprint: "sha256:mapping",
		SchemaPlanFingerprint:    "sha256:schema",
	}

	tests := []struct {
		name       string
		mutate     func(*VectorMigrationCheckpoint, *VectorMigrationResumeExpectation)
		wantSubstr string
	}{
		{name: "source", mutate: func(_ *VectorMigrationCheckpoint, e *VectorMigrationResumeExpectation) { e.SourceCollection = "other" }, wantSubstr: "source collection"},
		{name: "target", mutate: func(_ *VectorMigrationCheckpoint, e *VectorMigrationResumeExpectation) { e.TargetTable = "other" }, wantSubstr: "target table"},
		{name: "dimension", mutate: func(_ *VectorMigrationCheckpoint, e *VectorMigrationResumeExpectation) { e.Dimension = 4 }, wantSubstr: "dimension"},
		{name: "batch size", mutate: func(_ *VectorMigrationCheckpoint, e *VectorMigrationResumeExpectation) { e.BatchSize = 50 }, wantSubstr: "batch size"},
		{name: "mapping fingerprint", mutate: func(_ *VectorMigrationCheckpoint, e *VectorMigrationResumeExpectation) {
			e.RecordMappingFingerprint = "sha256:other"
		}, wantSubstr: "record mapping fingerprint"},
		{name: "schema fingerprint", mutate: func(_ *VectorMigrationCheckpoint, e *VectorMigrationResumeExpectation) {
			e.SchemaPlanFingerprint = "sha256:other"
		}, wantSubstr: "schema plan fingerprint"},
		{name: "completed status", mutate: func(c *VectorMigrationCheckpoint, _ *VectorMigrationResumeExpectation) {
			c.Status = VectorMigrationCheckpointStatusCompleted
		}, wantSubstr: "completed checkpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkpoint := base
			expected := expectation
			tt.mutate(&checkpoint, &expected)
			err := ValidateVectorMigrationResume(checkpoint, expected)
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("expected %q error, got %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestFileSHA256Fingerprint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(path, []byte("artifact"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	fingerprint, err := FileSHA256Fingerprint(path)
	if err != nil {
		t.Fatalf("FileSHA256Fingerprint returned error: %v", err)
	}
	if fingerprint != "sha256:c7c5c1d70c5dec4416ab6158afd0b223ef40c29b1dc1f97ed9428b94d4cadb1c" {
		t.Fatalf("fingerprint = %q", fingerprint)
	}
}
