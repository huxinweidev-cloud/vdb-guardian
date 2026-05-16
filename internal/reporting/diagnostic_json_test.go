package reporting

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/engine"
	"github.com/h3xwave/vdb-guardian/internal/jobs"
	"github.com/h3xwave/vdb-guardian/internal/migration"
)

func TestRenderMigrateAndVerifyDiagnosticJSONIncludesMachineReadableRunContext(t *testing.T) {
	data, err := RenderMigrateAndVerifyDiagnosticJSON(MigrateAndVerifyDiagnosticReport{
		MigrateAndVerifyReport: MigrateAndVerifyReport{
			JobID:                    "mv-smoke",
			State:                    jobs.StateSucceeded,
			SourceFingerprintPath:    "/tmp/run/mv-smoke-source-fingerprint.json",
			TargetFingerprintPath:    "/tmp/run/mv-smoke-target-fingerprint.json",
			ResultPath:               "/tmp/run/mv-smoke-result.json",
			ResetTarget:              true,
			StrictCount:              true,
			FullRecordCompareEnabled: true,
			SourceFullRecordPath:     "/tmp/run/mv-smoke-source-full-records.json",
			TargetFullRecordPath:     "/tmp/run/mv-smoke-target-full-records.json",
			FullRecordComparePath:    "/tmp/run/mv-smoke-full-record-compare.json",
			CheckpointPath:           "/tmp/run/mv-smoke-checkpoint.json",
			ResumeFromPath:           "/tmp/run/mv-smoke-checkpoint.json",
			Migration: migration.VectorMigrationResult{
				SourceCollection: "items",
				TargetTable:      "items",
				Dimension:        8,
				RecordsRead:      100,
				RecordsWritten:   100,
			},
			Output: engine.CompareOutput{
				ConsistencyScore: 0.9995,
				Metrics: engine.MetricSummary{
					FingerprintDistance:       0.0005,
					StableNeighborDistance:    0.1,
					BoundaryCandidateDistance: 0.2,
					BoundaryFlipRate:          0.3,
					MatchedQueryCount:         10,
					MissingSourceQueryCount:   1,
					MissingTargetQueryCount:   2,
				},
			},
		},
		MinConsistencyScore:    0.999,
		MaxFingerprintDistance: 0.001,
	})
	if err != nil {
		t.Fatalf("RenderMigrateAndVerifyDiagnosticJSON returned error: %v", err)
	}

	var got struct {
		SchemaVersion string `json:"schema_version"`
		JobID         string `json:"job_id"`
		State         string `json:"state"`
		Migration     struct {
			SourceCollection string `json:"source_collection"`
			TargetTable      string `json:"target_table"`
			Dimension        int    `json:"dimension"`
			RecordsRead      int    `json:"records_read"`
			RecordsWritten   int    `json:"records_written"`
		} `json:"migration"`
		Verification struct {
			ConsistencyScore float64 `json:"consistency_score"`
			Metrics          struct {
				FingerprintDistance       float64 `json:"fingerprint_distance"`
				StableNeighborDistance    float64 `json:"stable_neighbor_distance"`
				BoundaryCandidateDistance float64 `json:"boundary_candidate_distance"`
				BoundaryFlipRate          float64 `json:"boundary_flip_rate"`
				MatchedQueryCount         int     `json:"matched_query_count"`
				MissingSourceQueryCount   int     `json:"missing_source_query_count"`
				MissingTargetQueryCount   int     `json:"missing_target_query_count"`
			} `json:"metrics"`
		} `json:"verification"`
		Artifacts struct {
			SourceFingerprint string `json:"source_fingerprint"`
			TargetFingerprint string `json:"target_fingerprint"`
			ResultJSON        string `json:"result_json"`
		} `json:"artifacts"`
		Safety struct {
			ResetTarget bool `json:"reset_target"`
			StrictCount bool `json:"strict_count"`
		} `json:"safety"`
		FullRecordEquality struct {
			Enabled        bool   `json:"enabled"`
			SourceArtifact string `json:"source_artifact"`
			TargetArtifact string `json:"target_artifact"`
			CompareReport  string `json:"compare_report"`
		} `json:"full_record_equality"`
		Checkpoint struct {
			Path       string `json:"path"`
			ResumeFrom string `json:"resume_from"`
		} `json:"checkpoint"`
		QualityGates struct {
			MinConsistencyScore    float64 `json:"min_consistency_score"`
			MaxFingerprintDistance float64 `json:"max_fingerprint_distance"`
			Passed                 bool    `json:"passed"`
		} `json:"quality_gates"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("diagnostic JSON is not valid JSON: %v\n%s", err, string(data))
	}
	if got.SchemaVersion != "v1" || got.JobID != "mv-smoke" || got.State != "SUCCEEDED" {
		t.Fatalf("unexpected identity fields: %+v", got)
	}
	if got.Migration.SourceCollection != "items" || got.Migration.TargetTable != "items" || got.Migration.Dimension != 8 {
		t.Fatalf("unexpected migration fields: %+v", got.Migration)
	}
	if got.Migration.RecordsRead != 100 || got.Migration.RecordsWritten != 100 {
		t.Fatalf("unexpected migration counts: %+v", got.Migration)
	}
	if got.Verification.ConsistencyScore != 0.9995 || got.Verification.Metrics.FingerprintDistance != 0.0005 {
		t.Fatalf("unexpected verification metrics: %+v", got.Verification)
	}
	if got.Verification.Metrics.MatchedQueryCount != 10 || got.Verification.Metrics.MissingSourceQueryCount != 1 || got.Verification.Metrics.MissingTargetQueryCount != 2 {
		t.Fatalf("unexpected query counts: %+v", got.Verification.Metrics)
	}
	if got.Artifacts.SourceFingerprint != "/tmp/run/mv-smoke-source-fingerprint.json" || got.Artifacts.TargetFingerprint != "/tmp/run/mv-smoke-target-fingerprint.json" || got.Artifacts.ResultJSON != "/tmp/run/mv-smoke-result.json" {
		t.Fatalf("unexpected artifact paths: %+v", got.Artifacts)
	}
	if !got.Safety.ResetTarget || !got.Safety.StrictCount {
		t.Fatalf("unexpected safety flags: %+v", got.Safety)
	}
	if !got.FullRecordEquality.Enabled || got.FullRecordEquality.SourceArtifact != "/tmp/run/mv-smoke-source-full-records.json" || got.FullRecordEquality.TargetArtifact != "/tmp/run/mv-smoke-target-full-records.json" || got.FullRecordEquality.CompareReport != "/tmp/run/mv-smoke-full-record-compare.json" {
		t.Fatalf("unexpected full-record equality fields: %+v", got.FullRecordEquality)
	}
	if got.Checkpoint.Path != "/tmp/run/mv-smoke-checkpoint.json" || got.Checkpoint.ResumeFrom != "/tmp/run/mv-smoke-checkpoint.json" {
		t.Fatalf("unexpected checkpoint fields: %+v", got.Checkpoint)
	}
	if got.QualityGates.MinConsistencyScore != 0.999 || got.QualityGates.MaxFingerprintDistance != 0.001 || !got.QualityGates.Passed {
		t.Fatalf("unexpected quality gates: %+v", got.QualityGates)
	}
}

func TestRenderMigrateAndVerifyDiagnosticJSONRejectsMissingArtifacts(t *testing.T) {
	_, err := RenderMigrateAndVerifyDiagnosticJSON(MigrateAndVerifyDiagnosticReport{
		MigrateAndVerifyReport: MigrateAndVerifyReport{JobID: "mv-smoke"},
	})
	if err == nil || !strings.Contains(err.Error(), "artifact paths") {
		t.Fatalf("expected artifact path validation error, got %v", err)
	}
}
