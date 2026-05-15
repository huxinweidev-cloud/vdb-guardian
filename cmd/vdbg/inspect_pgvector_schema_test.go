package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

func TestRunInspectPGVectorSchemaRequiresConnectionURL(t *testing.T) {
	var stdout bytes.Buffer
	err := runInspectPGVectorSchemaCommandWithFactory(context.Background(), []string{}, &stdout, func(_ string) (planschema.PGVectorSchemaMetadataClient, func() error, error) {
		t.Fatal("factory should not be called without connection URL")
		return nil, nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "pgvector-connection-url is required") {
		t.Fatalf("expected missing connection URL error, got %v", err)
	}
}

func TestRunInspectPGVectorSchemaWritesOutputWith0600Permissions(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "live-schema.json")
	var stdout bytes.Buffer
	factory := func(connectionURL string) (planschema.PGVectorSchemaMetadataClient, func() error, error) {
		if connectionURL != "postgres://user@example/db" {
			t.Fatalf("unexpected connection URL: %q", connectionURL)
		}
		return &fakeInspectPGVectorSchemaCLIClient{extensionVersion: "0.8.0", columns: []planschema.PGVectorLiveColumnMetadata{{
			TableName:       "items",
			ColumnName:      "embedding",
			FormattedType:   "vector(1536)",
			UDTName:         "vector",
			OrdinalPosition: 1,
		}}}, func() error { return nil }, nil
	}

	err := runInspectPGVectorSchemaCommandWithFactory(context.Background(), []string{
		"--pgvector-connection-url", "postgres://user@example/db",
		"--target-schema", "public",
		"--output", outputPath,
	}, &stdout, factory)
	if err != nil {
		t.Fatalf("runInspectPGVectorSchemaCommandWithFactory returned error: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %v", info.Mode().Perm())
	}
	if strings.Contains(stdout.String(), "user@example") {
		t.Fatalf("stdout leaked connection URL: %q", stdout.String())
	}
	var inspection planschema.PGVectorLiveSchemaInspection
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if err = json.Unmarshal(data, &inspection); err != nil {
		t.Fatalf("parse output JSON: %v", err)
	}
	if inspection.Target.Schema != "public" || inspection.Summary.VectorColumnCount != 1 {
		t.Fatalf("unexpected inspection: %#v", inspection)
	}
}

func TestRunInspectPGVectorSchemaWritesJSONToStdoutWhenNoOutput(t *testing.T) {
	var stdout bytes.Buffer
	factory := func(_ string) (planschema.PGVectorSchemaMetadataClient, func() error, error) {
		return &fakeInspectPGVectorSchemaCLIClient{extensionVersion: "0.8.0"}, nil, nil
	}

	err := runInspectPGVectorSchemaCommandWithFactory(context.Background(), []string{
		"--pgvector-connection-url", "postgres://user@example/db",
	}, &stdout, factory)
	if err != nil {
		t.Fatalf("runInspectPGVectorSchemaCommandWithFactory returned error: %v", err)
	}
	var inspection planschema.PGVectorLiveSchemaInspection
	if err = json.Unmarshal(stdout.Bytes(), &inspection); err != nil {
		t.Fatalf("stdout should be JSON, got %q: %v", stdout.String(), err)
	}
	if inspection.Target.Schema != "public" {
		t.Fatalf("expected default schema public, got %#v", inspection.Target)
	}
}

type fakeInspectPGVectorSchemaCLIClient struct {
	extensionVersion string
	columns          []planschema.PGVectorLiveColumnMetadata
}

func (client *fakeInspectPGVectorSchemaCLIClient) InspectVectorExtension(ctx context.Context) (planschema.PGVectorExtensionMetadata, error) {
	_ = ctx
	return planschema.PGVectorExtensionMetadata{Installed: client.extensionVersion != "", Version: client.extensionVersion}, nil
}

func (client *fakeInspectPGVectorSchemaCLIClient) ListSchemaColumns(ctx context.Context, schema string) ([]planschema.PGVectorLiveColumnMetadata, error) {
	_ = ctx
	_ = schema
	return append([]planschema.PGVectorLiveColumnMetadata(nil), client.columns...), nil
}

func (client *fakeInspectPGVectorSchemaCLIClient) ListPrimaryKeys(ctx context.Context, schema string) ([]planschema.PGVectorLivePrimaryKeyMetadata, error) {
	_ = ctx
	_ = schema
	return nil, nil
}

func (client *fakeInspectPGVectorSchemaCLIClient) ListIndexes(ctx context.Context, schema string) ([]planschema.PGVectorLiveIndexMetadata, error) {
	_ = ctx
	_ = schema
	return nil, nil
}
