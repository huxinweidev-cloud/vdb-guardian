package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/h3xwave/vdb-guardian/internal/migration"
)

type cleanupTargetStaleOptions struct {
	ReconcileReportPath   string
	PGVectorConnectionURL string
	TargetTable           string
	TargetIDColumn        string
	OutputPath            string
	ConfirmDeleteStale    bool
}

type targetStaleDeleterFactory interface {
	New(connectionURL string, idColumn string) migration.TargetStaleDeleter
}

type pgvectorTargetStaleDeleterFactory struct{}

func (pgvectorTargetStaleDeleterFactory) New(connectionURL string, idColumn string) migration.TargetStaleDeleter {
	return migration.NewPGXPGVectorStaleTargetDeleterWithIDColumn(connectionURL, idColumn)
}

// runCleanupTargetStaleCommand deletes stale pgvector target rows from an audited reconciliation report.
func runCleanupTargetStaleCommand(ctx context.Context, args []string) error {
	return runCleanupTargetStaleCommandWithFactory(ctx, args, pgvectorTargetStaleDeleterFactory{})
}

func runCleanupTargetStaleCommandWithDeleter(args []string, factory targetStaleDeleterFactory) error {
	return runCleanupTargetStaleCommandWithFactory(context.Background(), args, factory)
}

func runCleanupTargetStaleCommandWithFactory(ctx context.Context, args []string, factory targetStaleDeleterFactory) error {
	options, err := parseCleanupTargetStaleOptions(args)
	if err != nil {
		return err
	}
	report, err := readTargetReconciliationReport(options.ReconcileReportPath)
	if err != nil {
		return err
	}
	deleter := factory.New(options.PGVectorConnectionURL, options.TargetIDColumn)
	result, err := migration.CleanupStaleTargetRecords(ctx, deleter, migration.TargetStaleCleanupRequest{
		TargetTable:        options.TargetTable,
		Report:             report,
		ConfirmDeleteStale: options.ConfirmDeleteStale,
	})
	if err != nil {
		return err
	}
	if err := writeTargetStaleCleanupResult(options.OutputPath, result); err != nil {
		return err
	}
	fmt.Printf("target stale cleanup completed\n")
	fmt.Printf("target_table: %s\n", result.TargetTable)
	fmt.Printf("requested_delete_count: %d\n", result.RequestedDeleteCount)
	fmt.Printf("deleted_count: %d\n", result.DeletedCount)
	fmt.Printf("result: %s\n", options.OutputPath)
	return nil
}

func parseCleanupTargetStaleOptions(args []string) (cleanupTargetStaleOptions, error) {
	flagSet := flag.NewFlagSet("cleanup-target-stale", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	options := cleanupTargetStaleOptions{TargetIDColumn: "id"}
	flagSet.StringVar(&options.ReconcileReportPath, "reconcile-report", "", "path to target reconciliation report JSON")
	flagSet.StringVar(&options.PGVectorConnectionURL, "pgvector-connection-url", "", "PostgreSQL connection URL for pgvector target")
	flagSet.StringVar(&options.TargetTable, "target-table", "", "pgvector target table to delete stale records from")
	flagSet.StringVar(&options.TargetIDColumn, "target-id-column", "id", "pgvector target id column")
	flagSet.StringVar(&options.OutputPath, "output", "", "path to write stale cleanup result JSON")
	flagSet.BoolVar(&options.ConfirmDeleteStale, "confirm-delete-stale", false, "explicitly confirm deletion of stale target records")
	if err := flagSet.Parse(args); err != nil {
		return cleanupTargetStaleOptions{}, err
	}
	if options.ReconcileReportPath == "" {
		return cleanupTargetStaleOptions{}, errors.New("reconcile-report is required")
	}
	if options.PGVectorConnectionURL == "" {
		return cleanupTargetStaleOptions{}, errors.New("pgvector-connection-url is required")
	}
	if options.TargetTable == "" {
		return cleanupTargetStaleOptions{}, errors.New("target-table is required")
	}
	if options.TargetIDColumn == "" {
		return cleanupTargetStaleOptions{}, errors.New("target-id-column is required")
	}
	if options.OutputPath == "" {
		return cleanupTargetStaleOptions{}, errors.New("output is required")
	}
	if !options.ConfirmDeleteStale {
		return cleanupTargetStaleOptions{}, errors.New("confirm-delete-stale is required for destructive stale target cleanup")
	}
	return options, nil
}

func readTargetReconciliationReport(path string) (migration.TargetReconciliationReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return migration.TargetReconciliationReport{}, fmt.Errorf("read target reconciliation report: %w", err)
	}
	var report migration.TargetReconciliationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return migration.TargetReconciliationReport{}, fmt.Errorf("decode target reconciliation report: %w", err)
	}
	return report, nil
}

func writeTargetStaleCleanupResult(path string, result migration.TargetStaleCleanupResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal target stale cleanup result: %w", err)
	}
	data = append(data, '\n')
	return migration.WriteSensitiveJSONFile0600(path, data, "target stale cleanup result")
}
