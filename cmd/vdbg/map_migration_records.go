package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/h3xwave/vdb-guardian/internal/migration"
	planschema "github.com/h3xwave/vdb-guardian/internal/schema"
)

type mapMigrationRecordsOptions struct {
	SchemaPlanPath string
	OutputPath     string
}

func runMapMigrationRecordsCommand(args []string, stdout io.Writer) error {
	options, err := parseMapMigrationRecordsOptions(args)
	if err != nil {
		return err
	}
	var schemaPlan planschema.PGVectorSchemaPlan
	if readErr := readMapMigrationRecordsJSON(options.SchemaPlanPath, &schemaPlan); readErr != nil {
		return fmt.Errorf("read schema plan: %w", readErr)
	}
	report, err := migration.BuildRecordMappingPlan(schemaPlan, migration.RecordMappingOptions{SchemaPlanPath: options.SchemaPlanPath})
	if err != nil {
		return err
	}
	if err := writeMapMigrationRecordsReport(options.OutputPath, stdout, report); err != nil {
		return err
	}
	if report.Status == migration.RecordMappingStatusFail {
		return fmt.Errorf("record mapping failed with %d blocking issue(s)", report.Summary.BlockingIssueCount)
	}
	return nil
}

func parseMapMigrationRecordsOptions(args []string) (mapMigrationRecordsOptions, error) {
	options := mapMigrationRecordsOptions{}
	flags := flag.NewFlagSet("map-migration-records", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.SchemaPlanPath, "schema-plan", "", "path to pgvector schema plan JSON")
	flags.StringVar(&options.OutputPath, "output", "", "optional record mapping JSON output path")
	if err := flags.Parse(args); err != nil {
		return mapMigrationRecordsOptions{}, err
	}
	if options.SchemaPlanPath == "" {
		return mapMigrationRecordsOptions{}, errors.New("schema-plan is required")
	}
	return options, nil
}

func readMapMigrationRecordsJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func writeMapMigrationRecordsReport(path string, stdout io.Writer, report migration.RecordMappingPlan) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "" {
		_, err = stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
