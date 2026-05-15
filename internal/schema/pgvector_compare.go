package schema

import (
	"fmt"
	"strings"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

// PlanCompareReportVersion identifies the stable JSON schema for reports
// comparing source Milvus inspection plans with generated pgvector schema plans.
const PlanCompareReportVersion = "v1"

const (
	// SchemaPlanCompareStatusPass means the compared plan item satisfied all
	// required equivalence rules.
	SchemaPlanCompareStatusPass = "pass"
	// SchemaPlanCompareStatusFail means at least one required equivalence rule did
	// not hold and the plan should not be applied without review.
	SchemaPlanCompareStatusFail = "fail"
	// SchemaPlanCompareStatusWarn means the plan is structurally usable but carries
	// degraded semantics or non-blocking warnings that require operator review.
	SchemaPlanCompareStatusWarn = "warn"
)

// PlanCompareOptions provides file provenance for schema comparison
// reports without embedding sensitive connection details.
type PlanCompareOptions struct {
	InspectionPlanPath string
	SchemaPlanPath     string
}

// PlanCompareReport describes deterministic validation results between a
// read-only Milvus inspection plan and a dry-run pgvector schema plan.
type PlanCompareReport struct {
	SchemaVersion  string                     `json:"schema_version"`
	Status         string                     `json:"status"`
	InspectionPlan string                     `json:"inspection_plan,omitempty"`
	SchemaPlan     string                     `json:"schema_plan,omitempty"`
	Summary        PlanCompareSummary         `json:"summary"`
	Collections    []PlanCollectionComparison `json:"collections"`
}

// PlanCompareSummary aggregates schema comparison results for CLI output
// and CI policy checks.
type PlanCompareSummary struct {
	CollectionsChecked      int `json:"collections_checked"`
	TablesChecked           int `json:"tables_checked"`
	FieldsChecked           int `json:"fields_checked"`
	ColumnsChecked          int `json:"columns_checked"`
	MismatchCount           int `json:"mismatch_count"`
	WarningCount            int `json:"warning_count"`
	UnsupportedFeatureCount int `json:"unsupported_feature_count"`
}

// PlanCollectionComparison reports all checks and mismatches for one
// Milvus collection and its corresponding pgvector table plan.
type PlanCollectionComparison struct {
	SourceCollection string         `json:"source_collection"`
	TargetTable      string         `json:"target_table,omitempty"`
	Status           string         `json:"status"`
	Checks           []PlanCheck    `json:"checks"`
	Mismatches       []PlanMismatch `json:"mismatches,omitempty"`
	Warnings         []string       `json:"warnings,omitempty"`
}

// PlanCheck records one successful or warning-level equivalence check.
type PlanCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// PlanMismatch records a blocking mismatch between source inspection
// metadata and the generated pgvector target schema plan.
type PlanMismatch struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// CompareSchemaPlans validates that a pgvector schema plan faithfully represents
// the inspected Milvus metadata before the plan is applied to PostgreSQL.
func CompareSchemaPlans(source inspection.MilvusInspectionPlan, target PGVectorSchemaPlan, options PlanCompareOptions) (PlanCompareReport, error) {
	if source.SchemaVersion != inspection.MilvusInspectionSchemaVersion {
		return PlanCompareReport{}, fmt.Errorf("unsupported inspection schema version %q", source.SchemaVersion)
	}
	if target.SchemaVersion != PGVectorSchemaPlanVersion {
		return PlanCompareReport{}, fmt.Errorf("unsupported pgvector schema plan version %q", target.SchemaVersion)
	}
	report := PlanCompareReport{
		SchemaVersion:  PlanCompareReportVersion,
		Status:         SchemaPlanCompareStatusPass,
		InspectionPlan: options.InspectionPlanPath,
		SchemaPlan:     options.SchemaPlanPath,
		Collections:    make([]PlanCollectionComparison, 0, len(source.Collections)),
	}
	tablesBySource := make(map[string]PGVectorTablePlan, len(target.Tables))
	for _, table := range target.Tables {
		tablesBySource[table.SourceCollection] = table
	}
	for _, collection := range source.Collections {
		comparison := compareCollectionSchemaPlan(collection, tablesBySource[collection.Name])
		report.Collections = append(report.Collections, comparison)
	}
	report.Summary = buildPlanCompareSummary(source, target, report.Collections)
	if report.Summary.MismatchCount > 0 {
		report.Status = SchemaPlanCompareStatusFail
	} else if report.Summary.WarningCount > 0 || report.Summary.UnsupportedFeatureCount > 0 {
		report.Status = SchemaPlanCompareStatusWarn
	}
	return report, nil
}

func compareCollectionSchemaPlan(collection inspection.MilvusCollectionPlan, table PGVectorTablePlan) PlanCollectionComparison {
	comparison := PlanCollectionComparison{
		SourceCollection: collection.Name,
		TargetTable:      table.TargetTable,
		Status:           SchemaPlanCompareStatusPass,
		Checks:           make([]PlanCheck, 0),
		Warnings:         append([]string{}, collection.Warnings...),
	}
	if table.SourceCollection == "" {
		comparison.addMismatch("table_present", collection.Name, "", "source collection has no target table plan")
		return comparison.finish()
	}
	expectedTable, err := SanitizePGIdentifier(collection.Name)
	if err != nil {
		comparison.addMismatch("table_identifier_valid", collection.Name, "", err.Error())
	} else if table.TargetTable != expectedTable {
		comparison.addMismatch("table_name_matches", collection.Name, table.TargetTable, fmt.Sprintf("expected target table %q", expectedTable))
	} else {
		comparison.addCheck("table_name_matches", collection.Name, table.TargetTable, "")
	}
	columnsBySource := make(map[string]PGVectorColumnPlan, len(table.Columns))
	for _, column := range table.Columns {
		columnsBySource[column.SourceField] = column
		if column.Warning != "" {
			comparison.Warnings = append(comparison.Warnings, column.Warning)
		}
	}
	for _, field := range collection.Fields {
		column, ok := columnsBySource[field.Name]
		if !ok {
			comparison.addMismatch("field_column_present", field.Name, "", "source field has no target column")
			continue
		}
		compareFieldColumn(&comparison, field, column)
	}
	if collection.DynamicFieldEnabled {
		compareMetadataColumn(&comparison, columnsBySource, "dynamic_field_mapped", "_milvus_dynamic", "jsonb")
	}
	if len(collection.Partitions) > 0 {
		compareMetadataColumn(&comparison, columnsBySource, "partition_metadata_mapped", "_milvus_partition", "text")
	}
	compareIndexes(&comparison, collection, table)
	return comparison.finish()
}

func compareFieldColumn(comparison *PlanCollectionComparison, field inspection.MilvusFieldPlan, column PGVectorColumnPlan) {
	if column.TargetColumn == "" {
		comparison.addMismatch("field_column_present", field.Name, "", "target column name is empty")
		return
	}
	comparison.addCheck("field_column_present", field.Name, column.TargetColumn, "")
	if column.TargetType != field.TargetType {
		comparison.addMismatch("field_type_matches", field.TargetType, column.TargetType, fmt.Sprintf("field %q target type changed", field.Name))
	} else {
		comparison.addCheck("field_type_matches", field.TargetType, column.TargetType, field.Name)
	}
	if field.PrimaryKey {
		if !column.PrimaryKey {
			comparison.addMismatch("primary_key_preserved", field.Name, column.TargetColumn, "source primary key is not marked primary key in target plan")
		} else {
			comparison.addCheck("primary_key_preserved", field.Name, column.TargetColumn, "")
		}
	}
	if column.Nullable != field.Nullable {
		comparison.addMismatch("nullable_matches", fmt.Sprintf("%t", field.Nullable), fmt.Sprintf("%t", column.Nullable), field.Name)
	}
	if field.SourceType == inspection.MilvusDataTypeFloatVector {
		expectedType := fmt.Sprintf("vector(%d)", field.Dimension)
		if field.Dimension <= 0 {
			expectedType = "vector"
		}
		if column.TargetType != expectedType {
			comparison.addMismatch("vector_dimension_preserved", expectedType, column.TargetType, field.Name)
		} else {
			comparison.addCheck("vector_dimension_preserved", fmt.Sprintf("%s(%d)", field.SourceType, field.Dimension), column.TargetType, field.Name)
		}
	}
	if field.SupportLevel == inspection.SupportLevelUnsupported || column.SupportLevel == inspection.SupportLevelUnsupported {
		comparison.Warnings = append(comparison.Warnings, fmt.Sprintf("field %q is unsupported", field.Name))
	}
}

func compareMetadataColumn(comparison *PlanCollectionComparison, columnsBySource map[string]PGVectorColumnPlan, checkName string, sourceField string, targetType string) {
	column, ok := columnsBySource[sourceField]
	if !ok {
		comparison.addMismatch(checkName, sourceField, "", "required metadata column is missing")
		return
	}
	if column.TargetType != targetType {
		comparison.addMismatch(checkName, targetType, column.TargetType, "metadata column type changed")
		return
	}
	comparison.addCheck(checkName, sourceField, fmt.Sprintf("%s %s", column.TargetColumn, column.TargetType), "")
}

func compareIndexes(comparison *PlanCollectionComparison, collection inspection.MilvusCollectionPlan, table PGVectorTablePlan) {
	indexesBySourceField := make(map[string]PGVectorIndexPlan, len(table.Indexes))
	for _, index := range table.Indexes {
		indexesBySourceField[index.SourceField] = index
		if index.Warning != "" {
			comparison.Warnings = append(comparison.Warnings, index.Warning)
		}
	}
	for _, sourceIndex := range collection.Indexes {
		if strings.EqualFold(sourceIndex.TargetIndexType, "flat") {
			comparison.addCheck("index_exact_scan_preserved", sourceIndex.Field, "flat/no index", "")
			continue
		}
		index, ok := indexesBySourceField[sourceIndex.Field]
		if sourceIndex.SupportLevel != inspection.SupportLevelUnsupported && sourceIndex.TargetIndexType != "" && !ok {
			comparison.addMismatch("index_plan_present", sourceIndex.Field, "", "source index recommendation has no target index plan")
			continue
		}
		if ok && index.CreateIndexSQL == "" && index.SupportLevel != inspection.SupportLevelUnsupported {
			comparison.addMismatch("index_ddl_present", sourceIndex.Field, index.TargetIndex, "supported index is missing create_index_sql")
		} else if ok {
			comparison.addCheck("index_plan_present", sourceIndex.Field, index.TargetIndex, "")
		}
	}
}

func (comparison *PlanCollectionComparison) addCheck(name string, source string, target string, detail string) {
	comparison.Checks = append(comparison.Checks, PlanCheck{Name: name, Status: SchemaPlanCompareStatusPass, Source: source, Target: target, Detail: detail})
}

func (comparison *PlanCollectionComparison) addMismatch(name string, source string, target string, detail string) {
	comparison.Mismatches = append(comparison.Mismatches, PlanMismatch{Name: name, Source: source, Target: target, Detail: detail})
}

func (comparison PlanCollectionComparison) finish() PlanCollectionComparison {
	if len(comparison.Mismatches) > 0 {
		comparison.Status = SchemaPlanCompareStatusFail
	} else if len(comparison.Warnings) > 0 {
		comparison.Status = SchemaPlanCompareStatusWarn
	}
	return comparison
}

func buildPlanCompareSummary(source inspection.MilvusInspectionPlan, target PGVectorSchemaPlan, collections []PlanCollectionComparison) PlanCompareSummary {
	summary := PlanCompareSummary{
		CollectionsChecked:      len(source.Collections),
		TablesChecked:           len(target.Tables),
		UnsupportedFeatureCount: source.Summary.UnsupportedFeatureCount + target.Summary.UnsupportedFeatureCount,
	}
	for _, collection := range source.Collections {
		summary.FieldsChecked += len(collection.Fields)
	}
	for _, table := range target.Tables {
		summary.ColumnsChecked += len(table.Columns)
	}
	for _, collection := range collections {
		summary.MismatchCount += len(collection.Mismatches)
		summary.WarningCount += len(collection.Warnings)
	}
	return summary
}
