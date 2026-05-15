package schema

import (
	"fmt"
	"strings"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

// PGVectorSchemaPlanVersion identifies the stable JSON schema for pgvector
// schema planning artifacts generated from Milvus inspection plans.
const PGVectorSchemaPlanVersion = "v1"

// PGVectorSchemaPlan is a machine-readable, dry-run schema plan for creating
// PostgreSQL/pgvector structures corresponding to inspected Milvus collections.
type PGVectorSchemaPlan struct {
	SchemaVersion string                `json:"schema_version"`
	SourcePlan    string                `json:"source_plan,omitempty"`
	Target        PGVectorPlanTarget    `json:"target"`
	Tables        []PGVectorTablePlan   `json:"tables"`
	Summary       PGVectorSchemaSummary `json:"summary"`
}

// PGVectorPlanTarget describes the target database family and schema for the
// generated DDL preview. It intentionally excludes connection credentials.
type PGVectorPlanTarget struct {
	Type   string `json:"type"`
	Schema string `json:"schema"`
}

// PGVectorTablePlan describes one target table and its generated DDL preview.
type PGVectorTablePlan struct {
	SourceCollection string               `json:"source_collection"`
	TargetSchema     string               `json:"target_schema"`
	TargetTable      string               `json:"target_table"`
	Columns          []PGVectorColumnPlan `json:"columns"`
	CreateTableSQL   string               `json:"create_table_sql"`
	Indexes          []PGVectorIndexPlan  `json:"indexes,omitempty"`
	Warnings         []string             `json:"warnings,omitempty"`
}

// PGVectorColumnPlan describes one target column derived from a Milvus field or
// from migration metadata such as dynamic fields and partition names.
type PGVectorColumnPlan struct {
	SourceField  string `json:"source_field"`
	TargetColumn string `json:"target_column"`
	TargetType   string `json:"target_type"`
	PrimaryKey   bool   `json:"primary_key,omitempty"`
	Nullable     bool   `json:"nullable"`
	SupportLevel string `json:"support_level"`
	Warning      string `json:"warning,omitempty"`
}

// PGVectorIndexPlan describes an index DDL recommendation derived from Milvus
// vector index metadata. Unsupported indexes are retained as warnings without
// executable SQL.
type PGVectorIndexPlan struct {
	SourceField     string `json:"source_field,omitempty"`
	TargetSchema    string `json:"target_schema,omitempty"`
	TargetTable     string `json:"target_table,omitempty"`
	TargetColumn    string `json:"target_column,omitempty"`
	TargetIndex     string `json:"target_index,omitempty"`
	TargetIndexType string `json:"target_index_type,omitempty"`
	TargetOps       string `json:"target_ops,omitempty"`
	CreateIndexSQL  string `json:"create_index_sql,omitempty"`
	SupportLevel    string `json:"support_level"`
	Warning         string `json:"warning,omitempty"`
}

// PGVectorSchemaSummary aggregates plan risks for CLI output and CI policy
// checks without requiring consumers to walk every generated table.
type PGVectorSchemaSummary struct {
	TableCount              int `json:"table_count"`
	WarningCount            int `json:"warning_count"`
	UnsupportedFeatureCount int `json:"unsupported_feature_count"`
}

// PGVectorSchemaPlannerOptions configures deterministic pgvector schema plan
// generation while keeping phase two dry-run and credential-free.
type PGVectorSchemaPlannerOptions struct {
	TargetSchema string
	SourcePlan   string
}

// BuildPGVectorSchemaPlan converts a read-only Milvus inspection plan into a
// PostgreSQL/pgvector schema and index DDL preview. It does not connect to or
// mutate PostgreSQL.
func BuildPGVectorSchemaPlan(source inspection.MilvusInspectionPlan, options PGVectorSchemaPlannerOptions) (PGVectorSchemaPlan, error) {
	if source.SchemaVersion != inspection.MilvusInspectionSchemaVersion {
		return PGVectorSchemaPlan{}, fmt.Errorf("unsupported inspection schema version %q", source.SchemaVersion)
	}
	targetSchema := options.TargetSchema
	if targetSchema == "" {
		targetSchema = "public"
	}
	targetSchema, err := SanitizePGIdentifier(targetSchema)
	if err != nil {
		return PGVectorSchemaPlan{}, fmt.Errorf("sanitize target schema: %w", err)
	}
	plan := PGVectorSchemaPlan{
		SchemaVersion: PGVectorSchemaPlanVersion,
		SourcePlan:    options.SourcePlan,
		Target:        PGVectorPlanTarget{Type: "pgvector", Schema: targetSchema},
		Tables:        make([]PGVectorTablePlan, 0, len(source.Collections)),
	}
	for _, collection := range source.Collections {
		table, err := buildTablePlan(collection, targetSchema)
		if err != nil {
			return PGVectorSchemaPlan{}, err
		}
		plan.Tables = append(plan.Tables, table)
	}
	plan.Summary = buildSchemaSummary(plan.Tables)
	return plan, nil
}

func buildTablePlan(collection inspection.MilvusCollectionPlan, targetSchema string) (PGVectorTablePlan, error) {
	targetTable, err := SanitizePGIdentifier(collection.Name)
	if err != nil {
		return PGVectorTablePlan{}, fmt.Errorf("sanitize collection %q: %w", collection.Name, err)
	}
	fieldNames := make([]string, 0, len(collection.Fields)+2)
	for _, field := range collection.Fields {
		fieldNames = append(fieldNames, field.Name)
	}
	if collection.DynamicFieldEnabled {
		fieldNames = append(fieldNames, "_milvus_dynamic")
	}
	if len(collection.Partitions) > 0 {
		fieldNames = append(fieldNames, "_milvus_partition")
	}
	identifiers, err := UniquePGIdentifiers(fieldNames)
	if err != nil {
		return PGVectorTablePlan{}, fmt.Errorf("sanitize fields for collection %q: %w", collection.Name, err)
	}
	table := PGVectorTablePlan{
		SourceCollection: collection.Name,
		TargetSchema:     targetSchema,
		TargetTable:      targetTable,
		Columns:          make([]PGVectorColumnPlan, 0, len(fieldNames)),
		Warnings:         append([]string{}, collection.Warnings...),
	}
	for _, field := range collection.Fields {
		column := PGVectorColumnPlan{
			SourceField:  field.Name,
			TargetColumn: identifiers[field.Name],
			TargetType:   field.TargetType,
			PrimaryKey:   field.PrimaryKey,
			Nullable:     field.Nullable,
			SupportLevel: field.SupportLevel,
			Warning:      field.Warning,
		}
		if column.TargetType == "" {
			column.TargetType = "jsonb"
			column.SupportLevel = inspection.SupportLevelUnsupported
			column.Warning = fmt.Sprintf("field %q has no target type recommendation", field.Name)
		}
		table.Columns = append(table.Columns, column)
	}
	if collection.DynamicFieldEnabled {
		table.Columns = append(table.Columns, PGVectorColumnPlan{SourceField: "_milvus_dynamic", TargetColumn: identifiers["_milvus_dynamic"], TargetType: "jsonb", Nullable: true, SupportLevel: inspection.SupportLevelDegraded})
	}
	if len(collection.Partitions) > 0 {
		table.Columns = append(table.Columns, PGVectorColumnPlan{SourceField: "_milvus_partition", TargetColumn: identifiers["_milvus_partition"], TargetType: "text", Nullable: true, SupportLevel: inspection.SupportLevelDegraded})
	}
	table.CreateTableSQL, err = RenderCreateTableSQL(table)
	if err != nil {
		return PGVectorTablePlan{}, err
	}
	table.Indexes = buildIndexPlans(collection, table, identifiers)
	return table, nil
}

// RenderCreateTableSQL renders a deterministic CREATE EXTENSION plus CREATE
// TABLE preview for a target pgvector table. It assumes identifiers were already
// sanitized by the planner.
func RenderCreateTableSQL(table PGVectorTablePlan) (string, error) {
	if table.TargetSchema == "" || table.TargetTable == "" {
		return "", fmt.Errorf("target schema and table are required")
	}
	if len(table.Columns) == 0 {
		return "", fmt.Errorf("at least one column is required")
	}
	lines := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		if column.TargetColumn == "" || column.TargetType == "" {
			return "", fmt.Errorf("column name and type are required")
		}
		line := fmt.Sprintf("  %s %s", column.TargetColumn, column.TargetType)
		if column.PrimaryKey {
			line += " PRIMARY KEY"
		} else if !column.Nullable {
			line += " NOT NULL"
		}
		lines = append(lines, line)
	}
	return fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS vector;\nCREATE TABLE IF NOT EXISTS %s.%s (\n%s\n);", table.TargetSchema, table.TargetTable, strings.Join(lines, ",\n")), nil
}

// RenderCreateIndexSQL renders a deterministic CREATE INDEX preview for a
// supported pgvector vector index recommendation.
func RenderCreateIndexSQL(index PGVectorIndexPlan) (string, error) {
	if index.TargetIndex == "" || index.TargetSchema == "" || index.TargetTable == "" || index.TargetIndexType == "" || index.TargetColumn == "" {
		return "", fmt.Errorf("target index, schema, table, index type, and column are required")
	}
	ops := ""
	if index.TargetOps != "" {
		ops = " " + index.TargetOps
	}
	return fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s.%s USING %s (%s%s);", index.TargetIndex, index.TargetSchema, index.TargetTable, index.TargetIndexType, index.TargetColumn, ops), nil
}

func buildIndexPlans(collection inspection.MilvusCollectionPlan, table PGVectorTablePlan, identifiers map[string]string) []PGVectorIndexPlan {
	indexes := make([]PGVectorIndexPlan, 0, len(collection.Indexes))
	for _, sourceIndex := range collection.Indexes {
		targetColumn := identifiers[sourceIndex.Field]
		if targetColumn == "" || sourceIndex.TargetIndexType == "" {
			if sourceIndex.Warning != "" {
				table.Warnings = append(table.Warnings, sourceIndex.Warning)
			}
			continue
		}
		if strings.EqualFold(sourceIndex.TargetIndexType, "flat") {
			continue
		}
		targetIndex := fmt.Sprintf("%s_%s_%s_idx", table.TargetTable, targetColumn, strings.ToLower(sourceIndex.TargetIndexType))
		plan := PGVectorIndexPlan{
			SourceField:     sourceIndex.Field,
			TargetSchema:    table.TargetSchema,
			TargetTable:     table.TargetTable,
			TargetColumn:    targetColumn,
			TargetIndex:     targetIndex,
			TargetIndexType: strings.ToLower(sourceIndex.TargetIndexType),
			TargetOps:       sourceIndex.TargetOps,
			SupportLevel:    sourceIndex.SupportLevel,
			Warning:         sourceIndex.Warning,
		}
		if sql, err := RenderCreateIndexSQL(plan); err == nil {
			plan.CreateIndexSQL = sql
		} else {
			plan.SupportLevel = inspection.SupportLevelUnsupported
			plan.Warning = err.Error()
		}
		indexes = append(indexes, plan)
	}
	return indexes
}

func buildSchemaSummary(tables []PGVectorTablePlan) PGVectorSchemaSummary {
	summary := PGVectorSchemaSummary{TableCount: len(tables)}
	for _, table := range tables {
		summary.WarningCount += len(table.Warnings)
		for _, column := range table.Columns {
			if column.Warning != "" {
				summary.WarningCount++
			}
			if column.SupportLevel == inspection.SupportLevelUnsupported {
				summary.UnsupportedFeatureCount++
			}
		}
		for _, index := range table.Indexes {
			if index.Warning != "" {
				summary.WarningCount++
			}
			if index.SupportLevel == inspection.SupportLevelUnsupported {
				summary.UnsupportedFeatureCount++
			}
		}
	}
	return summary
}
