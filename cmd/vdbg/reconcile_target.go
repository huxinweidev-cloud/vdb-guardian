package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/h3xwave/vdb-guardian/internal/migration"
)

type reconcileTargetOptions struct {
	SourcePath string
	TargetPath string
	OutputPath string
}

// runReconcileTargetCommand reconciles local source and target full-record
// artifacts and writes a machine-readable stale row audit report. It is
// artifact-only and never connects to a source or target database.
func runReconcileTargetCommand(args []string) error {
	options, err := parseReconcileTargetOptions(args)
	if err != nil {
		return err
	}
	source, err := readFullRecordArtifact(options.SourcePath)
	if err != nil {
		return fmt.Errorf("read source full-record artifact: %w", err)
	}
	target, err := readFullRecordArtifact(options.TargetPath)
	if err != nil {
		return fmt.Errorf("read target full-record artifact: %w", err)
	}
	report, err := migration.BuildTargetReconciliationReport(source, target)
	if err != nil {
		return err
	}
	if err := migration.WriteTargetReconciliationReport(options.OutputPath, report); err != nil {
		return err
	}
	fmt.Printf("target reconciliation completed\n")
	fmt.Printf("status: %s\n", report.Status)
	fmt.Printf("source_records: %d\n", report.Summary.SourceRecordCount)
	fmt.Printf("target_records: %d\n", report.Summary.TargetRecordCount)
	fmt.Printf("stale_target_records: %d\n", report.Summary.StaleTargetCount)
	fmt.Printf("missing_target_records: %d\n", report.Summary.MissingTargetCount)
	fmt.Printf("changed_records: %d\n", report.Summary.ChangedRecordCount)
	fmt.Printf("result: %s\n", options.OutputPath)
	if report.Status != migration.TargetReconciliationStatusPass {
		return fmt.Errorf("target reconciliation failed: %d stale target records, %d missing target records, %d changed records", report.Summary.StaleTargetCount, report.Summary.MissingTargetCount, report.Summary.ChangedRecordCount)
	}
	return nil
}

func parseReconcileTargetOptions(args []string) (reconcileTargetOptions, error) {
	flagSet := flag.NewFlagSet("reconcile-target", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var options reconcileTargetOptions
	flagSet.StringVar(&options.SourcePath, "source", "", "path to source full-record artifact JSON")
	flagSet.StringVar(&options.TargetPath, "target", "", "path to target full-record artifact JSON")
	flagSet.StringVar(&options.OutputPath, "output", "", "path to write target reconciliation report JSON")
	if err := flagSet.Parse(args); err != nil {
		return reconcileTargetOptions{}, err
	}
	if options.SourcePath == "" {
		return reconcileTargetOptions{}, errors.New("source full-record artifact path is required")
	}
	if options.TargetPath == "" {
		return reconcileTargetOptions{}, errors.New("target full-record artifact path is required")
	}
	if options.OutputPath == "" {
		return reconcileTargetOptions{}, errors.New("output path is required")
	}
	return options, nil
}
