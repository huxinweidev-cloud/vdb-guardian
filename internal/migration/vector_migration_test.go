package migration

import (
	"context"
	"encoding/json"
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
		Checkpoint: &VectorMigrationReportCheckpoint{
			Path:             "/tmp/checkpoint.json",
			ResumeFrom:       "/tmp/checkpoint.json",
			CompletedBatches: 3,
			FailedBatches:    0,
			NextRecordOffset: 300,
		},
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
	if report.Checkpoint == nil || report.Checkpoint.Path != "/tmp/checkpoint.json" || report.Checkpoint.ResumeFrom != "/tmp/checkpoint.json" || report.Checkpoint.CompletedBatches != 3 || report.Checkpoint.NextRecordOffset != 300 {
		t.Fatalf("unexpected checkpoint summary: %+v", report.Checkpoint)
	}
}

func TestBuildVectorMigrationReportIncludesWriteModeMetrics(t *testing.T) {
	report := BuildVectorMigrationReport(VectorMigrationResult{
		WriteModeRequested: "auto",
		WriteModeUsed:      "batch-upsert",
		CopyBatches:        1,
		BatchUpsertBatches: 1,
		CopyFallbacks:      1,
	}, VectorMigrationReportOptions{})

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report map: %v", err)
	}
	summary, ok := decoded["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary missing or wrong type in %s", string(data))
	}

	assertJSONField(t, summary, "write_mode_requested", "auto")
	assertJSONField(t, summary, "write_mode_used", "batch-upsert")
	assertJSONField(t, summary, "copy_batches", float64(1))
	assertJSONField(t, summary, "batch_upsert_batches", float64(1))
	assertJSONField(t, summary, "copy_fallbacks", float64(1))
	assertJSONFieldAbsent(t, decoded, "connection_url")
	assertJSONFieldAbsent(t, decoded, "pgvector_connection_url")
	assertJSONFieldAbsent(t, decoded, "source_connection_url")
	assertJSONFieldAbsent(t, decoded, "target_connection_url")
	if strings.Contains(string(data), "postgres://") || strings.Contains(string(data), "postgresql://") {
		t.Fatalf("report JSON appears to contain a connection URL: %s", string(data))
	}
}

func assertJSONField(t *testing.T, fields map[string]any, name string, want any) {
	t.Helper()
	got, ok := fields[name]
	if !ok {
		t.Fatalf("JSON field %q missing in %#v", name, fields)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON field %q = %#v, want %#v", name, got, want)
	}
}

func assertJSONFieldAbsent(t *testing.T, fields map[string]any, name string) {
	t.Helper()
	if _, ok := fields[name]; ok {
		t.Fatalf("JSON field %q must not be present in %#v", name, fields)
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

func TestVectorMigrationRunnerCopiesFullRecordPayloads(t *testing.T) {
	ctx := context.Background()
	sourceRecord := VectorMigrationRecord{
		ID:              "vec-1",
		Vector:          []float64{0.1, 0.2, 0.3},
		Scalars:         map[string]any{"title": "first", "price": 12.5, "stock": int64(7), "active": true},
		DynamicMetadata: map[string]any{"brand": "acme", "tags": []any{"alpha", "beta"}},
		Partition:       "tenant_a",
	}
	source := &fakeVectorMigrationSource{records: []VectorMigrationRecord{sourceRecord}}
	target := &fakeVectorMigrationTarget{}
	runner, err := NewVectorMigrationRunner(VectorMigrationConfig{SourceCollection: "items", TargetTable: "items_copy", Dimension: 3}, source, target)
	if err != nil {
		t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
	}

	_, err = runner.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if len(target.records) != 1 {
		t.Fatalf("target records length = %d, want 1", len(target.records))
	}
	written := target.records[0]
	if !reflect.DeepEqual(written, sourceRecord) {
		t.Fatalf("written record = %#v, want %#v", written, sourceRecord)
	}

	source.records[0].Vector[0] = 99
	source.records[0].Scalars["title"] = "mutated"
	source.records[0].DynamicMetadata["brand"] = "mutated"
	if written.Vector[0] != 0.1 || written.Scalars["title"] != "first" || written.DynamicMetadata["brand"] != "acme" {
		t.Fatalf("expected target full-record payload to be copied independently, got %#v", written)
	}
}

func TestVectorMigrationRunnerWritesBatchesAndCheckpoints(t *testing.T) {
	source := &fakeVectorMigrationSource{records: []VectorMigrationRecord{
		{ID: "vec-1", Vector: []float64{1, 1, 1}},
		{ID: "vec-2", Vector: []float64{2, 2, 2}},
		{ID: "vec-3", Vector: []float64{3, 3, 3}},
		{ID: "vec-4", Vector: []float64{4, 4, 4}},
		{ID: "vec-5", Vector: []float64{5, 5, 5}},
	}}
	target := &fakeVectorMigrationTarget{}
	store := &fakeVectorMigrationCheckpointStore{}
	runner, err := NewVectorMigrationRunnerWithCheckpointStore(VectorMigrationConfig{SourceCollection: "items", TargetTable: "items_copy", Dimension: 3, BatchSize: 2, CheckpointPath: "/tmp/checkpoint.json", JobID: "job-1"}, source, target, store)
	if err != nil {
		t.Fatalf("NewVectorMigrationRunnerWithCheckpointStore returned error: %v", err)
	}

	result, err := runner.Migrate(context.Background())
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if result.RecordsRead != 5 || result.RecordsWritten != 5 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(target.writeBatches) != 3 {
		t.Fatalf("write batch count = %d, want 3", len(target.writeBatches))
	}
	if len(target.writeBatches[0]) != 2 || len(target.writeBatches[1]) != 2 || len(target.writeBatches[2]) != 1 {
		t.Fatalf("unexpected write batches: %#v", target.writeBatches)
	}
	if len(store.checkpoints) != 4 {
		t.Fatalf("checkpoint count = %d, want 4", len(store.checkpoints))
	}
	last := store.checkpoints[len(store.checkpoints)-1]
	if last.Status != VectorMigrationCheckpointStatusCompleted || last.Resume.NextRecordOffset != 5 || len(last.CompletedBatches) != 3 {
		t.Fatalf("unexpected final checkpoint: %+v", last)
	}
}

func TestVectorMigrationRunnerWritesFailedCheckpointBeforeReturningWriteError(t *testing.T) {
	source := &fakeVectorMigrationSource{records: []VectorMigrationRecord{
		{ID: "vec-1", Vector: []float64{1, 1, 1}},
		{ID: "vec-2", Vector: []float64{2, 2, 2}},
		{ID: "vec-3", Vector: []float64{3, 3, 3}},
	}}
	target := &fakeVectorMigrationTarget{failOnCall: 2}
	store := &fakeVectorMigrationCheckpointStore{}
	runner, err := NewVectorMigrationRunnerWithCheckpointStore(VectorMigrationConfig{SourceCollection: "items", TargetTable: "items_copy", Dimension: 3, BatchSize: 2, CheckpointPath: "/tmp/checkpoint.json"}, source, target, store)
	if err != nil {
		t.Fatalf("NewVectorMigrationRunnerWithCheckpointStore returned error: %v", err)
	}

	_, err = runner.Migrate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "write target records") {
		t.Fatalf("expected write target records error, got %v", err)
	}
	if len(store.checkpoints) != 2 {
		t.Fatalf("checkpoint count = %d, want 2", len(store.checkpoints))
	}
	failed := store.checkpoints[len(store.checkpoints)-1]
	if failed.Status != VectorMigrationCheckpointStatusFailed || len(failed.CompletedBatches) != 1 || len(failed.FailedBatches) != 1 {
		t.Fatalf("unexpected failed checkpoint: %+v", failed)
	}
	if failed.Resume.NextRecordOffset != 2 || failed.FailedBatches[0].Start != 2 || failed.FailedBatches[0].End != 3 {
		t.Fatalf("unexpected failed checkpoint offsets: %+v", failed)
	}
}

func TestVectorMigrationRunnerAggregatesWriteStats(t *testing.T) {
	source := &fakeVectorMigrationSource{records: []VectorMigrationRecord{
		{ID: "vec-1", Vector: []float64{1, 1, 1}},
		{ID: "vec-2", Vector: []float64{2, 2, 2}},
		{ID: "vec-3", Vector: []float64{3, 3, 3}},
		{ID: "vec-4", Vector: []float64{4, 4, 4}},
	}}
	target := &fakeStatsMigrationTarget{
		results: []VectorMigrationWriteResult{
			{WriteModeUsed: "copy", CopyBatches: 1},
			{WriteModeUsed: "batch-upsert", BatchUpsertBatches: 1, CopyFallbacks: 1},
		},
	}
	runner, err := NewVectorMigrationRunner(VectorMigrationConfig{SourceCollection: "items", TargetTable: "items_copy", Dimension: 3, BatchSize: 2}, source, target)
	if err != nil {
		t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
	}

	result, err := runner.Migrate(context.Background())
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	if result.WriteModeUsed != "mixed" {
		t.Fatalf("write mode used = %q, want mixed", result.WriteModeUsed)
	}
	if result.CopyBatches != 1 || result.BatchUpsertBatches != 1 || result.CopyFallbacks != 1 {
		t.Fatalf("write stats = copy %d batch-upsert %d fallbacks %d, want 1/1/1", result.CopyBatches, result.BatchUpsertBatches, result.CopyFallbacks)
	}
}

func TestVectorMigrationWriteResultAddPreservesSingleAndEmptyWriteModes(t *testing.T) {
	cases := []struct {
		name    string
		batches []VectorMigrationWriteResult
		want    string
	}{
		{name: "empty", want: ""},
		{name: "copy only", batches: []VectorMigrationWriteResult{{WriteModeUsed: "copy", CopyBatches: 1}, {WriteModeUsed: "copy", CopyBatches: 1}}, want: "copy"},
		{name: "batch upsert only", batches: []VectorMigrationWriteResult{{WriteModeUsed: "batch-upsert", BatchUpsertBatches: 1}, {WriteModeUsed: "batch-upsert", BatchUpsertBatches: 1}}, want: "batch-upsert"},
		{name: "mixed by counters", batches: []VectorMigrationWriteResult{{WriteModeUsed: "copy", CopyBatches: 1}, {WriteModeUsed: "batch-upsert", BatchUpsertBatches: 1}}, want: "mixed"},
		{name: "mixed by reported modes", batches: []VectorMigrationWriteResult{{WriteModeUsed: "copy"}, {WriteModeUsed: "batch-upsert"}}, want: "mixed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stats VectorMigrationWriteResult
			for _, batch := range tc.batches {
				stats.add(batch)
			}
			if stats.WriteModeUsed != tc.want {
				t.Fatalf("write mode used = %q, want %q", stats.WriteModeUsed, tc.want)
			}
		})
	}
}

func TestVectorMigrationRunnerResumeSkipsCompletedRecords(t *testing.T) {
	source := &fakeVectorMigrationSource{records: []VectorMigrationRecord{
		{ID: "vec-1", Vector: []float64{1, 1, 1}},
		{ID: "vec-2", Vector: []float64{2, 2, 2}},
		{ID: "vec-3", Vector: []float64{3, 3, 3}},
		{ID: "vec-4", Vector: []float64{4, 4, 4}},
		{ID: "vec-5", Vector: []float64{5, 5, 5}},
	}}
	target := &fakeVectorMigrationTarget{}
	store := &fakeVectorMigrationCheckpointStore{}
	checkpoint := BuildVectorMigrationCheckpoint(VectorMigrationCheckpointInput{Status: VectorMigrationCheckpointStatusFailed, SourceCollection: "items", TargetTable: "items_copy", Dimension: 3, BatchSize: 2, RecordsRead: 5, RecordsWritten: 2, CompletedBatches: []VectorMigrationCheckpointBatch{{Index: 0, Start: 0, End: 2, RecordsWritten: 2}}, Resume: VectorMigrationCheckpointResume{NextBatchIndex: 1, NextRecordOffset: 2}})
	runner, err := NewVectorMigrationRunnerWithCheckpointStore(VectorMigrationConfig{SourceCollection: "items", TargetTable: "items_copy", Dimension: 3, BatchSize: 2, ResumeCheckpoint: &checkpoint, CheckpointPath: "/tmp/checkpoint.json"}, source, target, store)
	if err != nil {
		t.Fatalf("NewVectorMigrationRunnerWithCheckpointStore returned error: %v", err)
	}

	result, err := runner.Migrate(context.Background())
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if result.RecordsRead != 5 || result.RecordsWritten != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(target.records) != 3 || target.records[0].ID != "vec-3" || target.records[2].ID != "vec-5" {
		t.Fatalf("resume wrote unexpected records: %#v", target.records)
	}
	last := store.checkpoints[len(store.checkpoints)-1]
	if last.Resume.NextRecordOffset != 5 || len(last.CompletedBatches) != 3 {
		t.Fatalf("unexpected resumed checkpoint: %+v", last)
	}
}

func TestVectorMigrationRunnerRejectsNonFiniteScalarFloat(t *testing.T) {
	target := &fakeVectorMigrationTarget{}
	runner, err := NewVectorMigrationRunner(VectorMigrationConfig{Dimension: 3}, &fakeVectorMigrationSource{records: []VectorMigrationRecord{{ID: "vec-1", Vector: []float64{1, 2, 3}, Scalars: map[string]any{"score": math.NaN()}}}}, target)
	if err != nil {
		t.Fatalf("NewVectorMigrationRunner returned error: %v", err)
	}

	_, err = runner.Migrate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "scalar \"score\" contains non-finite value") {
		t.Fatalf("expected non-finite scalar validation error, got %v", err)
	}
	if len(target.records) != 0 {
		t.Fatalf("target should not receive invalid records: %#v", target.records)
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
	table        string
	records      []VectorMigrationRecord
	writeBatches [][]VectorMigrationRecord
	err          error
	failOnCall   int
	calls        int
}

func (t *fakeVectorMigrationTarget) WriteRecords(ctx context.Context, table string, records []VectorMigrationRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.table = table
	t.calls++
	if t.err != nil {
		return t.err
	}
	if t.failOnCall > 0 && t.calls == t.failOnCall {
		return errors.New("write batch exploded")
	}
	copied := copyVectorMigrationRecords(records)
	t.writeBatches = append(t.writeBatches, copied)
	t.records = append(t.records, copied...)
	return nil
}

type fakeStatsMigrationTarget struct {
	table        string
	records      []VectorMigrationRecord
	writeBatches [][]VectorMigrationRecord
	results      []VectorMigrationWriteResult
	calls        int
}

func (t *fakeStatsMigrationTarget) WriteRecords(ctx context.Context, table string, records []VectorMigrationRecord) error {
	_, err := t.WriteRecordsWithResult(ctx, table, records)
	return err
}

func (t *fakeStatsMigrationTarget) WriteRecordsWithResult(ctx context.Context, table string, records []VectorMigrationRecord) (VectorMigrationWriteResult, error) {
	if err := ctx.Err(); err != nil {
		return VectorMigrationWriteResult{}, err
	}
	t.table = table
	if t.calls >= len(t.results) {
		return VectorMigrationWriteResult{}, errors.New("missing fake write result")
	}
	result := t.results[t.calls]
	t.calls++
	copied := copyVectorMigrationRecords(records)
	t.writeBatches = append(t.writeBatches, copied)
	t.records = append(t.records, copied...)
	return result, nil
}

type fakeVectorMigrationCheckpointStore struct {
	checkpoints []VectorMigrationCheckpoint
	err         error
}

func (s *fakeVectorMigrationCheckpointStore) Save(ctx context.Context, checkpoint VectorMigrationCheckpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.err != nil {
		return s.err
	}
	s.checkpoints = append(s.checkpoints, checkpoint)
	return nil
}
