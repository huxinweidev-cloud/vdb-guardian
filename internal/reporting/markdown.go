package reporting

import (
	"errors"
	"fmt"
	"strings"

	"github.com/h3xwave/vdb-guardian/internal/engine"
	"github.com/h3xwave/vdb-guardian/internal/jobs"
	"github.com/h3xwave/vdb-guardian/internal/migration"
)

// MigrateAndVerifyReport contains the completed migration, verification, and
// artifact metadata required to render a durable human-readable report for one
// migrate-and-verify run.
//
// MigrateAndVerifyReport 包含一次 migrate-and-verify 执行完成后的迁移结果、验证结果与产物路径，
// 用于生成可归档、可人工审阅的 Markdown 报告。
type MigrateAndVerifyReport struct {
	// JobID identifies the migrate-and-verify run and ties the report to artifacts.
	JobID string
	// State is the final verification lifecycle state.
	State jobs.State
	// Migration summarizes the source-to-target vector record transfer.
	Migration migration.VectorMigrationResult
	// Output contains the fingerprint comparison metrics returned by the engine.
	Output engine.CompareOutput
	// SourceFingerprintPath points to the source fingerprint artifact JSON.
	SourceFingerprintPath string
	// TargetFingerprintPath points to the target fingerprint artifact JSON.
	TargetFingerprintPath string
	// ResultPath points to the machine-readable comparison result JSON.
	ResultPath string
	// ResetTarget reports whether destructive target cleanup was enabled before migration.
	ResetTarget bool
	// StrictCount reports whether target row count validation was enabled after migration.
	StrictCount bool
}

// RenderMigrateAndVerifyMarkdown renders a deterministic Markdown report for a
// completed migrate-and-verify run. It validates required identity and artifact
// fields before rendering so callers do not persist ambiguous reports.
//
// RenderMigrateAndVerifyMarkdown 会为一次已完成的 migrate-and-verify 执行渲染确定性的 Markdown 报告。
// 它会先校验必要的作业标识与产物路径，避免调用方持久化缺乏上下文的模糊报告。
func RenderMigrateAndVerifyMarkdown(report MigrateAndVerifyReport) (string, error) {
	if report.JobID == "" {
		return "", errors.New("migrate-and-verify report job id must not be empty")
	}
	if report.SourceFingerprintPath == "" || report.TargetFingerprintPath == "" || report.ResultPath == "" {
		return "", errors.New("migrate-and-verify report artifact paths must not be empty")
	}

	resetTarget := "no"
	if report.ResetTarget {
		resetTarget = "yes"
	}
	strictCount := "no"
	if report.StrictCount {
		strictCount = "yes"
	}

	var builder strings.Builder
	builder.WriteString("# vdb-guardian migrate-and-verify report\n\n")
	builder.WriteString("## Summary\n\n")
	fmt.Fprintf(&builder, "- Job ID: `%s`\n", report.JobID)
	fmt.Fprintf(&builder, "- State: `%s`\n", report.State.String())
	fmt.Fprintf(&builder, "- Source collection: `%s`\n", report.Migration.SourceCollection)
	fmt.Fprintf(&builder, "- Target table: `%s`\n", report.Migration.TargetTable)
	fmt.Fprintf(&builder, "- Dimension: `%d`\n", report.Migration.Dimension)
	fmt.Fprintf(&builder, "- Records read: `%d`\n", report.Migration.RecordsRead)
	fmt.Fprintf(&builder, "- Records written: `%d`\n", report.Migration.RecordsWritten)
	fmt.Fprintf(&builder, "- Reset target: `%s`\n", resetTarget)
	fmt.Fprintf(&builder, "- Strict count: `%s`\n\n", strictCount)

	builder.WriteString("## Metrics\n\n")
	builder.WriteString("| Metric | Value |\n")
	builder.WriteString("|---|---:|\n")
	fmt.Fprintf(&builder, "| Consistency score | %.6f |\n", report.Output.ConsistencyScore)
	fmt.Fprintf(&builder, "| Fingerprint distance | %.6f |\n", report.Output.Metrics.FingerprintDistance)
	fmt.Fprintf(&builder, "| Stable neighbor distance | %.6f |\n", report.Output.Metrics.StableNeighborDistance)
	fmt.Fprintf(&builder, "| Boundary candidate distance | %.6f |\n", report.Output.Metrics.BoundaryCandidateDistance)
	fmt.Fprintf(&builder, "| Boundary flip rate | %.6f |\n", report.Output.Metrics.BoundaryFlipRate)
	fmt.Fprintf(&builder, "| Matched queries | %d |\n", report.Output.Metrics.MatchedQueryCount)
	fmt.Fprintf(&builder, "| Missing source queries | %d |\n", report.Output.Metrics.MissingSourceQueryCount)
	fmt.Fprintf(&builder, "| Missing target queries | %d |\n\n", report.Output.Metrics.MissingTargetQueryCount)

	builder.WriteString("## Artifacts\n\n")
	fmt.Fprintf(&builder, "- Source fingerprint: `%s`\n", report.SourceFingerprintPath)
	fmt.Fprintf(&builder, "- Target fingerprint: `%s`\n", report.TargetFingerprintPath)
	fmt.Fprintf(&builder, "- Result JSON: `%s`\n\n", report.ResultPath)

	builder.WriteString("## Safety notes\n\n")
	if report.ResetTarget {
		builder.WriteString("This run used `--reset-target`, so the pgvector target table was truncated before migration. Only use this mode for disposable smoke runs or explicitly approved destructive cleanup.\n")
	} else {
		builder.WriteString("This run did not use `--reset-target`; pre-existing stale target rows may still require separate production cleanup or checkpoint semantics.\n")
	}
	return builder.String(), nil
}
