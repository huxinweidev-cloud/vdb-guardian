package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/h3xwave/vdb-guardian/internal/schema"
)

type compareAppliedSchemaOptions struct {
	SchemaPlanPath string
	LiveSchemaPath string
	OutputPath     string
}

// runCompareAppliedSchemaCommand compares a pgvector schema plan artifact with a
// live pgvector schema inspection artifact and writes a deterministic JSON drift
// report.
func runCompareAppliedSchemaCommand(args []string, stdout io.Writer) error {
	options, err := parseCompareAppliedSchemaOptions(args)
	if err != nil {
		return err
	}
	plan, err := loadCompareAppliedSchemaPlan(options.SchemaPlanPath)
	if err != nil {
		return err
	}
	live, err := loadCompareAppliedLiveSchema(options.LiveSchemaPath)
	if err != nil {
		return err
	}
	report, err := schema.CompareAppliedPGVectorSchema(plan, live, schema.AppliedSchemaCompareOptions{
		SchemaPlanPath: options.SchemaPlanPath,
		LiveSchemaPath: options.LiveSchemaPath,
	})
	if err != nil {
		return err
	}
	if options.OutputPath == "" {
		if err = writeCompareAppliedSchemaReport(stdout, report); err != nil {
			return err
		}
	} else {
		if err = writeCompareAppliedSchemaReportFile(options.OutputPath, report); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "applied schema comparison completed\n")
		fmt.Fprintf(stdout, "status: %s\n", report.Status)
		fmt.Fprintf(stdout, "mismatches: %d\n", report.Summary.MismatchCount)
		fmt.Fprintf(stdout, "warnings: %d\n", report.Summary.WarningCount)
	}
	if report.Status == schema.SchemaPlanCompareStatusFail {
		return fmt.Errorf("applied schema comparison failed with %d mismatch(es)", report.Summary.MismatchCount)
	}
	return nil
}

func parseCompareAppliedSchemaOptions(args []string) (compareAppliedSchemaOptions, error) {
	options := compareAppliedSchemaOptions{}
	flags := flag.NewFlagSet("compare-applied-schema", flag.ContinueOnError)
	flags.StringVar(&options.SchemaPlanPath, "schema-plan", "", "path to pgvector schema plan JSON")
	flags.StringVar(&options.LiveSchemaPath, "live-schema", "", "path to live pgvector schema inspection JSON")
	flags.StringVar(&options.OutputPath, "output", "", "optional path for applied schema comparison JSON report")
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		return compareAppliedSchemaOptions{}, err
	}
	if options.SchemaPlanPath == "" {
		return compareAppliedSchemaOptions{}, errors.New("--schema-plan is required")
	}
	if options.LiveSchemaPath == "" {
		return compareAppliedSchemaOptions{}, errors.New("--live-schema is required")
	}
	return options, nil
}

func loadCompareAppliedSchemaPlan(path string) (schema.PGVectorSchemaPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return schema.PGVectorSchemaPlan{}, fmt.Errorf("read pgvector schema plan %q: %w", path, err)
	}
	var plan schema.PGVectorSchemaPlan
	if err = json.Unmarshal(data, &plan); err != nil {
		return schema.PGVectorSchemaPlan{}, fmt.Errorf("parse pgvector schema plan %q: %w", path, err)
	}
	return plan, nil
}

func loadCompareAppliedLiveSchema(path string) (schema.PGVectorLiveSchemaInspection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return schema.PGVectorLiveSchemaInspection{}, fmt.Errorf("read live pgvector schema inspection %q: %w", path, err)
	}
	var live schema.PGVectorLiveSchemaInspection
	if err = json.Unmarshal(data, &live); err != nil {
		return schema.PGVectorLiveSchemaInspection{}, fmt.Errorf("parse live pgvector schema inspection %q: %w", path, err)
	}
	return live, nil
}

func writeCompareAppliedSchemaReportFile(path string, report schema.AppliedSchemaCompareReport) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open applied schema comparison report %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()
	if err = writeCompareAppliedSchemaReport(file, report); err != nil {
		return fmt.Errorf("write applied schema comparison report %q: %w", path, err)
	}
	return nil
}

func writeCompareAppliedSchemaReport(writer io.Writer, report schema.AppliedSchemaCompareReport) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode applied schema comparison report: %w", err)
	}
	return nil
}
