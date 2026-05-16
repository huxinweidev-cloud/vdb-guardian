package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/migration"
)

type compareFullRecordsOptions struct {
	SourcePath string
	TargetPath string
	OutputPath string
}

// runCompareFullRecordsCommand compares local full-record artifacts and writes a
// machine-readable equality report. It is artifact-only and never connects to a
// source or target database.
func runCompareFullRecordsCommand(args []string) error {
	return runCompareFullRecords(args)
}

func parseCompareFullRecordsOptions(args []string) (compareFullRecordsOptions, error) {
	flagSet := flag.NewFlagSet("compare-full-records", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var options compareFullRecordsOptions
	flagSet.StringVar(&options.SourcePath, "source", "", "path to source full-record artifact JSON")
	flagSet.StringVar(&options.TargetPath, "target", "", "path to target full-record artifact JSON")
	flagSet.StringVar(&options.OutputPath, "output", "", "path to write full-record comparison report JSON")
	if err := flagSet.Parse(args); err != nil {
		return compareFullRecordsOptions{}, err
	}
	if options.SourcePath == "" {
		return compareFullRecordsOptions{}, errors.New("source full-record artifact path is required")
	}
	if options.TargetPath == "" {
		return compareFullRecordsOptions{}, errors.New("target full-record artifact path is required")
	}
	if options.OutputPath == "" {
		return compareFullRecordsOptions{}, errors.New("output path is required")
	}
	return options, nil
}

func runCompareFullRecords(args []string) error {
	options, err := parseCompareFullRecordsOptions(args)
	if err != nil {
		return err
	}
	report, err := compareFullRecordsFromFiles(options.SourcePath, options.TargetPath)
	if err != nil {
		return err
	}
	if err := writeFullRecordCompareReport(options.OutputPath, report); err != nil {
		return err
	}
	fmt.Printf("full-record comparison completed\n")
	fmt.Printf("status: %s\n", report.Status)
	fmt.Printf("source_records: %d\n", report.Source.RecordCount)
	fmt.Printf("target_records: %d\n", report.Target.RecordCount)
	fmt.Printf("mismatched_records: %d\n", report.Summary.MismatchedRecords)
	fmt.Printf("result: %s\n", options.OutputPath)
	if report.Status != migration.FullRecordCompareStatusPass {
		return fmt.Errorf("full-record comparison failed: %d mismatched records, %d missing source records, %d missing target records", report.Summary.MismatchedRecords, report.Summary.MissingSourceRecords, report.Summary.MissingTargetRecords)
	}
	return nil
}

func compareFullRecordsFromFiles(sourcePath, targetPath string) (migration.FullRecordCompareReport, error) {
	source, err := readFullRecordArtifact(sourcePath)
	if err != nil {
		return migration.FullRecordCompareReport{}, fmt.Errorf("read source full-record artifact: %w", err)
	}
	target, err := readFullRecordArtifact(targetPath)
	if err != nil {
		return migration.FullRecordCompareReport{}, fmt.Errorf("read target full-record artifact: %w", err)
	}
	report, err := migration.CompareFullRecordArtifacts(source, target)
	if err != nil {
		return migration.FullRecordCompareReport{}, err
	}
	return report, nil
}

func readFullRecordArtifact(path string) (migration.FullRecordArtifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return migration.FullRecordArtifact{}, err
	}
	var artifact migration.FullRecordArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return migration.FullRecordArtifact{}, fmt.Errorf("decode %q: %w", path, err)
	}
	return artifact, nil
}

func writeFullRecordCompareReport(path string, report migration.FullRecordCompareReport) error {
	data, err := migration.MarshalFullRecordCompareReport(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create full-record compare output dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write full-record compare report %q: %w", path, err)
	}
	return nil
}
