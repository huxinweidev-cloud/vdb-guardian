package schema

import (
	"context"
	"errors"
	"fmt"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

const (
	// PGVectorSchemaApplyReportVersion identifies the stable JSON schema for
	// pgvector schema apply reports emitted by the apply planning stage.
	PGVectorSchemaApplyReportVersion = "v1"
	// PGVectorSchemaApplyModeDryRun records the non-mutating mode where SQL is
	// reported but not executed against PostgreSQL.
	PGVectorSchemaApplyModeDryRun = "dry_run"
	// PGVectorSchemaApplyModeExecute records the mutating mode where schema DDL is
	// executed through the supplied executor.
	PGVectorSchemaApplyModeExecute = "execute"
	// PGVectorSchemaApplyStatusPlanned marks a dry-run report that did not mutate
	// PostgreSQL but successfully planned all statements.
	PGVectorSchemaApplyStatusPlanned = "planned"
	// PGVectorSchemaApplyStatusApplied marks an execute report where all selected
	// tables and indexes were applied successfully.
	PGVectorSchemaApplyStatusApplied = "applied"
	// PGVectorSchemaApplyStatusBlocked marks a report blocked before SQL execution
	// because the schema plan required explicit operator acknowledgement.
	PGVectorSchemaApplyStatusBlocked = "blocked"
	// PGVectorSchemaApplyStatusFailed marks a report where SQL execution failed
	// after applying zero or more earlier statements.
	PGVectorSchemaApplyStatusFailed = "failed"
)

// PGVectorSchemaExecutor executes PostgreSQL DDL for a validated pgvector schema
// plan. Implementations may wrap pgx connections, while tests use fakes so the
// apply logic can be verified without a running database.
type PGVectorSchemaExecutor interface {
	Exec(ctx context.Context, sql string) error
}

// PGVectorSchemaApplyOptions controls how a pgvector schema plan is applied or
// dry-run. Execute mode mutates PostgreSQL through the supplied executor; dry-run
// mode only returns the statements that would be executed.
type PGVectorSchemaApplyOptions struct {
	JobID            string
	Mode             string
	SchemaPlanPath   string
	SkipIndexes      bool
	AllowUnsupported bool
}

// PGVectorSchemaApplyReport is the machine-readable result of applying or
// planning application of a pgvector schema plan. It intentionally omits database
// credentials so it is safe to persist as a diagnostic artifact.
type PGVectorSchemaApplyReport struct {
	SchemaVersion string                     `json:"schema_version"`
	JobID         string                     `json:"job_id"`
	Mode          string                     `json:"mode"`
	Status        string                     `json:"status"`
	SchemaPlan    string                     `json:"schema_plan,omitempty"`
	Target        PGVectorPlanTarget         `json:"target"`
	Tables        []PGVectorSchemaApplyTable `json:"tables"`
	Warnings      []string                   `json:"warnings,omitempty"`
	Errors        []string                   `json:"errors,omitempty"`
	Summary       PGVectorSchemaApplySummary `json:"summary"`
}

// PGVectorSchemaApplySummary records aggregate counts for an apply report so
// automation can decide whether the run was a dry-run, complete apply, blocked
// apply, or partial failure without parsing table-level details.
type PGVectorSchemaApplySummary struct {
	TableCount              int `json:"table_count"`
	TableAppliedCount       int `json:"table_applied_count"`
	IndexCount              int `json:"index_count"`
	IndexAppliedCount       int `json:"index_applied_count"`
	WarningCount            int `json:"warning_count"`
	UnsupportedFeatureCount int `json:"unsupported_feature_count"`
}

// PGVectorSchemaApplyTable records table-level SQL and execution outcome for a
// pgvector schema apply run.
type PGVectorSchemaApplyTable struct {
	SourceCollection string                     `json:"source_collection"`
	TargetTable      string                     `json:"target_table"`
	CreateTableSQL   string                     `json:"create_table_sql"`
	Applied          bool                       `json:"applied"`
	Error            string                     `json:"error,omitempty"`
	Indexes          []PGVectorSchemaApplyIndex `json:"indexes,omitempty"`
}

// PGVectorSchemaApplyIndex records index-level SQL and execution outcome for a
// pgvector schema apply run.
type PGVectorSchemaApplyIndex struct {
	SourceField    string `json:"source_field"`
	TargetIndex    string `json:"target_index"`
	CreateIndexSQL string `json:"create_index_sql"`
	Applied        bool   `json:"applied"`
	Skipped        bool   `json:"skipped,omitempty"`
	Error          string `json:"error,omitempty"`
}

// ApplyPGVectorSchemaPlan applies or dry-runs a previously generated pgvector
// schema plan. It returns a diagnostic report even when execution is blocked or a
// statement fails so callers can write artifacts before surfacing the error.
func ApplyPGVectorSchemaPlan(ctx context.Context, plan PGVectorSchemaPlan, executor PGVectorSchemaExecutor, options PGVectorSchemaApplyOptions) (PGVectorSchemaApplyReport, error) {
	mode := options.Mode
	if mode == "" {
		mode = PGVectorSchemaApplyModeDryRun
	}
	report := newPGVectorSchemaApplyReport(plan, options, mode)
	if validationErr := validatePGVectorSchemaApplyInputs(plan, executor, options, mode, &report); validationErr != nil {
		return report, validationErr
	}
	if mode == PGVectorSchemaApplyModeDryRun {
		report.Status = PGVectorSchemaApplyStatusPlanned
		return report, nil
	}
	for tableIndex := range report.Tables {
		tableReport := &report.Tables[tableIndex]
		if tableReport.CreateTableSQL == "" {
			report.Status = PGVectorSchemaApplyStatusFailed
			err := fmt.Errorf("table %q is missing create_table_sql", tableReport.TargetTable)
			tableReport.Error = err.Error()
			report.Errors = append(report.Errors, err.Error())
			return report, err
		}
		if err := executor.Exec(ctx, tableReport.CreateTableSQL); err != nil {
			report.Status = PGVectorSchemaApplyStatusFailed
			tableReport.Error = err.Error()
			report.Errors = append(report.Errors, err.Error())
			return report, err
		}
		tableReport.Applied = true
		report.Summary.TableAppliedCount++
		for indexIndex := range tableReport.Indexes {
			indexReport := &tableReport.Indexes[indexIndex]
			if options.SkipIndexes {
				indexReport.Skipped = true
				continue
			}
			if indexReport.CreateIndexSQL == "" {
				report.Status = PGVectorSchemaApplyStatusFailed
				err := fmt.Errorf("index %q is missing create_index_sql", indexReport.TargetIndex)
				indexReport.Error = err.Error()
				report.Errors = append(report.Errors, err.Error())
				return report, err
			}
			if err := executor.Exec(ctx, indexReport.CreateIndexSQL); err != nil {
				report.Status = PGVectorSchemaApplyStatusFailed
				indexReport.Error = err.Error()
				report.Errors = append(report.Errors, err.Error())
				return report, err
			}
			indexReport.Applied = true
			report.Summary.IndexAppliedCount++
		}
	}
	report.Status = PGVectorSchemaApplyStatusApplied
	return report, nil
}

func validatePGVectorSchemaApplyInputs(plan PGVectorSchemaPlan, executor PGVectorSchemaExecutor, options PGVectorSchemaApplyOptions, mode string, report *PGVectorSchemaApplyReport) error {
	if plan.SchemaVersion != PGVectorSchemaPlanVersion {
		return blockedPGVectorSchemaApplyError(report, fmt.Errorf("unsupported pgvector schema plan version %q", plan.SchemaVersion))
	}
	if mode != PGVectorSchemaApplyModeDryRun && mode != PGVectorSchemaApplyModeExecute {
		return blockedPGVectorSchemaApplyError(report, fmt.Errorf("unsupported pgvector schema apply mode %q", mode))
	}
	if mode == PGVectorSchemaApplyModeExecute && executor == nil {
		return blockedPGVectorSchemaApplyError(report, errors.New("execute mode requires a pgvector schema executor"))
	}
	if mode == PGVectorSchemaApplyModeExecute && report.Summary.UnsupportedFeatureCount > 0 && !options.AllowUnsupported {
		return blockedPGVectorSchemaApplyError(report, fmt.Errorf("pgvector schema plan contains %d unsupported features; pass allow-unsupported to execute anyway", report.Summary.UnsupportedFeatureCount))
	}
	return nil
}

func blockedPGVectorSchemaApplyError(report *PGVectorSchemaApplyReport, err error) error {
	report.Status = PGVectorSchemaApplyStatusBlocked
	report.Errors = append(report.Errors, err.Error())
	return err
}

func collectPGVectorSchemaPlanWarnings(plan PGVectorSchemaPlan) []string {
	warnings := make([]string, 0, plan.Summary.WarningCount)
	for _, table := range plan.Tables {
		warnings = append(warnings, table.Warnings...)
		for _, column := range table.Columns {
			if column.Warning != "" {
				warnings = append(warnings, column.Warning)
			}
		}
		for _, index := range table.Indexes {
			if index.Warning != "" {
				warnings = append(warnings, index.Warning)
			}
		}
	}
	return warnings
}

func countUnsupportedPGVectorSchemaFeatures(plan PGVectorSchemaPlan) int {
	count := 0
	for _, table := range plan.Tables {
		for _, column := range table.Columns {
			if column.SupportLevel == inspection.SupportLevelUnsupported {
				count++
			}
		}
		for _, index := range table.Indexes {
			if index.SupportLevel == inspection.SupportLevelUnsupported {
				count++
			}
		}
	}
	return count
}

func newPGVectorSchemaApplyReport(plan PGVectorSchemaPlan, options PGVectorSchemaApplyOptions, mode string) PGVectorSchemaApplyReport {
	warnings := collectPGVectorSchemaPlanWarnings(plan)
	unsupportedFeatureCount := countUnsupportedPGVectorSchemaFeatures(plan)
	report := PGVectorSchemaApplyReport{
		SchemaVersion: PGVectorSchemaApplyReportVersion,
		JobID:         options.JobID,
		Mode:          mode,
		SchemaPlan:    options.SchemaPlanPath,
		Target:        plan.Target,
		Warnings:      warnings,
		Summary: PGVectorSchemaApplySummary{
			TableCount:              len(plan.Tables),
			WarningCount:            len(warnings),
			UnsupportedFeatureCount: unsupportedFeatureCount,
		},
	}
	for _, table := range plan.Tables {
		tableReport := PGVectorSchemaApplyTable{
			SourceCollection: table.SourceCollection,
			TargetTable:      table.TargetTable,
			CreateTableSQL:   table.CreateTableSQL,
		}
		for _, index := range table.Indexes {
			tableReport.Indexes = append(tableReport.Indexes, PGVectorSchemaApplyIndex{
				SourceField:    index.SourceField,
				TargetIndex:    index.TargetIndex,
				CreateIndexSQL: index.CreateIndexSQL,
			})
			report.Summary.IndexCount++
		}
		report.Tables = append(report.Tables, tableReport)
	}
	return report
}
