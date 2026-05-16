package migration

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestBuildVectorMigrationReport(t *testing.T) {
	result := VectorMigrationResult{
		SourceCollection: "items",
		TargetTable:      "items",
		Dimension:        8,
		RecordsRead:      100,
		RecordsWritten:   100,
	}
	report := BuildVectorMigrationReport(result, VectorMigrationReportOptions{
		JobID:             "migration-smoke",
		SchemaPreflight:   true,
		SchemaComparePath: "/tmp/schema-compare.json",
		Mapping: &VectorMigrationReportMapping{
			SchemaPlan:                    "/tmp/schema-plan.json",
			Status:                        RecordMappingStatusPass,
			ScalarMappingCount:            1,
			DynamicMetadataMappingCount:   1,
			PartitionMetadataMappingCount: 1,
			BlockingIssueCount:            0,
		},
	})

	if report.SchemaVersion != VectorMigrationReportVersion {
		t.Fatalf("unexpected schema version: %s", report.SchemaVersion)
	}
	if report.Status != "completed" {
		t.Fatalf("unexpected status: %s", report.Status)
	}
	if report.Source.Type != "milvus" || report.Source.Collection != "items" {
		t.Fatalf("unexpected source: %+v", report.Source)
	}
	if report.Target.Type != "pgvector" || report.Target.Table != "items" {
		t.Fatalf("unexpected target: %+v", report.Target)
	}
	if report.Summary.RecordsRead != 100 || report.Summary.RecordsWritten != 100 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	if !report.Preflight.SchemaMatchRequired || report.Preflight.SchemaCompareStatus != "pass" {
		t.Fatalf("unexpected preflight: %+v", report.Preflight)
	}
	if report.Mapping == nil || report.Mapping.SchemaPlan != "/tmp/schema-plan.json" || report.Mapping.Status != RecordMappingStatusPass || report.Mapping.ScalarMappingCount != 1 || report.Mapping.DynamicMetadataMappingCount != 1 || report.Mapping.PartitionMetadataMappingCount != 1 || report.Mapping.BlockingIssueCount != 0 {
		t.Fatalf("unexpected mapping summary: %+v", report.Mapping)
	}
}

func TestVectorMigrationRunnerMigratesReadRecordsIntoWriter(t *testing.T) {
	ctx := context.Background()
	source := &fakeVectorMigrationSource{
		records: []VectorMigrationRecord{
			{ID: "vec-1", Vector: []float64{0.1, 0.2, 0.3}},
			{ID: "vec-2", Vector: []float64{0.4, 0.5, 0.6}},
		},
	}
	target := &fakeVectorMigrationTarget{}
	runner, err := NewVectorMigrationRunner(VectorMigrationConfig{
		SourceCollection: "items",
		TargetTable:      "items_copy",
		Dimension:        3,
		BatchSize:        2,
	}, source, target)
	if err != nil {
		t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
	}

	result, err := runner.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	if result.SourceCollection != "items" {
		t.Fatalf("SourceCollection = %q, want items", result.SourceCollection)
	}
	if result.TargetTable != "items_copy" {
		t.Fatalf("TargetTable = %q, want items_copy", result.TargetTable)
	}
	if result.Dimension != 3 {
		t.Fatalf("Dimension = %d, want 3", result.Dimension)
	}
	if result.RecordsRead != 2 || result.RecordsWritten != 2 {
		t.Fatalf("record counts = read %d written %d, want 2/2", result.RecordsRead, result.RecordsWritten)
	}
	if source.collection != "items" {
		t.Fatalf("source collection = %q, want items", source.collection)
	}
	if target.table != "items_copy" {
		t.Fatalf("target table = %q, want items_copy", target.table)
	}
	if !reflect.DeepEqual(target.records, source.records) {
		t.Fatalf("written records = %#v, want %#v", target.records, source.records)
	}
	if &target.records[0].Vector[0] == &source.records[0].Vector[0] {
		t.Fatal("expected migration runner to pass copied vectors to target writer")
	}
}

func TestVectorMigrationRunnerAppliesDefaults(t *testing.T) {
	runner, err := NewVectorMigrationRunner(VectorMigrationConfig{Dimension: 8}, &fakeVectorMigrationSource{}, &fakeVectorMigrationTarget{})
	if err != nil {
		t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
	}

	if runner.config.SourceCollection != "items" {
		t.Fatalf("default source collection = %q, want items", runner.config.SourceCollection)
	}
	if runner.config.TargetTable != "items" {
		t.Fatalf("default target table = %q, want items", runner.config.TargetTable)
	}
	if runner.config.BatchSize != 100 {
		t.Fatalf("default batch size = %d, want 100", runner.config.BatchSize)
	}
}

func TestNewVectorMigrationRunnerRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name   string
		config VectorMigrationConfig
		source vectorMigrationSource
		target vectorMigrationTarget
		want   string
	}{
		{name: "missing source", config: VectorMigrationConfig{Dimension: 3}, target: &fakeVectorMigrationTarget{}, want: "source reader is required"},
		{name: "missing target", config: VectorMigrationConfig{Dimension: 3}, source: &fakeVectorMigrationSource{}, want: "target writer is required"},
		{name: "bad dimension", config: VectorMigrationConfig{Dimension: 0}, source: &fakeVectorMigrationSource{}, target: &fakeVectorMigrationTarget{}, want: "dimension must be in range"},
		{name: "bad batch size", config: VectorMigrationConfig{Dimension: 3, BatchSize: -1}, source: &fakeVectorMigrationSource{}, target: &fakeVectorMigrationTarget{}, want: "batch size must be positive"},
		{name: "bad source collection", config: VectorMigrationConfig{Dimension: 3, SourceCollection: "bad-name"}, source: &fakeVectorMigrationSource{}, target: &fakeVectorMigrationTarget{}, want: "invalid vector migration source collection"},
		{name: "bad target table", config: VectorMigrationConfig{Dimension: 3, TargetTable: "bad-name"}, source: &fakeVectorMigrationSource{}, target: &fakeVectorMigrationTarget{}, want: "invalid vector migration target table"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewVectorMigrationRunner(tc.config, tc.source, tc.target)
			if err == nil {
				t.Fatal("expected invalid config to return an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestVectorMigrationRunnerRejectsInvalidSourceRecords(t *testing.T) {
	cases := []struct {
		name    string
		records []VectorMigrationRecord
		want    string
	}{
		{name: "empty id", records: []VectorMigrationRecord{{ID: "", Vector: []float64{1, 2, 3}}}, want: "record at index 0 has empty id"},
		{name: "wrong dimension", records: []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{1, 2}}}, want: "record \"vec-1\" vector dimension 2 does not match migration dimension 3"},
		{name: "non finite", records: []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{1, 2, math.Inf(1)}}}, want: "non-finite"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner, err := NewVectorMigrationRunner(VectorMigrationConfig{Dimension: 3}, &fakeVectorMigrationSource{records: tc.records}, &fakeVectorMigrationTarget{})
			if err != nil {
				t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
			}

			_, err = runner.Migrate(context.Background())
			if err == nil {
				t.Fatal("expected invalid source record to return an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestVectorMigrationRunnerPropagatesReaderAndWriterErrors(t *testing.T) {
	readErr := errors.New("read exploded")
	runner, err := NewVectorMigrationRunner(VectorMigrationConfig{Dimension: 3}, &fakeVectorMigrationSource{err: readErr}, &fakeVectorMigrationTarget{})
	if err != nil {
		t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
	}
	_, err = runner.Migrate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read source records") || !errors.Is(err, readErr) {
		t.Fatalf("read error = %v, want wrapped read exploded", err)
	}

	writeErr := errors.New("write exploded")
	runner, err = NewVectorMigrationRunner(
		VectorMigrationConfig{Dimension: 3},
		&fakeVectorMigrationSource{records: []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{1, 2, 3}}}},
		&fakeVectorMigrationTarget{err: writeErr},
	)
	if err != nil {
		t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
	}
	_, err = runner.Migrate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "write target records") || !errors.Is(err, writeErr) {
		t.Fatalf("write error = %v, want wrapped write exploded", err)
	}
}

func TestVectorMigrationRunnerHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner, err := NewVectorMigrationRunner(VectorMigrationConfig{Dimension: 3}, &fakeVectorMigrationSource{}, &fakeVectorMigrationTarget{})
	if err != nil {
		t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
	}

	_, err = runner.Migrate(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Migrate error = %v, want context.Canceled", err)
	}
}

type fakeVectorMigrationSource struct {
	collection string
	records    []VectorMigrationRecord
	err        error
}

func (s *fakeVectorMigrationSource) ReadRecords(ctx context.Context, collection string) ([]VectorMigrationRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.collection = collection
	if s.err != nil {
		return nil, s.err
	}
	return append([]VectorMigrationRecord(nil), s.records...), nil
}

type fakeVectorMigrationTarget struct {
	table   string
	records []VectorMigrationRecord
	err     error
}

func (t *fakeVectorMigrationTarget) WriteRecords(ctx context.Context, table string, records []VectorMigrationRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.table = table
	if t.err != nil {
		return t.err
	}
	t.records = make([]VectorMigrationRecord, len(records))
	for index, record := range records {
		t.records[index] = VectorMigrationRecord{ID: record.ID, Vector: append([]float64(nil), record.Vector...)}
	}
	return nil
}
