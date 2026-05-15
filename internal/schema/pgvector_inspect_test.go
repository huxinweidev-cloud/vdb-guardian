package schema

import (
	"context"
	"reflect"
	"testing"
)

func TestInspectPGVectorSchemaBuildsDeterministicSummary(t *testing.T) {
	client := &fakePGVectorSchemaMetadataClient{
		extensionVersion: "0.8.0",
		columns: []PGVectorLiveColumnMetadata{{
			TableName:       "items",
			ColumnName:      "embedding",
			FormattedType:   "vector(1536)",
			DataType:        "USER-DEFINED",
			UDTName:         "vector",
			IsNullable:      true,
			OrdinalPosition: 2,
		}, {
			TableName:       "items",
			ColumnName:      "id",
			FormattedType:   "bigint",
			DataType:        "bigint",
			UDTName:         "int8",
			IsNullable:      false,
			OrdinalPosition: 1,
		}},
		primaryKeys: []PGVectorLivePrimaryKeyMetadata{{TableName: "items", ColumnName: "id"}},
		indexes: []PGVectorLiveIndexMetadata{{
			TableName:  "items",
			IndexName:  "items_embedding_hnsw_idx",
			Method:     "hnsw",
			Definition: "CREATE INDEX items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops)",
		}},
	}

	inspection, err := InspectPGVectorSchema(context.Background(), client, PGVectorLiveSchemaInspectOptions{TargetSchema: "public"})
	if err != nil {
		t.Fatalf("InspectPGVectorSchema returned error: %v", err)
	}
	if inspection.SchemaVersion != PGVectorLiveSchemaInspectionVersion {
		t.Fatalf("unexpected schema version: %q", inspection.SchemaVersion)
	}
	if !inspection.Extension.Installed || inspection.Extension.Version != "0.8.0" {
		t.Fatalf("unexpected extension inspection: %#v", inspection.Extension)
	}
	if inspection.Summary.TableCount != 1 || inspection.Summary.ColumnCount != 2 || inspection.Summary.VectorColumnCount != 1 || inspection.Summary.IndexCount != 1 {
		t.Fatalf("unexpected summary: %#v", inspection.Summary)
	}
	columns := inspection.Tables[0].Columns
	if got := []string{columns[0].Name, columns[1].Name}; !reflect.DeepEqual(got, []string{"id", "embedding"}) {
		t.Fatalf("columns not sorted by ordinal position: %#v", got)
	}
	if !columns[0].PrimaryKey {
		t.Fatalf("expected id to be primary key: %#v", columns[0])
	}
	if columns[1].VectorDimension != 1536 {
		t.Fatalf("expected vector dimension 1536, got %#v", columns[1])
	}
	if inspection.Tables[0].Indexes[0].Method != "hnsw" {
		t.Fatalf("unexpected index: %#v", inspection.Tables[0].Indexes[0])
	}
}

func TestInspectPGVectorSchemaWarnsWhenVectorExtensionMissing(t *testing.T) {
	client := &fakePGVectorSchemaMetadataClient{
		extensionInstalled: false,
		columns: []PGVectorLiveColumnMetadata{{
			TableName:       "items",
			ColumnName:      "id",
			FormattedType:   "bigint",
			DataType:        "bigint",
			UDTName:         "int8",
			OrdinalPosition: 1,
		}},
	}

	inspection, err := InspectPGVectorSchema(context.Background(), client, PGVectorLiveSchemaInspectOptions{TargetSchema: "public"})
	if err != nil {
		t.Fatalf("InspectPGVectorSchema returned error: %v", err)
	}
	if inspection.Extension.Installed {
		t.Fatalf("expected vector extension to be missing: %#v", inspection.Extension)
	}
	if inspection.Summary.WarningCount != 1 {
		t.Fatalf("expected one warning, got summary %#v warnings %#v", inspection.Summary, inspection.Warnings)
	}
	if inspection.Warnings[0] != "pgvector extension is not installed" {
		t.Fatalf("unexpected warning: %#v", inspection.Warnings)
	}
}

func TestParsePGVectorDimension(t *testing.T) {
	tests := []struct {
		formattedType string
		want          int
	}{
		{formattedType: "vector(1536)", want: 1536},
		{formattedType: "public.vector(768)", want: 768},
		{formattedType: "bigint", want: 0},
		{formattedType: "vector", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.formattedType, func(t *testing.T) {
			if got := ParsePGVectorDimension(tt.formattedType); got != tt.want {
				t.Fatalf("ParsePGVectorDimension(%q) = %d, want %d", tt.formattedType, got, tt.want)
			}
		})
	}
}

type fakePGVectorSchemaMetadataClient struct {
	extensionInstalled bool
	extensionVersion   string
	columns            []PGVectorLiveColumnMetadata
	primaryKeys        []PGVectorLivePrimaryKeyMetadata
	indexes            []PGVectorLiveIndexMetadata
}

func (client *fakePGVectorSchemaMetadataClient) InspectVectorExtension(ctx context.Context) (PGVectorExtensionMetadata, error) {
	_ = ctx
	if client.extensionVersion != "" {
		return PGVectorExtensionMetadata{Installed: true, Version: client.extensionVersion}, nil
	}
	return PGVectorExtensionMetadata{Installed: client.extensionInstalled}, nil
}

func (client *fakePGVectorSchemaMetadataClient) ListSchemaColumns(ctx context.Context, schema string) ([]PGVectorLiveColumnMetadata, error) {
	_ = ctx
	_ = schema
	return append([]PGVectorLiveColumnMetadata(nil), client.columns...), nil
}

func (client *fakePGVectorSchemaMetadataClient) ListPrimaryKeys(ctx context.Context, schema string) ([]PGVectorLivePrimaryKeyMetadata, error) {
	_ = ctx
	_ = schema
	return append([]PGVectorLivePrimaryKeyMetadata(nil), client.primaryKeys...), nil
}

func (client *fakePGVectorSchemaMetadataClient) ListIndexes(ctx context.Context, schema string) ([]PGVectorLiveIndexMetadata, error) {
	_ = ctx
	_ = schema
	return append([]PGVectorLiveIndexMetadata(nil), client.indexes...), nil
}
