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
}

type VectorMigrationReport struct {
	SchemaVersion string                         `json:"schema_version"`
	JobID         string                         `json:"job_id,omitempty"`
	Status        string                         `json:"status"`
	Source        VectorMigrationReportEndpoint  `json:"source"`
	Target        VectorMigrationReportEndpoint  `json:"target"`
	Preflight     VectorMigrationReportPreflight `json:"preflight"`
	Summary       VectorMigrationReportSummary   `json:"summary"`
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
	Dimension      int `json:"dimension"`
	RecordsRead    int `json:"records_read"`
	RecordsWritten int `json:"records_written"`
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
		Summary: VectorMigrationReportSummary{
			Dimension:      result.Dimension,
			RecordsRead:    result.RecordsRead,
			RecordsWritten: result.RecordsWritten,
		},
	}
}
