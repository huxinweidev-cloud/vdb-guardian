package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

func TestRunInspectMilvusWritesPlanToFile(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "plan.json")
	fake := fakeInspectMilvusRunner{
		plan: inspection.MilvusInspectionPlan{
			SchemaVersion: inspection.MilvusInspectionSchemaVersion,
			Source:        inspection.MilvusInspectionSource{Type: "milvus", Address: "localhost:19530"},
			Collections: []inspection.MilvusCollectionPlan{
				{Name: "items", Fields: []inspection.MilvusFieldPlan{{Name: "id", SourceType: inspection.MilvusDataTypeInt64, TargetType: "bigint", SupportLevel: inspection.SupportLevelSupported}}},
			},
			Summary: inspection.MilvusInspectionSummary{CollectionCount: 1, SupportedCollectionCount: 1},
		},
	}
	var stdout bytes.Buffer

	err := runInspectMilvusWithRunner(context.Background(), []string{"--milvus-address", "localhost:19530", "--output", outputPath}, fake.run, &stdout)
	if err != nil {
		t.Fatalf("inspect-milvus failed: %v", err)
	}
	if fake.options.Address != "localhost:19530" {
		t.Fatalf("address = %q", fake.options.Address)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read plan output: %v", err)
	}
	var plan inspection.MilvusInspectionPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("plan is not JSON: %v\n%s", err, string(data))
	}
	if plan.Collections[0].Name != "items" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if !strings.Contains(stdout.String(), "inspection completed") || !strings.Contains(stdout.String(), "collections: 1") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
}

func TestRunInspectMilvusPrintsJSONToStdout(t *testing.T) {
	fake := fakeInspectMilvusRunner{plan: inspection.MilvusInspectionPlan{SchemaVersion: inspection.MilvusInspectionSchemaVersion, Summary: inspection.MilvusInspectionSummary{CollectionCount: 0}}}
	var stdout bytes.Buffer

	err := runInspectMilvusWithRunner(context.Background(), []string{"--milvus-address", "localhost:19530"}, fake.run, &stdout)
	if err != nil {
		t.Fatalf("inspect-milvus stdout failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "\"schema_version\": \"v1\"") {
		t.Fatalf("expected JSON stdout, got:\n%s", stdout.String())
	}
}

func TestParseInspectMilvusOptionsRequiresAddress(t *testing.T) {
	_, err := parseInspectMilvusOptions([]string{})
	if err == nil || !strings.Contains(err.Error(), "milvus-address") {
		t.Fatalf("expected address error, got %v", err)
	}
}

type fakeInspectMilvusRunner struct {
	plan    inspection.MilvusInspectionPlan
	options *inspectMilvusOptions
}

func (f *fakeInspectMilvusRunner) run(ctx context.Context, options inspectMilvusOptions) (inspection.MilvusInspectionPlan, error) {
	if err := ctx.Err(); err != nil {
		return inspection.MilvusInspectionPlan{}, err
	}
	f.options = &options
	return f.plan, nil
}
