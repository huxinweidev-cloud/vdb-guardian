package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

type planPGVectorSchemaOptions struct {
	InspectionPlanPath string
	OutputPath         string
	TargetSchema       string
}

// runPlanPGVectorSchemaCommand generates a dry-run pgvector schema plan from a
// previously emitted Milvus inspection plan without connecting to PostgreSQL.
func runPlanPGVectorSchemaCommand(args []string) error {
	return runPlanPGVectorSchemaCommandWithWriters(args, os.Stdout)
}

func runPlanPGVectorSchemaCommandWithWriters(args []string, stdout io.Writer) error {
	options, err := parsePlanPGVectorSchemaOptions(args)
	if err != nil {
		return err
	}
	inspectionPlan, err := readMilvusInspectionPlan(options.InspectionPlanPath)
	if err != nil {
		return err
	}
	schemaPlan, err := planschema.BuildPGVectorSchemaPlan(inspectionPlan, planschema.PGVectorSchemaPlannerOptions{TargetSchema: options.TargetSchema, SourcePlan: options.InspectionPlanPath})
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(schemaPlan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pgvector schema plan: %w", err)
	}
	data = append(data, '\n')
	if options.OutputPath == "" {
		_, err = stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create plan-pgvector-schema output dir: %w", err)
	}
	if err := os.WriteFile(options.OutputPath, data, 0o600); err != nil {
		return fmt.Errorf("write plan-pgvector-schema output %q: %w", options.OutputPath, err)
	}
	fmt.Fprintf(stdout, "schema plan completed\n")
	fmt.Fprintf(stdout, "output: %s\n", options.OutputPath)
	fmt.Fprintf(stdout, "tables: %d\n", schemaPlan.Summary.TableCount)
	fmt.Fprintf(stdout, "warnings: %d\n", schemaPlan.Summary.WarningCount)
	fmt.Fprintf(stdout, "unsupported_features: %d\n", schemaPlan.Summary.UnsupportedFeatureCount)
	return nil
}

func parsePlanPGVectorSchemaOptions(args []string) (planPGVectorSchemaOptions, error) {
	options := planPGVectorSchemaOptions{TargetSchema: "public"}
	flags := flag.NewFlagSet("plan-pgvector-schema", flag.ContinueOnError)
	flags.StringVar(&options.InspectionPlanPath, "inspection-plan", "", "Path to a Milvus inspection JSON plan")
	flags.StringVar(&options.OutputPath, "output", "", "Optional output path for the pgvector schema plan JSON")
	flags.StringVar(&options.TargetSchema, "target-schema", "public", "PostgreSQL schema name for generated DDL")
	if err := flags.Parse(args); err != nil {
		return planPGVectorSchemaOptions{}, err
	}
	if options.InspectionPlanPath == "" {
		return planPGVectorSchemaOptions{}, fmt.Errorf("inspection-plan is required")
	}
	return options, nil
}

func readMilvusInspectionPlan(path string) (inspection.MilvusInspectionPlan, error) {
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
