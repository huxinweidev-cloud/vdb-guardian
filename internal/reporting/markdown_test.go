package reporting

import (
	"strings"
	"testing"

	"github.com/h3xwave/vdb-guardian/internal/engine"
	"github.com/h3xwave/vdb-guardian/internal/jobs"
	"github.com/h3xwave/vdb-guardian/internal/migration"
)

func TestRenderMigrateAndVerifyMarkdownIncludesSummaryMetricsAndArtifacts(t *testing.T) {
	report, err := RenderMigrateAndVerifyMarkdown(MigrateAndVerifyReport{
		JobID:                 "mv-smoke",
		State:                 jobs.StateSucceeded,
		SourceFingerprintPath: "/tmp/run/mv-smoke-source-fingerprint.json",
		TargetFingerprintPath: "/tmp/run/mv-smoke-target-fingerprint.json",
		ResultPath:            "/tmp/run/mv-smoke-result.json",
		ResetTarget:           true,
		StrictCount:           true,
		Migration: migration.VectorMigrationResult{
			SourceCollection: "items",
			TargetTable:      "items",
			Dimension:        8,
			RecordsRead:      100,
			RecordsWritten:   100,
		},
		Output: engine.CompareOutput{
			ConsistencyScore: 1,
			Metrics: engine.MetricSummary{
				FingerprintDistance:       0,
				StableNeighborDistance:    0.1,
				BoundaryCandidateDistance: 0.2,
				BoundaryFlipRate:          0.3,
				MatchedQueryCount:         10,
				MissingSourceQueryCount:   1,
				MissingTargetQueryCount:   2,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderMigrateAndVerifyMarkdown returned error: %v", err)
	}
	for _, want := range []string{
		"# vdb-guardian migrate-and-verify report",
		"- Job ID: `mv-smoke`",
		"- State: `SUCCEEDED`",
		"- Source collection: `items`",
		"- Target table: `items`",
		"- Dimension: `8`",
		"- Records read: `100`",
		"- Records written: `100`",
		"- Reset target: `yes`",
		"- Strict count: `yes`",
		"| Consistency score | 1.000000 |",
		"| Fingerprint distance | 0.000000 |",
		"| Stable neighbor distance | 0.100000 |",
		"| Boundary candidate distance | 0.200000 |",
		"| Boundary flip rate | 0.300000 |",
		"| Matched queries | 10 |",
		"| Missing source queries | 1 |",
		"| Missing target queries | 2 |",
		"- Source fingerprint: `/tmp/run/mv-smoke-source-fingerprint.json`",
		"- Target fingerprint: `/tmp/run/mv-smoke-target-fingerprint.json`",
		"- Result JSON: `/tmp/run/mv-smoke-result.json`",
		"This run used `--reset-target`",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected report to contain %q, got:\n%s", want, report)
		}
	}
}

func TestRenderMigrateAndVerifyMarkdownRejectsMissingJobID(t *testing.T) {
	_, err := RenderMigrateAndVerifyMarkdown(MigrateAndVerifyReport{})
	if err == nil || !strings.Contains(err.Error(), "job id") {
		t.Fatalf("expected job id validation error, got %v", err)
	}
}
