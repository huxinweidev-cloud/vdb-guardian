package migration

import (
	"fmt"
	"strings"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

// RecordMappingPlanVersion identifies the stable JSON schema for full-record
// migration mapping plans derived from pgvector schema planning artifacts.
const RecordMappingPlanVersion = "v1"

const (
	// RecordMappingStatusPass means the schema plan can produce an unambiguous
	// first-phase record mapping.
	RecordMappingStatusPass = "pass"
	// RecordMappingStatusFail means one or more blocking mapping issues were found.
	RecordMappingStatusFail = "fail"
)

const (
	// RecordMappingIssueSeverityBlocker marks an issue that must stop full-record
	// migration until the schema plan or mapping rules are fixed.
	RecordMappingIssueSeverityBlocker = "blocker"
)

const (
	// RecordMappingKindPrimaryKey maps the source primary key to the target row id.
	RecordMappingKindPrimaryKey = "primary_key"
	// RecordMappingKindVector maps the dense vector payload to a pgvector column.
	RecordMappingKindVector = "vector"
	// RecordMappingKindScalar maps a regular scalar source field to a target column.
	RecordMappingKindScalar = "scalar"
	// RecordMappingKindDynamicMetadata maps Milvus dynamic fields to metadata JSON.
	RecordMappingKindDynamicMetadata = "dynamic_metadata"
	// RecordMappingKindPartitionMetadata maps the Milvus partition name to metadata.
	RecordMappingKindPartitionMetadata = "partition_metadata"
)

// RecordMappingOptions configures deterministic mapping-plan construction while
// keeping the phase local-artifact only and credential-free.
type RecordMappingOptions struct {
	SchemaPlanPath string
}

// RecordMappingPlan is a machine-readable plan that classifies how records from
// Milvus collections should be projected into pgvector rows in later phases.
type RecordMappingPlan struct {
	SchemaVersion string                    `json:"schema_version"`
	SchemaPlan    string                    `json:"schema_plan,omitempty"`
	Status        string                    `json:"status"`
	Mappings      []CollectionRecordMapping `json:"mappings"`
	Issues        []RecordMappingIssue      `json:"issues,omitempty"`
	Summary       RecordMappingSummary      `json:"summary"`
}

// CollectionRecordMapping describes the row-level mapping for one source
// collection and one target pgvector table.
type CollectionRecordMapping struct {
	SourceCollection  string               `json:"source_collection"`
	TargetSchema      string               `json:"target_schema"`
	TargetTable       string               `json:"target_table"`
	PrimaryKey        *RecordFieldMapping  `json:"primary_key,omitempty"`
	Vector            *RecordFieldMapping  `json:"vector,omitempty"`
	Scalars           []RecordFieldMapping `json:"scalars,omitempty"`
	DynamicMetadata   *RecordFieldMapping  `json:"dynamic_metadata,omitempty"`
	PartitionMetadata *RecordFieldMapping  `json:"partition_metadata,omitempty"`
	Issues            []RecordMappingIssue `json:"issues,omitempty"`
}

// RecordFieldMapping classifies a single source-field to target-column mapping
// for later full-record migration execution.
type RecordFieldMapping struct {
	Kind         string `json:"kind"`
	SourceField  string `json:"source_field"`
	TargetColumn string `json:"target_column"`
	TargetType   string `json:"target_type"`
	Nullable     bool   `json:"nullable"`
	SupportLevel string `json:"support_level"`
	Warning      string `json:"warning,omitempty"`
}

// RecordMappingIssue explains why a schema plan cannot yet drive safe
// full-record migration, including source and target context for diagnostics.
type RecordMappingIssue struct {
	Severity         string `json:"severity"`
	SourceCollection string `json:"source_collection,omitempty"`
	SourceField      string `json:"source_field,omitempty"`
	TargetColumn     string `json:"target_column,omitempty"`
	Message          string `json:"message"`
}

// RecordMappingSummary aggregates mapping counts for CLI output and migration
// reports without requiring consumers to walk every collection.
type RecordMappingSummary struct {
	CollectionCount               int `json:"collection_count"`
	ScalarMappingCount            int `json:"scalar_mapping_count"`
	DynamicMetadataMappingCount   int `json:"dynamic_metadata_mapping_count"`
	PartitionMetadataMappingCount int `json:"partition_metadata_mapping_count"`
	IssueCount                    int `json:"issue_count"`
	BlockingIssueCount            int `json:"blocking_issue_count"`
}

// BuildRecordMappingPlan converts a credential-free pgvector schema plan into a
// deterministic full-record mapping plan. It does not connect to Milvus or
// PostgreSQL and does not read or write row payloads.
func BuildRecordMappingPlan(plan planschema.PGVectorSchemaPlan, options RecordMappingOptions) (RecordMappingPlan, error) {
	if plan.SchemaVersion != planschema.PGVectorSchemaPlanVersion {
		return RecordMappingPlan{}, fmt.Errorf("unsupported pgvector schema plan version %q", plan.SchemaVersion)
	}
	mappingPlan := RecordMappingPlan{
		SchemaVersion: RecordMappingPlanVersion,
		SchemaPlan:    options.SchemaPlanPath,
		Status:        RecordMappingStatusPass,
		Mappings:      make([]CollectionRecordMapping, 0, len(plan.Tables)),
	}
	for _, table := range plan.Tables {
		mapping := buildCollectionRecordMapping(table)
		mappingPlan.Mappings = append(mappingPlan.Mappings, mapping)
		mappingPlan.Issues = append(mappingPlan.Issues, mapping.Issues...)
	}
	mappingPlan.Summary = buildRecordMappingSummary(mappingPlan)
	if mappingPlan.Summary.BlockingIssueCount > 0 {
		mappingPlan.Status = RecordMappingStatusFail
	}
	return mappingPlan, nil
}

func buildCollectionRecordMapping(table planschema.PGVectorTablePlan) CollectionRecordMapping {
	mapping := CollectionRecordMapping{
		SourceCollection: table.SourceCollection,
		TargetSchema:     table.TargetSchema,
		TargetTable:      table.TargetTable,
	}
	primaryKeyCount := 0
	vectorCount := 0
	for _, column := range table.Columns {
		fieldMapping := recordFieldMappingFromColumn(column)
		if column.SupportLevel == inspection.SupportLevelUnsupported {
			mapping.Issues = append(mapping.Issues, recordMappingIssue(table, column, "unsupported column cannot be migrated safely"))
		}
		switch classifyRecordMappingColumn(column) {
		case RecordMappingKindPrimaryKey:
			primaryKeyCount++
			fieldMapping.Kind = RecordMappingKindPrimaryKey
			if mapping.PrimaryKey == nil {
				mapping.PrimaryKey = &fieldMapping
			}
		case RecordMappingKindVector:
			vectorCount++
			fieldMapping.Kind = RecordMappingKindVector
			if mapping.Vector == nil {
				mapping.Vector = &fieldMapping
			}
		case RecordMappingKindDynamicMetadata:
			fieldMapping.Kind = RecordMappingKindDynamicMetadata
			mapping.DynamicMetadata = &fieldMapping
		case RecordMappingKindPartitionMetadata:
			fieldMapping.Kind = RecordMappingKindPartitionMetadata
			mapping.PartitionMetadata = &fieldMapping
		default:
			fieldMapping.Kind = RecordMappingKindScalar
			mapping.Scalars = append(mapping.Scalars, fieldMapping)
		}
	}
	if primaryKeyCount == 0 {
		mapping.Issues = append(mapping.Issues, RecordMappingIssue{Severity: RecordMappingIssueSeverityBlocker, SourceCollection: table.SourceCollection, Message: "primary key mapping is required"})
	}
	if primaryKeyCount > 1 {
		mapping.Issues = append(mapping.Issues, RecordMappingIssue{Severity: RecordMappingIssueSeverityBlocker, SourceCollection: table.SourceCollection, Message: "multiple primary key mappings are not supported in this phase"})
	}
	if vectorCount == 0 {
		mapping.Issues = append(mapping.Issues, RecordMappingIssue{Severity: RecordMappingIssueSeverityBlocker, SourceCollection: table.SourceCollection, Message: "vector mapping is required"})
	}
	if vectorCount > 1 {
		mapping.Issues = append(mapping.Issues, RecordMappingIssue{Severity: RecordMappingIssueSeverityBlocker, SourceCollection: table.SourceCollection, Message: "multiple vector mappings are not supported in this phase"})
	}
	return mapping
}

func recordFieldMappingFromColumn(column planschema.PGVectorColumnPlan) RecordFieldMapping {
	return RecordFieldMapping{
		SourceField:  column.SourceField,
		TargetColumn: column.TargetColumn,
		TargetType:   column.TargetType,
		Nullable:     column.Nullable,
		SupportLevel: column.SupportLevel,
		Warning:      column.Warning,
	}
}

func classifyRecordMappingColumn(column planschema.PGVectorColumnPlan) string {
	if column.PrimaryKey {
		return RecordMappingKindPrimaryKey
	}
	if column.SourceField == "_milvus_dynamic" {
		return RecordMappingKindDynamicMetadata
	}
	if column.SourceField == "_milvus_partition" {
		return RecordMappingKindPartitionMetadata
	}
	if strings.HasPrefix(strings.ToLower(column.TargetType), "vector") {
		return RecordMappingKindVector
	}
	return RecordMappingKindScalar
}

func recordMappingIssue(table planschema.PGVectorTablePlan, column planschema.PGVectorColumnPlan, message string) RecordMappingIssue {
	if column.Warning != "" {
		message += ": " + column.Warning
	}
	return RecordMappingIssue{Severity: RecordMappingIssueSeverityBlocker, SourceCollection: table.SourceCollection, SourceField: column.SourceField, TargetColumn: column.TargetColumn, Message: message}
}

func buildRecordMappingSummary(plan RecordMappingPlan) RecordMappingSummary {
	summary := RecordMappingSummary{CollectionCount: len(plan.Mappings), IssueCount: len(plan.Issues)}
	for _, mapping := range plan.Mappings {
		summary.ScalarMappingCount += len(mapping.Scalars)
		if mapping.DynamicMetadata != nil {
			summary.DynamicMetadataMappingCount++
		}
		if mapping.PartitionMetadata != nil {
			summary.PartitionMetadataMappingCount++
		}
		for _, issue := range mapping.Issues {
			if issue.Severity == RecordMappingIssueSeverityBlocker {
				summary.BlockingIssueCount++
			}
		}
	}
	return summary
}
