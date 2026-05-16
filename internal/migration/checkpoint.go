package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VectorMigrationCheckpointVersion is the stable schema version for checkpoint
// artifacts emitted by checkpointed Milvus-to-pgvector migration runs.
const VectorMigrationCheckpointVersion = "v1"

// VectorMigrationCheckpointStatusRunning marks a checkpoint written after one or
// more successful batches while the migration can still continue.
const VectorMigrationCheckpointStatusRunning = "running"

// VectorMigrationCheckpointStatusCompleted marks a checkpoint written after all
// migration batches have been written successfully.
const VectorMigrationCheckpointStatusCompleted = "completed"

// VectorMigrationCheckpointStatusFailed marks a checkpoint written after a batch
// failure so operators can inspect completed work and resume safely later.
const VectorMigrationCheckpointStatusFailed = "failed"

// VectorMigrationCheckpointInput contains the non-secret migration state used to
// build a checkpoint artifact. It deliberately excludes connector addresses and
// database connection URLs so persisted artifacts can be shared for audit without
// leaking credentials.
type VectorMigrationCheckpointInput struct {
	JobID            string
	Status           string
	SourceCollection string
	TargetTable      string
	Dimension        int
	BatchSize        int
	RecordsRead      int
	RecordsWritten   int
	CompletedBatches []VectorMigrationCheckpointBatch
	FailedBatches    []VectorMigrationCheckpointBatch
	Resume           VectorMigrationCheckpointResume
}

// VectorMigrationCheckpoint is the JSON artifact that records batch-level
// progress for one Milvus-to-pgvector migration job. It stores only non-secret
// identifiers, counters, batch ranges, and local artifact fingerprints.
type VectorMigrationCheckpoint struct {
	SchemaVersion    string                            `json:"schema_version"`
	JobID            string                            `json:"job_id,omitempty"`
	Status           string                            `json:"status"`
	Source           VectorMigrationCheckpointEndpoint `json:"source"`
	Target           VectorMigrationCheckpointEndpoint `json:"target"`
	Dimension        int                               `json:"dimension"`
	BatchSize        int                               `json:"batch_size"`
	RecordsRead      int                               `json:"records_read"`
	RecordsWritten   int                               `json:"records_written"`
	CompletedBatches []VectorMigrationCheckpointBatch  `json:"completed_batches"`
	FailedBatches    []VectorMigrationCheckpointBatch  `json:"failed_batches"`
	Resume           VectorMigrationCheckpointResume   `json:"resume"`
}

// VectorMigrationCheckpointEndpoint identifies the logical source or target in a
// checkpoint artifact without storing network addresses or credentials.
type VectorMigrationCheckpointEndpoint struct {
	Type       string `json:"type"`
	Collection string `json:"collection,omitempty"`
	Table      string `json:"table,omitempty"`
}

// VectorMigrationCheckpointBatch records the zero-based source record range for
// a completed or failed migration batch. Failed batches include a sanitized error
// string to support diagnostics without connector secrets.
type VectorMigrationCheckpointBatch struct {
	Index          int    `json:"index"`
	Start          int    `json:"start"`
	End            int    `json:"end"`
	RecordsWritten int    `json:"records_written,omitempty"`
	Error          string `json:"error,omitempty"`
}

// VectorMigrationCheckpointResume records the next safe resume position and the
// artifact fingerprints that must match before a resumed migration can write to
// the target again.
type VectorMigrationCheckpointResume struct {
	NextBatchIndex           int    `json:"next_batch_index"`
	NextRecordOffset         int    `json:"next_record_offset"`
	RecordMappingPath        string `json:"record_mapping_path,omitempty"`
	SchemaPlanPath           string `json:"schema_plan_path,omitempty"`
	RecordMappingFingerprint string `json:"record_mapping_fingerprint,omitempty"`
	SchemaPlanFingerprint    string `json:"schema_plan_fingerprint,omitempty"`
}

// BuildVectorMigrationCheckpoint normalizes migration progress into the stable
// checkpoint artifact shape used by CLI reports and resume validation.
func BuildVectorMigrationCheckpoint(input VectorMigrationCheckpointInput) VectorMigrationCheckpoint {
	return VectorMigrationCheckpoint{
		SchemaVersion: VectorMigrationCheckpointVersion,
		JobID:         input.JobID,
		Status:        input.Status,
		Source: VectorMigrationCheckpointEndpoint{
			Type:       "milvus",
			Collection: input.SourceCollection,
		},
		Target: VectorMigrationCheckpointEndpoint{
			Type:  "pgvector",
			Table: input.TargetTable,
		},
		Dimension:        input.Dimension,
		BatchSize:        input.BatchSize,
		RecordsRead:      input.RecordsRead,
		RecordsWritten:   input.RecordsWritten,
		CompletedBatches: append([]VectorMigrationCheckpointBatch(nil), input.CompletedBatches...),
		FailedBatches:    append([]VectorMigrationCheckpointBatch(nil), input.FailedBatches...),
		Resume:           input.Resume,
	}
}

// WriteVectorMigrationCheckpoint writes a checkpoint artifact as indented JSON
// with 0600 permissions. Parent directories are created as needed so CLI callers
// can place artifacts in a dedicated run directory.
func WriteVectorMigrationCheckpoint(path string, checkpoint VectorMigrationCheckpoint) error {
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vector migration checkpoint: %w", err)
	}
	data = append(data, '\n')
	if err := writeVectorMigrationArtifact0600(path, data); err != nil {
		return fmt.Errorf("write vector migration checkpoint: %w", err)
	}
	return nil
}

// WriteVectorMigrationReport writes a migration report with 0600 permissions,
// tightening permissions even when replacing an existing broader file.
func WriteVectorMigrationReport(path string, report VectorMigrationReport) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vector migration report: %w", err)
	}
	data = append(data, '\n')
	if err := writeVectorMigrationArtifact0600(path, data); err != nil {
		return fmt.Errorf("write vector migration report: %w", err)
	}
	return nil
}

func writeVectorMigrationArtifact0600(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// ReadVectorMigrationCheckpoint loads a checkpoint artifact from disk. The
// returned value is used by resume validation before any target write occurs.
func ReadVectorMigrationCheckpoint(path string) (VectorMigrationCheckpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return VectorMigrationCheckpoint{}, fmt.Errorf("read vector migration checkpoint: %w", err)
	}
	var checkpoint VectorMigrationCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return VectorMigrationCheckpoint{}, fmt.Errorf("unmarshal vector migration checkpoint: %w", err)
	}
	return checkpoint, nil
}

// VectorMigrationResumeExpectation contains the current non-secret migration
// identity and artifact fingerprints that must match a checkpoint before resume.
type VectorMigrationResumeExpectation struct {
	SourceCollection         string
	TargetTable              string
	Dimension                int
	BatchSize                int
	RecordMappingFingerprint string
	SchemaPlanFingerprint    string
}

// ValidateVectorMigrationResume checks that a saved checkpoint is safe to resume
// with the current migration request. It rejects completed checkpoints and any
// source, target, dimension, batch, schema, or mapping drift before writes occur.
func ValidateVectorMigrationResume(checkpoint VectorMigrationCheckpoint, expected VectorMigrationResumeExpectation) error {
	if checkpoint.SchemaVersion != VectorMigrationCheckpointVersion {
		return fmt.Errorf("checkpoint schema version %q is not supported", checkpoint.SchemaVersion)
	}
	if checkpoint.Status == VectorMigrationCheckpointStatusCompleted {
		return errors.New("cannot resume from completed checkpoint")
	}
	if checkpoint.Status != VectorMigrationCheckpointStatusRunning && checkpoint.Status != VectorMigrationCheckpointStatusFailed {
		return fmt.Errorf("checkpoint status %q is not resumable", checkpoint.Status)
	}
	if checkpoint.Source.Collection != expected.SourceCollection {
		return fmt.Errorf("checkpoint source collection %q does not match %q", checkpoint.Source.Collection, expected.SourceCollection)
	}
	if checkpoint.Target.Table != expected.TargetTable {
		return fmt.Errorf("checkpoint target table %q does not match %q", checkpoint.Target.Table, expected.TargetTable)
	}
	if checkpoint.Dimension != expected.Dimension {
		return fmt.Errorf("checkpoint dimension %d does not match %d", checkpoint.Dimension, expected.Dimension)
	}
	if checkpoint.BatchSize != expected.BatchSize {
		return fmt.Errorf("checkpoint batch size %d does not match %d", checkpoint.BatchSize, expected.BatchSize)
	}
	if checkpoint.Resume.RecordMappingFingerprint != expected.RecordMappingFingerprint {
		return fmt.Errorf("checkpoint record mapping fingerprint %q does not match %q", checkpoint.Resume.RecordMappingFingerprint, expected.RecordMappingFingerprint)
	}
	if checkpoint.Resume.SchemaPlanFingerprint != expected.SchemaPlanFingerprint {
		return fmt.Errorf("checkpoint schema plan fingerprint %q does not match %q", checkpoint.Resume.SchemaPlanFingerprint, expected.SchemaPlanFingerprint)
	}
	if err := validateVectorMigrationCheckpointInvariants(checkpoint); err != nil {
		return err
	}
	return nil
}

func validateVectorMigrationCheckpointInvariants(checkpoint VectorMigrationCheckpoint) error {
	if checkpoint.BatchSize <= 0 {
		return fmt.Errorf("checkpoint batch size must be positive")
	}
	if checkpoint.RecordsRead < 0 || checkpoint.RecordsWritten < 0 {
		return fmt.Errorf("checkpoint record counts must be non-negative")
	}
	if checkpoint.Resume.NextRecordOffset < 0 {
		return fmt.Errorf("checkpoint next record offset must be non-negative")
	}
	if checkpoint.Resume.NextRecordOffset > checkpoint.RecordsRead {
		return fmt.Errorf("checkpoint next record offset %d exceeds records read %d", checkpoint.Resume.NextRecordOffset, checkpoint.RecordsRead)
	}
	if checkpoint.Resume.NextBatchIndex < 0 {
		return fmt.Errorf("checkpoint next batch index must be non-negative")
	}
	if checkpoint.Resume.NextRecordOffset != checkpoint.Resume.NextBatchIndex*checkpoint.BatchSize && checkpoint.Resume.NextRecordOffset != checkpoint.RecordsRead {
		return fmt.Errorf("checkpoint resume offset %d is not aligned with batch index %d and batch size %d", checkpoint.Resume.NextRecordOffset, checkpoint.Resume.NextBatchIndex, checkpoint.BatchSize)
	}
	written := 0
	for _, batch := range checkpoint.CompletedBatches {
		if err := validateVectorMigrationCheckpointBatch(batch, checkpoint.RecordsRead); err != nil {
			return err
		}
		written += batch.RecordsWritten
	}
	if written != checkpoint.RecordsWritten {
		return fmt.Errorf("checkpoint completed batch records %d do not match records_written %d", written, checkpoint.RecordsWritten)
	}
	return nil
}

func validateVectorMigrationCheckpointBatch(batch VectorMigrationCheckpointBatch, recordsRead int) error {
	if batch.Index < 0 || batch.Start < 0 || batch.End < batch.Start || batch.End > recordsRead {
		return fmt.Errorf("checkpoint batch %d has invalid range [%d,%d)", batch.Index, batch.Start, batch.End)
	}
	if batch.RecordsWritten < 0 || batch.RecordsWritten > batch.End-batch.Start {
		return fmt.Errorf("checkpoint batch %d has invalid records_written %d", batch.Index, batch.RecordsWritten)
	}
	return nil
}

// SanitizeVectorMigrationCheckpointError returns a bounded diagnostic string for
// checkpoint artifacts without persisting connector error text that may contain
// credentials, URLs, SQL, or row payloads.
func SanitizeVectorMigrationCheckpointError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for _, marker := range []string{"postgres://", "postgresql://", "password", "credential", "token", "secret", "api_key", "Bearer"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(marker)) {
			return "[REDACTED]"
		}
	}
	if len(text) > 160 {
		text = text[:160] + "..."
	}
	return text
}

// FileSHA256Fingerprint returns a stable sha256 fingerprint for a local artifact
// file. It is intended for schema and record-mapping artifacts, never for secret
// connection strings.
func FileSHA256Fingerprint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read fingerprint source: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
