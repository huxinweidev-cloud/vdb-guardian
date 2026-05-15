package schema

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

// AppliedSchemaCompareReportVersion identifies the stable JSON schema for
// reports comparing planned pgvector schema artifacts with live pgvector schema
// inspection artifacts.
const AppliedSchemaCompareReportVersion = "v1"

// AppliedSchemaCompareOptions carries local artifact provenance for applied
// schema comparison reports without embedding database credentials.
type AppliedSchemaCompareOptions struct {
	SchemaPlanPath string
	LiveSchemaPath string
}

// AppliedSchemaCompareReport describes deterministic validation results between
// a planned pgvector schema artifact and a live PostgreSQL/pgvector schema
// inspection artifact.
type AppliedSchemaCompareReport struct {
	SchemaVersion string                      `json:"schema_version"`
	Status        string                      `json:"status"`
	SchemaPlan    string                      `json:"schema_plan,omitempty"`
	LiveSchema    string                      `json:"live_schema,omitempty"`
	Summary       AppliedSchemaCompareSummary `json:"summary"`
	Tables        []AppliedTableComparison    `json:"tables"`
	Warnings      []string                    `json:"warnings,omitempty"`
}

// AppliedSchemaCompareSummary aggregates applied schema drift checks for CLI
// output and policy gates.
type AppliedSchemaCompareSummary struct {
	TablesChecked  int `json:"tables_checked"`
	ColumnsChecked int `json:"columns_checked"`
	IndexesChecked int `json:"indexes_checked"`
	MismatchCount  int `json:"mismatch_count"`
	WarningCount   int `json:"warning_count"`
}

// AppliedTableComparison reports all planned-vs-live checks for one target
// pgvector table.
type AppliedTableComparison struct {
	TargetTable string                  `json:"target_table"`
	Status      string                  `json:"status"`
	Checks      []AppliedSchemaCheck    `json:"checks"`
	Mismatches  []AppliedSchemaMismatch `json:"mismatches,omitempty"`
	Warnings    []string                `json:"warnings,omitempty"`
}

// AppliedSchemaCheck records one successful applied-schema equivalence check.
type AppliedSchemaCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// AppliedSchemaMismatch records a blocking schema drift mismatch between the
// planned pgvector schema and the live PostgreSQL/pgvector schema.
type AppliedSchemaMismatch struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// CompareAppliedPGVectorSchema validates that a live PostgreSQL/pgvector schema
// inspection matches the planned pgvector schema artifact before record
// migration begins.
//
// It returns a deterministic report and never connects to PostgreSQL. Unsupported
// input artifact schema versions are returned as errors because the comparison
// rules are versioned with those JSON contracts.
func CompareAppliedPGVectorSchema(plan PGVectorSchemaPlan, live PGVectorLiveSchemaInspection, options AppliedSchemaCompareOptions) (AppliedSchemaCompareReport, error) {
	if plan.SchemaVersion != PGVectorSchemaPlanVersion {
		return AppliedSchemaCompareReport{}, fmt.Errorf("unsupported pgvector schema plan version %q", plan.SchemaVersion)
	}
	if live.SchemaVersion != PGVectorLiveSchemaInspectionVersion {
		return AppliedSchemaCompareReport{}, fmt.Errorf("unsupported pgvector live schema inspection version %q", live.SchemaVersion)
	}
	report := AppliedSchemaCompareReport{
		SchemaVersion: AppliedSchemaCompareReportVersion,
		Status:        SchemaPlanCompareStatusPass,
		SchemaPlan:    options.SchemaPlanPath,
		LiveSchema:    options.LiveSchemaPath,
		Tables:        make([]AppliedTableComparison, 0, len(plan.Tables)),
	}
	liveTablesByName := make(map[string]PGVectorLiveTableInspection, len(live.Tables))
	plannedTables := make(map[string]struct{}, len(plan.Tables))
	for _, table := range live.Tables {
		liveTablesByName[table.TargetTable] = table
	}
	for _, plannedTable := range plan.Tables {
		plannedTables[plannedTable.TargetTable] = struct{}{}
		comparison := compareAppliedTableSchema(plannedTable, liveTablesByName[plannedTable.TargetTable], live)
		report.Tables = append(report.Tables, comparison)
	}
	for _, liveTable := range live.Tables {
		if _, ok := plannedTables[liveTable.TargetTable]; !ok {
			report.Warnings = append(report.Warnings, fmt.Sprintf("live table %q is not present in schema plan", liveTable.TargetTable))
		}
	}
	report.Summary = buildAppliedSchemaCompareSummary(plan, report)
	if report.Summary.MismatchCount > 0 {
		report.Status = SchemaPlanCompareStatusFail
	} else if report.Summary.WarningCount > 0 {
		report.Status = SchemaPlanCompareStatusWarn
	}
	return report, nil
}

func compareAppliedTableSchema(plan PGVectorTablePlan, live PGVectorLiveTableInspection, liveSchema PGVectorLiveSchemaInspection) AppliedTableComparison {
	comparison := AppliedTableComparison{
		TargetTable: plan.TargetTable,
		Status:      SchemaPlanCompareStatusPass,
		Checks:      make([]AppliedSchemaCheck, 0),
	}
	if live.TargetTable == "" {
		comparison.addMismatch("table_present", plan.TargetTable, "", "planned table is missing from live pgvector schema")
		return comparison.finish()
	}
	comparison.addCheck("table_present", plan.TargetTable, live.TargetTable, "")
	if plan.TargetSchema != "" && plan.TargetSchema != liveSchema.Target.Schema {
		comparison.addMismatch("target_schema_matches", plan.TargetSchema, liveSchema.Target.Schema, "planned target schema differs from live schema")
	}
	if tablePlansVectorColumn(plan) && !liveSchema.Extension.Installed {
		comparison.addMismatch("pgvector_extension_installed", "vector", "missing", "planned vector columns require pgvector extension")
	}
	liveColumns := make(map[string]PGVectorLiveColumnInspection, len(live.Columns))
	plannedColumns := make(map[string]struct{}, len(plan.Columns))
	for _, column := range live.Columns {
		liveColumns[column.Name] = column
	}
	for _, plannedColumn := range plan.Columns {
		plannedColumns[plannedColumn.TargetColumn] = struct{}{}
		compareAppliedColumn(&comparison, plannedColumn, liveColumns[plannedColumn.TargetColumn])
	}
	for _, liveColumn := range live.Columns {
		if _, ok := plannedColumns[liveColumn.Name]; !ok {
			comparison.Warnings = append(comparison.Warnings, fmt.Sprintf("live column %q.%q is not present in schema plan", live.TargetTable, liveColumn.Name))
		}
	}
	compareAppliedIndexes(&comparison, plan, live)
	return comparison.finish()
}

func compareAppliedColumn(comparison *AppliedTableComparison, planned PGVectorColumnPlan, live PGVectorLiveColumnInspection) {
	if live.Name == "" {
		comparison.addMismatch("column_present", planned.TargetColumn, "", "planned column is missing from live pgvector schema")
		return
	}
	comparison.addCheck("column_present", planned.TargetColumn, live.Name, "")
	if !appliedColumnTypeMatches(planned.TargetType, live) {
		comparison.addMismatch("column_type_matches", planned.TargetType, formatLiveColumnType(live), planned.TargetColumn)
	} else {
		comparison.addCheck("column_type_matches", planned.TargetType, formatLiveColumnType(live), planned.TargetColumn)
	}
	if planned.Nullable != live.Nullable {
		comparison.addMismatch("nullable_matches", fmt.Sprintf("%t", planned.Nullable), fmt.Sprintf("%t", live.Nullable), planned.TargetColumn)
	} else {
		comparison.addCheck("nullable_matches", fmt.Sprintf("%t", planned.Nullable), fmt.Sprintf("%t", live.Nullable), planned.TargetColumn)
	}
	if planned.PrimaryKey {
		if !live.PrimaryKey {
			comparison.addMismatch("primary_key_preserved", planned.TargetColumn, live.Name, "planned primary key column is not primary key live")
		} else {
			comparison.addCheck("primary_key_preserved", planned.TargetColumn, live.Name, "")
		}
	}
	plannedDimension, hasPlannedDimension := parseAppliedVectorDimension(planned.TargetType)
	if hasPlannedDimension {
		if plannedDimension != live.VectorDimension {
			comparison.addMismatch("vector_dimension_preserved", fmt.Sprintf("vector(%d)", plannedDimension), formatLiveColumnType(live), planned.TargetColumn)
		} else {
			comparison.addCheck("vector_dimension_preserved", fmt.Sprintf("vector(%d)", plannedDimension), formatLiveColumnType(live), planned.TargetColumn)
		}
	}
	if planned.Warning != "" {
		comparison.Warnings = append(comparison.Warnings, planned.Warning)
	}
}

func compareAppliedIndexes(comparison *AppliedTableComparison, plan PGVectorTablePlan, live PGVectorLiveTableInspection) {
	liveIndexes := make(map[string]PGVectorLiveIndexInspection, len(live.Indexes))
	plannedIndexes := make(map[string]struct{}, len(plan.Indexes))
	for _, index := range live.Indexes {
		liveIndexes[index.Name] = index
	}
	for _, planned := range plan.Indexes {
		if planned.TargetIndex != "" {
			plannedIndexes[planned.TargetIndex] = struct{}{}
		}
		if planned.Warning != "" {
			comparison.Warnings = append(comparison.Warnings, planned.Warning)
		}
		if !requiresAppliedLiveIndex(planned) {
			continue
		}
		liveIndex, ok := liveIndexes[planned.TargetIndex]
		if !ok {
			comparison.addMismatch("index_present", planned.TargetIndex, "", "planned index is missing from live pgvector schema")
			continue
		}
		comparison.addCheck("index_present", planned.TargetIndex, liveIndex.Name, "")
		if !strings.EqualFold(planned.TargetIndexType, liveIndex.Method) {
			comparison.addMismatch("index_method_matches", planned.TargetIndexType, liveIndex.Method, planned.TargetIndex)
		} else {
			comparison.addCheck("index_method_matches", planned.TargetIndexType, liveIndex.Method, planned.TargetIndex)
		}
	}
	for _, liveIndex := range live.Indexes {
		if _, ok := plannedIndexes[liveIndex.Name]; !ok {
			comparison.Warnings = append(comparison.Warnings, fmt.Sprintf("live index %q.%q is not present in schema plan", live.TargetTable, liveIndex.Name))
		}
	}
}

func appliedColumnTypeMatches(plannedType string, live PGVectorLiveColumnInspection) bool {
	planned := strings.ToLower(strings.TrimSpace(plannedType))
	liveType := strings.ToLower(strings.TrimSpace(formatLiveColumnType(live)))
	if planned == liveType {
		return true
	}
	plannedDimension, hasDimension := parseAppliedVectorDimension(planned)
	if hasDimension && strings.EqualFold(live.Type, "vector") && live.VectorDimension == plannedDimension {
		return true
	}
	return false
}

func formatLiveColumnType(live PGVectorLiveColumnInspection) string {
	if strings.TrimSpace(live.FormattedType) != "" {
		return live.FormattedType
	}
	if live.VectorDimension > 0 && strings.EqualFold(live.Type, "vector") {
		return fmt.Sprintf("vector(%d)", live.VectorDimension)
	}
	return live.Type
}

func tablePlansVectorColumn(table PGVectorTablePlan) bool {
	for _, column := range table.Columns {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(column.TargetType)), "vector") {
			return true
		}
	}
	return false
}

func requiresAppliedLiveIndex(index PGVectorIndexPlan) bool {
	if index.SupportLevel == inspection.SupportLevelUnsupported || index.CreateIndexSQL == "" || index.TargetIndex == "" {
		return false
	}
	return !strings.EqualFold(index.TargetIndexType, "flat")
}

func parseAppliedVectorDimension(targetType string) (int, bool) {
	matches := appliedVectorTypePattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(targetType)))
	if len(matches) != 2 {
		return 0, false
	}
	dimension, err := strconv.Atoi(matches[1])
	if err != nil || dimension <= 0 {
		return 0, false
	}
	return dimension, true
}

func buildAppliedSchemaCompareSummary(plan PGVectorSchemaPlan, report AppliedSchemaCompareReport) AppliedSchemaCompareSummary {
	summary := AppliedSchemaCompareSummary{TablesChecked: len(plan.Tables)}
	for _, table := range report.Tables {
		summary.MismatchCount += len(table.Mismatches)
		summary.WarningCount += len(table.Warnings)
		for _, check := range table.Checks {
			switch check.Name {
			case "column_present":
				summary.ColumnsChecked++
			case "index_present":
				summary.IndexesChecked++
			}
		}
	}
	summary.WarningCount += len(report.Warnings)
	return summary
}

func (comparison *AppliedTableComparison) addCheck(name string, source string, target string, detail string) {
	comparison.Checks = append(comparison.Checks, AppliedSchemaCheck{Name: name, Status: SchemaPlanCompareStatusPass, Source: source, Target: target, Detail: detail})
}

func (comparison *AppliedTableComparison) addMismatch(name string, source string, target string, detail string) {
	comparison.Mismatches = append(comparison.Mismatches, AppliedSchemaMismatch{Name: name, Source: source, Target: target, Detail: detail})
}

func (comparison AppliedTableComparison) finish() AppliedTableComparison {
	sort.SliceStable(comparison.Warnings, func(i, j int) bool { return comparison.Warnings[i] < comparison.Warnings[j] })
	if len(comparison.Mismatches) > 0 {
		comparison.Status = SchemaPlanCompareStatusFail
	} else if len(comparison.Warnings) > 0 {
		comparison.Status = SchemaPlanCompareStatusWarn
	}
	return comparison
}

var appliedVectorTypePattern = regexp.MustCompile(`^vector\((\d+)\)$`)
