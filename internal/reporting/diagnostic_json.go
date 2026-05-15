package reporting

import (
	"encoding/json"
	"fmt"
)

// MigrateAndVerifyDiagnosticReport contains the human-readable report inputs plus
// quality gate thresholds needed to render a machine-readable diagnostic JSON
// artifact for CI, APIs, and automated migration smoke checks.
//
// MigrateAndVerifyDiagnosticReport 包含生成机器可读诊断 JSON 所需的迁移、验证、产物路径和质量门禁阈值。
// 该结构体用于让 CI、API 和自动化 smoke check 在不解析 Markdown 的情况下消费迁移一致性结果。
type MigrateAndVerifyDiagnosticReport struct {
	MigrateAndVerifyReport
	// MinConsistencyScore is the configured lower bound for passing consistency score.
	MinConsistencyScore float64
	// MaxFingerprintDistance is the configured upper bound for passing fingerprint distance.
	MaxFingerprintDistance float64
}

type migrateAndVerifyDiagnosticJSON struct {
	SchemaVersion string                              `json:"schema_version"`
	JobID         string                              `json:"job_id"`
	State         string                              `json:"state"`
	Migration     migrateAndVerifyDiagnosticMigration `json:"migration"`
	Verification  migrateAndVerifyDiagnosticVerify    `json:"verification"`
	Artifacts     migrateAndVerifyDiagnosticArtifacts `json:"artifacts"`
	Safety        migrateAndVerifyDiagnosticSafety    `json:"safety"`
	QualityGates  migrateAndVerifyDiagnosticGates     `json:"quality_gates"`
}

type migrateAndVerifyDiagnosticMigration struct {
	SourceCollection string `json:"source_collection"`
	TargetTable      string `json:"target_table"`
	Dimension        int    `json:"dimension"`
	RecordsRead      int    `json:"records_read"`
	RecordsWritten   int    `json:"records_written"`
}

type migrateAndVerifyDiagnosticVerify struct {
	ConsistencyScore float64                           `json:"consistency_score"`
	Metrics          migrateAndVerifyDiagnosticMetrics `json:"metrics"`
}

type migrateAndVerifyDiagnosticMetrics struct {
	FingerprintDistance       float64 `json:"fingerprint_distance"`
	StableNeighborDistance    float64 `json:"stable_neighbor_distance"`
	BoundaryCandidateDistance float64 `json:"boundary_candidate_distance"`
	BoundaryFlipRate          float64 `json:"boundary_flip_rate"`
	MatchedQueryCount         int     `json:"matched_query_count"`
	MissingSourceQueryCount   int     `json:"missing_source_query_count"`
	MissingTargetQueryCount   int     `json:"missing_target_query_count"`
}

type migrateAndVerifyDiagnosticArtifacts struct {
	SourceFingerprint string `json:"source_fingerprint"`
	TargetFingerprint string `json:"target_fingerprint"`
	ResultJSON        string `json:"result_json"`
}

type migrateAndVerifyDiagnosticSafety struct {
	ResetTarget bool `json:"reset_target"`
	StrictCount bool `json:"strict_count"`
}

type migrateAndVerifyDiagnosticGates struct {
	MinConsistencyScore    float64 `json:"min_consistency_score"`
	MaxFingerprintDistance float64 `json:"max_fingerprint_distance"`
	Passed                 bool    `json:"passed"`
}

// RenderMigrateAndVerifyDiagnosticJSON renders a stable, machine-readable JSON
// diagnostic artifact for a completed migrate-and-verify run. It intentionally
// complements the compact engine result artifact and Markdown report instead of
// replacing either one.
//
// RenderMigrateAndVerifyDiagnosticJSON 会为 migrate-and-verify 执行生成稳定的机器可读 JSON 诊断产物。
// 它用于补充已有的轻量 result JSON 和 Markdown 报告，而不是替代二者。
func RenderMigrateAndVerifyDiagnosticJSON(report MigrateAndVerifyDiagnosticReport) ([]byte, error) {
	if report.JobID == "" {
		return nil, fmt.Errorf("migrate-and-verify diagnostic report job id must not be empty")
	}
	if report.SourceFingerprintPath == "" || report.TargetFingerprintPath == "" || report.ResultPath == "" {
		return nil, fmt.Errorf("migrate-and-verify diagnostic report artifact paths must not be empty")
	}

	metrics := report.Output.Metrics
	payload := migrateAndVerifyDiagnosticJSON{
		SchemaVersion: "v1",
		JobID:         report.JobID,
		State:         report.State.String(),
		Migration: migrateAndVerifyDiagnosticMigration{
			SourceCollection: report.Migration.SourceCollection,
			TargetTable:      report.Migration.TargetTable,
			Dimension:        report.Migration.Dimension,
			RecordsRead:      report.Migration.RecordsRead,
			RecordsWritten:   report.Migration.RecordsWritten,
		},
		Verification: migrateAndVerifyDiagnosticVerify{
			ConsistencyScore: report.Output.ConsistencyScore,
			Metrics: migrateAndVerifyDiagnosticMetrics{
				FingerprintDistance:       metrics.FingerprintDistance,
				StableNeighborDistance:    metrics.StableNeighborDistance,
				BoundaryCandidateDistance: metrics.BoundaryCandidateDistance,
				BoundaryFlipRate:          metrics.BoundaryFlipRate,
				MatchedQueryCount:         metrics.MatchedQueryCount,
				MissingSourceQueryCount:   metrics.MissingSourceQueryCount,
				MissingTargetQueryCount:   metrics.MissingTargetQueryCount,
			},
		},
		Artifacts: migrateAndVerifyDiagnosticArtifacts{
			SourceFingerprint: report.SourceFingerprintPath,
			TargetFingerprint: report.TargetFingerprintPath,
			ResultJSON:        report.ResultPath,
		},
		Safety: migrateAndVerifyDiagnosticSafety{
			ResetTarget: report.ResetTarget,
			StrictCount: report.StrictCount,
		},
		QualityGates: migrateAndVerifyDiagnosticGates{
			MinConsistencyScore:    report.MinConsistencyScore,
			MaxFingerprintDistance: report.MaxFingerprintDistance,
			Passed:                 report.Output.ConsistencyScore >= report.MinConsistencyScore && metrics.FingerprintDistance <= report.MaxFingerprintDistance,
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal migrate-and-verify diagnostic report: %w", err)
	}
	return data, nil
}
