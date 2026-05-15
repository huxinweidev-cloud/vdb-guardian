package schema

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

const (
	// PGVectorLiveSchemaInspectionVersion identifies the stable JSON schema for
	// live pgvector schema inspection artifacts.
	PGVectorLiveSchemaInspectionVersion = "v1"
)

// PGVectorSchemaMetadataClient defines the read-only PostgreSQL metadata queries
// required to inspect an applied pgvector schema without executing DDL or DML.
type PGVectorSchemaMetadataClient interface {
	InspectVectorExtension(ctx context.Context) (PGVectorExtensionMetadata, error)
	ListSchemaColumns(ctx context.Context, schema string) ([]PGVectorLiveColumnMetadata, error)
	ListPrimaryKeys(ctx context.Context, schema string) ([]PGVectorLivePrimaryKeyMetadata, error)
	ListIndexes(ctx context.Context, schema string) ([]PGVectorLiveIndexMetadata, error)
}

// PGVectorLiveSchemaInspectOptions configures read-only live pgvector schema
// inspection.
type PGVectorLiveSchemaInspectOptions struct {
	TargetSchema string
}

// PGVectorExtensionMetadata contains raw pg_extension metadata for the vector
// extension.
type PGVectorExtensionMetadata struct {
	Installed bool
	Version   string
}

// PGVectorLiveColumnMetadata contains raw read-only metadata for a PostgreSQL
// column in the inspected schema.
type PGVectorLiveColumnMetadata struct {
	TableName       string
	ColumnName      string
	FormattedType   string
	DataType        string
	UDTName         string
	IsNullable      bool
	OrdinalPosition int
}

// PGVectorLivePrimaryKeyMetadata contains raw read-only metadata for a primary
// key column.
type PGVectorLivePrimaryKeyMetadata struct {
	TableName  string
	ColumnName string
}

// PGVectorLiveIndexMetadata contains raw read-only metadata for an index in the
// inspected schema.
type PGVectorLiveIndexMetadata struct {
	TableName  string
	IndexName  string
	Method     string
	Definition string
}

// PGVectorLiveSchemaInspection is the deterministic JSON artifact emitted by
// live pgvector schema inspection.
type PGVectorLiveSchemaInspection struct {
	SchemaVersion string                        `json:"schema_version"`
	Target        PGVectorLiveSchemaTarget      `json:"target"`
	Extension     PGVectorExtensionInspection   `json:"extension"`
	Tables        []PGVectorLiveTableInspection `json:"tables"`
	Warnings      []string                      `json:"warnings,omitempty"`
	Summary       PGVectorLiveSchemaSummary     `json:"summary"`
}

// PGVectorLiveSchemaTarget identifies the inspected pgvector target schema.
type PGVectorLiveSchemaTarget struct {
	Type   string `json:"type"`
	Schema string `json:"schema"`
}

// PGVectorExtensionInspection records whether the pgvector extension is installed.
type PGVectorExtensionInspection struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
}

// PGVectorLiveTableInspection describes one live PostgreSQL table discovered in
// the inspected schema.
type PGVectorLiveTableInspection struct {
	TargetTable string                         `json:"target_table"`
	Columns     []PGVectorLiveColumnInspection `json:"columns"`
	Indexes     []PGVectorLiveIndexInspection  `json:"indexes,omitempty"`
}

// PGVectorLiveColumnInspection describes one live PostgreSQL column, including
// pgvector dimensions when present.
type PGVectorLiveColumnInspection struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	FormattedType   string `json:"formatted_type"`
	Nullable        bool   `json:"nullable"`
	PrimaryKey      bool   `json:"primary_key"`
	VectorDimension int    `json:"vector_dimension,omitempty"`
}

// PGVectorLiveIndexInspection describes one live PostgreSQL index.
type PGVectorLiveIndexInspection struct {
	Name       string `json:"name"`
	Method     string `json:"method"`
	Definition string `json:"definition"`
}

// PGVectorLiveSchemaSummary contains aggregate counts for live schema inspection.
type PGVectorLiveSchemaSummary struct {
	TableCount        int `json:"table_count"`
	ColumnCount       int `json:"column_count"`
	VectorColumnCount int `json:"vector_column_count"`
	IndexCount        int `json:"index_count"`
	WarningCount      int `json:"warning_count"`
}

// InspectPGVectorSchema builds a deterministic, read-only inspection artifact
// from PostgreSQL catalog metadata supplied by the client.
func InspectPGVectorSchema(ctx context.Context, client PGVectorSchemaMetadataClient, options PGVectorLiveSchemaInspectOptions) (PGVectorLiveSchemaInspection, error) {
	if client == nil {
		return PGVectorLiveSchemaInspection{}, fmt.Errorf("pgvector schema metadata client is required")
	}
	targetSchema := options.TargetSchema
	if targetSchema == "" {
		targetSchema = "public"
	}
	extension, err := client.InspectVectorExtension(ctx)
	if err != nil {
		return PGVectorLiveSchemaInspection{}, fmt.Errorf("inspect pgvector extension: %w", err)
	}
	columns, err := client.ListSchemaColumns(ctx, targetSchema)
	if err != nil {
		return PGVectorLiveSchemaInspection{}, fmt.Errorf("list pgvector schema columns: %w", err)
	}
	primaryKeys, err := client.ListPrimaryKeys(ctx, targetSchema)
	if err != nil {
		return PGVectorLiveSchemaInspection{}, fmt.Errorf("list pgvector primary keys: %w", err)
	}
	indexes, err := client.ListIndexes(ctx, targetSchema)
	if err != nil {
		return PGVectorLiveSchemaInspection{}, fmt.Errorf("list pgvector indexes: %w", err)
	}
	inspection := buildPGVectorLiveSchemaInspection(targetSchema, extension, columns, primaryKeys, indexes)
	return inspection, nil
}

func buildPGVectorLiveSchemaInspection(schema string, extension PGVectorExtensionMetadata, columns []PGVectorLiveColumnMetadata, primaryKeys []PGVectorLivePrimaryKeyMetadata, indexes []PGVectorLiveIndexMetadata) PGVectorLiveSchemaInspection {
	primaryKeySet := make(map[string]bool, len(primaryKeys))
	for _, key := range primaryKeys {
		primaryKeySet[key.TableName+"\x00"+key.ColumnName] = true
	}
	indexByTable := make(map[string][]PGVectorLiveIndexInspection)
	for _, index := range indexes {
		indexByTable[index.TableName] = append(indexByTable[index.TableName], PGVectorLiveIndexInspection{Name: index.IndexName, Method: index.Method, Definition: index.Definition})
	}
	columnsByTable := make(map[string][]PGVectorLiveColumnMetadata)
	for _, column := range columns {
		columnsByTable[column.TableName] = append(columnsByTable[column.TableName], column)
	}
	tableNames := make([]string, 0, len(columnsByTable))
	for tableName := range columnsByTable {
		tableNames = append(tableNames, tableName)
	}
	sort.Strings(tableNames)
	inspection := PGVectorLiveSchemaInspection{
		SchemaVersion: PGVectorLiveSchemaInspectionVersion,
		Target:        PGVectorLiveSchemaTarget{Type: "pgvector", Schema: schema},
		Extension:     PGVectorExtensionInspection{Name: "vector", Installed: extension.Installed, Version: extension.Version},
		Tables:        make([]PGVectorLiveTableInspection, 0, len(tableNames)),
	}
	if !extension.Installed {
		inspection.Warnings = append(inspection.Warnings, "pgvector extension is not installed")
	}
	for _, tableName := range tableNames {
		tableColumns := columnsByTable[tableName]
		sort.SliceStable(tableColumns, func(left int, right int) bool {
			if tableColumns[left].OrdinalPosition == tableColumns[right].OrdinalPosition {
				return tableColumns[left].ColumnName < tableColumns[right].ColumnName
			}
			return tableColumns[left].OrdinalPosition < tableColumns[right].OrdinalPosition
		})
		table := PGVectorLiveTableInspection{TargetTable: tableName, Columns: make([]PGVectorLiveColumnInspection, 0, len(tableColumns))}
		for _, column := range tableColumns {
			vectorDimension := ParsePGVectorDimension(column.FormattedType)
			if vectorDimension > 0 {
				inspection.Summary.VectorColumnCount++
			}
			table.Columns = append(table.Columns, PGVectorLiveColumnInspection{
				Name:            column.ColumnName,
				Type:            normalizePGVectorLiveColumnType(column),
				FormattedType:   column.FormattedType,
				Nullable:        column.IsNullable,
				PrimaryKey:      primaryKeySet[column.TableName+"\x00"+column.ColumnName],
				VectorDimension: vectorDimension,
			})
			inspection.Summary.ColumnCount++
		}
		table.Indexes = append(table.Indexes, indexByTable[tableName]...)
		sort.SliceStable(table.Indexes, func(left int, right int) bool { return table.Indexes[left].Name < table.Indexes[right].Name })
		inspection.Summary.IndexCount += len(table.Indexes)
		inspection.Tables = append(inspection.Tables, table)
	}
	inspection.Summary.TableCount = len(inspection.Tables)
	inspection.Summary.WarningCount = len(inspection.Warnings)
	return inspection
}

func normalizePGVectorLiveColumnType(column PGVectorLiveColumnMetadata) string {
	if ParsePGVectorDimension(column.FormattedType) > 0 || column.UDTName == "vector" {
		return "vector"
	}
	if column.FormattedType != "" {
		return column.FormattedType
	}
	if column.DataType != "" {
		return column.DataType
	}
	return column.UDTName
}

var pgvectorDimensionPattern = regexp.MustCompile(`(?:^|\.)vector\((\d+)\)$`)

// ParsePGVectorDimension extracts the vector dimension from PostgreSQL
// format_type output such as vector(1536) or public.vector(768).
func ParsePGVectorDimension(formattedType string) int {
	matches := pgvectorDimensionPattern.FindStringSubmatch(formattedType)
	if len(matches) != 2 {
		return 0
	}
	dimension, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return dimension
}
