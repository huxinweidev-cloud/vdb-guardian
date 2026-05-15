package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/schema"
	"github.com/jackc/pgx/v5"
)

type applyPGVectorSchemaOptions struct {
	SchemaPlanPath   string
	ArtifactDir      string
	JobID            string
	ConnectionURL    string
	Mode             string
	SkipIndexes      bool
	AllowUnsupported bool
}

type applyPGVectorSchemaExecutorFactory func(connectionURL string) (schema.PGVectorSchemaExecutor, func() error, error)

func closeApplyPGVectorSchemaExecutor(closeExecutor func() error) error {
	if closeExecutor == nil {
		return nil
	}
	return closeExecutor()
}

func runApplyPGVectorSchemaCommand(ctx context.Context, args []string) error {
	return runApplyPGVectorSchemaCommandWithFactory(ctx, args, os.Stdout, newPGVectorSchemaExecutor)
}

func runApplyPGVectorSchemaCommandWithFactory(ctx context.Context, args []string, stdout io.Writer, factory applyPGVectorSchemaExecutorFactory) error {
	options, err := parseApplyPGVectorSchemaOptions(args)
	if err != nil {
		return err
	}
	plan, err := loadApplyPGVectorSchemaPlan(options.SchemaPlanPath)
	if err != nil {
		return err
	}
	var executor schema.PGVectorSchemaExecutor
	var closeExecutor func() error
	if options.Mode == schema.PGVectorSchemaApplyModeExecute {
		if factory == nil {
			return errors.New("pgvector schema executor factory is required")
		}
		executor, closeExecutor, err = factory(options.ConnectionURL)
		if err != nil {
			return err
		}
		if closeExecutor != nil {
			defer func() {
				closeErr := closeApplyPGVectorSchemaExecutor(closeExecutor)
				if closeErr != nil {
					_, _ = fmt.Fprintf(io.Discard, "%v", closeErr)
				}
			}()
		}
	}
	report, applyErr := schema.ApplyPGVectorSchemaPlan(ctx, plan, executor, schema.PGVectorSchemaApplyOptions{
		JobID:            options.JobID,
		Mode:             options.Mode,
		SchemaPlanPath:   options.SchemaPlanPath,
		SkipIndexes:      options.SkipIndexes,
		AllowUnsupported: options.AllowUnsupported,
	})
	reportPath, writeErr := writeApplyPGVectorSchemaReport(options.ArtifactDir, options.JobID, report)
	if writeErr != nil {
		return writeErr
	}
	fmt.Fprintf(stdout, "pgvector schema apply report written to %s\n", reportPath)
	if applyErr != nil {
		return applyErr
	}
	return nil
}

func parseApplyPGVectorSchemaOptions(args []string) (applyPGVectorSchemaOptions, error) {
	flagSet := flag.NewFlagSet("apply-pgvector-schema", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	var options applyPGVectorSchemaOptions
	var dryRun bool
	var execute bool
	flagSet.StringVar(&options.SchemaPlanPath, "schema-plan", "", "path to a pgvector schema plan JSON file")
	flagSet.StringVar(&options.ArtifactDir, "artifact-dir", "artifacts", "directory for the schema apply report")
	flagSet.StringVar(&options.JobID, "job-id", "pgvector-schema-apply", "job ID used in the apply report filename")
	flagSet.StringVar(&options.ConnectionURL, "pgvector-connection-url", "", "PostgreSQL connection URL for execute mode")
	flagSet.BoolVar(&dryRun, "dry-run", false, "plan schema apply without connecting to PostgreSQL")
	flagSet.BoolVar(&execute, "execute", false, "execute schema DDL against PostgreSQL")
	flagSet.BoolVar(&options.SkipIndexes, "skip-indexes", false, "skip index DDL execution in execute mode")
	flagSet.BoolVar(&options.AllowUnsupported, "allow-unsupported", false, "allow execute mode even when the schema plan contains unsupported features")
	if err := flagSet.Parse(args); err != nil {
		return applyPGVectorSchemaOptions{}, err
	}
	if options.SchemaPlanPath == "" {
		return applyPGVectorSchemaOptions{}, errors.New("schema-plan is required")
	}
	if execute && dryRun {
		return applyPGVectorSchemaOptions{}, errors.New("choose either dry-run or execute, not both")
	}
	options.Mode = schema.PGVectorSchemaApplyModeDryRun
	if execute {
		options.Mode = schema.PGVectorSchemaApplyModeExecute
	}
	if options.Mode == schema.PGVectorSchemaApplyModeExecute && options.ConnectionURL == "" {
		return applyPGVectorSchemaOptions{}, errors.New("pgvector connection URL is required in execute mode")
	}
	if options.ArtifactDir == "" {
		return applyPGVectorSchemaOptions{}, errors.New("artifact-dir is required")
	}
	if options.JobID == "" {
		return applyPGVectorSchemaOptions{}, errors.New("job-id is required")
	}
	return options, nil
}

func loadApplyPGVectorSchemaPlan(path string) (schema.PGVectorSchemaPlan, error) {
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

func writeApplyPGVectorSchemaReport(artifactDir string, jobID string, report schema.PGVectorSchemaApplyReport) (string, error) {
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(artifactDir, fmt.Sprintf("%s-pgvector-schema-apply-report.json", jobID))
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

type pgxSchemaExecutor struct {
	conn *pgx.Conn
}

func newPGVectorSchemaExecutor(connectionURL string) (schema.PGVectorSchemaExecutor, func() error, error) {
	conn, err := pgx.Connect(context.Background(), connectionURL)
	if err != nil {
		return nil, nil, err
	}
	executor := &pgxSchemaExecutor{conn: conn}
	return executor, func() error { return conn.Close(context.Background()) }, nil
}

func (executor *pgxSchemaExecutor) Exec(ctx context.Context, sql string) error {
	_, err := executor.conn.Exec(ctx, sql)
	return err
}
