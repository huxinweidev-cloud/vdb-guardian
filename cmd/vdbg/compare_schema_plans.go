package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

type compareSchemaPlansOptions struct {
	InspectionPlanPath string
	SchemaPlanPath     string
	OutputPath         string
}

// runCompareSchemaPlansCommand compares a Milvus inspection plan with a
// generated pgvector schema plan and writes a deterministic JSON report.
func runCompareSchemaPlansCommand(args []string, stdout io.Writer) error {
	options, err := parseCompareSchemaPlansOptions(args)
	if err != nil {
		return err
	}
	inspectionPlan, err := loadCompareInspectionPlan(options.InspectionPlanPath)
	if err != nil {
		return err
	}
	schemaPlan, err := loadComparePGVectorSchemaPlan(options.SchemaPlanPath)
	if err != nil {
		return err
	}
	report, err := planschema.CompareSchemaPlans(inspectionPlan, schemaPlan, planschema.PlanCompareOptions{InspectionPlanPath: options.InspectionPlanPath, SchemaPlanPath: options.SchemaPlanPath})
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schema comparison report: %w", err)
	}
	data = append(data, '\n')
	if options.OutputPath == "" {
		_, err = stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create compare-schema-plans output dir: %w", err)
	}
	if err := os.WriteFile(options.OutputPath, data, 0o600); err != nil {
		return fmt.Errorf("write compare-schema-plans output %q: %w", options.OutputPath, err)
	}
	fmt.Fprintf(stdout, "schema comparison completed\n")
	fmt.Fprintf(stdout, "output: %s\n", options.OutputPath)
	fmt.Fprintf(stdout, "status: %s\n", report.Status)
	fmt.Fprintf(stdout, "mismatches: %d\n", report.Summary.MismatchCount)
	fmt.Fprintf(stdout, "warnings: %d\n", report.Summary.WarningCount)
	fmt.Fprintf(stdout, "unsupported_features: %d\n", report.Summary.UnsupportedFeatureCount)
	if report.Status == planschema.SchemaPlanCompareStatusFail {
		return fmt.Errorf("schema comparison failed with %d mismatch(es)", report.Summary.MismatchCount)
	}
	return nil
}

func parseCompareSchemaPlansOptions(args []string) (compareSchemaPlansOptions, error) {
	options := compareSchemaPlansOptions{}
	flags := flag.NewFlagSet("compare-schema-plans", flag.ContinueOnError)
	flags.StringVar(&options.InspectionPlanPath, "inspection-plan", "", "Path to a Milvus inspection JSON plan")
	flags.StringVar(&options.SchemaPlanPath, "schema-plan", "", "Path to a pgvector schema JSON plan")
	flags.StringVar(&options.OutputPath, "output", "", "Optional output path for schema comparison report JSON")
	if err := flags.Parse(args); err != nil {
		return compareSchemaPlansOptions{}, err
	}
	if options.InspectionPlanPath == "" {
		return compareSchemaPlansOptions{}, errors.New("inspection-plan is required")
	}
	if options.SchemaPlanPath == "" {
		return compareSchemaPlansOptions{}, errors.New("schema-plan is required")
	}
	return options, nil
}

func loadCompareInspectionPlan(path string) (inspection.MilvusInspectionPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return inspection.MilvusInspectionPlan{}, fmt.Errorf("read inspection plan %q: %w", path, err)
	}
	var plan inspection.MilvusInspectionPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return inspection.MilvusInspectionPlan{}, fmt.Errorf("parse inspection plan %q: %w", path, err)
	}
	return plan, nil
}

func loadComparePGVectorSchemaPlan(path string) (planschema.PGVectorSchemaPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return planschema.PGVectorSchemaPlan{}, fmt.Errorf("read pgvector schema plan %q: %w", path, err)
	}
	var plan planschema.PGVectorSchemaPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return planschema.PGVectorSchemaPlan{}, fmt.Errorf("parse pgvector schema plan %q: %w", path, err)
	}
	return plan, nil
}
