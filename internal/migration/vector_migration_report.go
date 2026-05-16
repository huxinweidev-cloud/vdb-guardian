package migration

const VectorMigrationReportVersion = "v1"

const (
	VectorMigrationReportStatusCompleted = "completed"
	VectorMigrationReportStatusFailed    = "failed"
)

type VectorMigrationReportOptions struct {
	JobID             string
	SchemaPreflight   bool
	SchemaComparePath string
	Mapping           *VectorMigrationReportMapping
	Checkpoint        *VectorMigrationReportCheckpoint
}

type VectorMigrationReport struct {
	SchemaVersion string                           `json:"schema_version"`
	JobID         string                           `json:"job_id,omitempty"`
	Status        string                           `json:"status"`
	Source        VectorMigrationReportEndpoint    `json:"source"`
	Target        VectorMigrationReportEndpoint    `json:"target"`
	Preflight     VectorMigrationReportPreflight   `json:"preflight"`
	Mapping       *VectorMigrationReportMapping    `json:"mapping,omitempty"`
	Checkpoint    *VectorMigrationReportCheckpoint `json:"checkpoint,omitempty"`
	Summary       VectorMigrationReportSummary     `json:"summary"`
}

type VectorMigrationReportEndpoint struct {
	Type       string `json:"type"`
	Collection string `json:"collection,omitempty"`
	Table      string `json:"table,omitempty"`
}

type VectorMigrationReportPreflight struct {
	SchemaMatchRequired bool   `json:"schema_match_required"`
	SchemaComparePath   string `json:"schema_compare_path,omitempty"`
	SchemaCompareStatus string `json:"schema_compare_status"`
}

type VectorMigrationReportSummary struct {
	Dimension          int    `json:"dimension"`
	RecordsRead        int    `json:"records_read"`
	RecordsWritten     int    `json:"records_written"`
	WriteModeRequested string `json:"write_mode_requested,omitempty"`
	WriteModeUsed      string `json:"write_mode_used,omitempty"`
	CopyBatches        int    `json:"copy_batches"`
	BatchUpsertBatches int    `json:"batch_upsert_batches"`
	CopyFallbacks      int    `json:"copy_fallbacks"`
}

type VectorMigrationReportMapping struct {
	SchemaPlan                    string `json:"schema_plan,omitempty"`
	Status                        string `json:"status"`
	ScalarMappingCount            int    `json:"scalar_mapping_count"`
	DynamicMetadataMappingCount   int    `json:"dynamic_metadata_mapping_count"`
	PartitionMetadataMappingCount int    `json:"partition_metadata_mapping_count"`
	BlockingIssueCount            int    `json:"blocking_issue_count"`
}

type VectorMigrationReportCheckpoint struct {
	Path             string `json:"path,omitempty"`
	ResumeFrom       string `json:"resume_from,omitempty"`
	CompletedBatches int    `json:"completed_batches"`
	FailedBatches    int    `json:"failed_batches"`
	NextRecordOffset int    `json:"next_record_offset"`
}

func BuildVectorMigrationReport(result VectorMigrationResult, options VectorMigrationReportOptions) VectorMigrationReport {
	preflightStatus := "skipped"
	if options.SchemaPreflight {
		preflightStatus = "pass"
	}
	return VectorMigrationReport{
		SchemaVersion: VectorMigrationReportVersion,
		JobID:         options.JobID,
		Status:        VectorMigrationReportStatusCompleted,
		Source: VectorMigrationReportEndpoint{
			Type:       "milvus",
			Collection: result.SourceCollection,
		},
		Target: VectorMigrationReportEndpoint{
			Type:  "pgvector",
			Table: result.TargetTable,
		},
		Preflight: VectorMigrationReportPreflight{
			SchemaMatchRequired: options.SchemaPreflight,
			SchemaComparePath:   options.SchemaComparePath,
			SchemaCompareStatus: preflightStatus,
		},
		Mapping:    options.Mapping,
		Checkpoint: options.Checkpoint,
		Summary: VectorMigrationReportSummary{
			Dimension:          result.Dimension,
			RecordsRead:        result.RecordsRead,
			RecordsWritten:     result.RecordsWritten,
			WriteModeRequested: result.WriteModeRequested,
			WriteModeUsed:      result.WriteModeUsed,
			CopyBatches:        result.CopyBatches,
			BatchUpsertBatches: result.BatchUpsertBatches,
			CopyFallbacks:      result.CopyFallbacks,
		},
	}
}
