package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/h3xwave/vdb-guardian/internal/migration"
)

type buildPGVectorRecordArtifactOptions struct {
	ConnectionURL     string
	RecordMappingPath string
	OutputPath        string
}

// runBuildPGVectorRecordArtifactCommand builds a target full-record artifact
// from live pgvector rows according to a passing record mapping artifact.
func runBuildPGVectorRecordArtifactCommand(ctx context.Context, args []string) error {
	options, err := parseBuildPGVectorRecordArtifactOptions(args)
	if err != nil {
		return err
	}
	mapping, err := loadSinglePassingRecordMapping(options.RecordMappingPath)
	if err != nil {
		return err
	}
	reader := migration.NewPGXPGVectorFullRecordReader(options.ConnectionURL)
	records, err := reader.ReadPGVectorFullRecords(ctx, migration.PGVectorFullRecordReadRequestFromMapping(mapping))
	if err != nil {
		return fmt.Errorf("read pgvector full records: %w", err)
	}
	return writePGVectorFullRecordArtifact(options, mapping, records)
}

func parseBuildPGVectorRecordArtifactOptions(args []string) (buildPGVectorRecordArtifactOptions, error) {
	flagSet := flag.NewFlagSet("build-pgvector-record-artifact", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})
	var options buildPGVectorRecordArtifactOptions
	flagSet.StringVar(&options.ConnectionURL, "pgvector-connection-url", "", "PostgreSQL connection URL for reading pgvector full records")
	flagSet.StringVar(&options.RecordMappingPath, "record-mapping", "", "map-migration-records JSON path")
	flagSet.StringVar(&options.OutputPath, "output", "", "path to write target full-record artifact JSON")
	if err := flagSet.Parse(args); err != nil {
		return buildPGVectorRecordArtifactOptions{}, err
	}
	if options.ConnectionURL == "" {
		return buildPGVectorRecordArtifactOptions{}, errors.New("pgvector-connection-url is required")
	}
	if options.RecordMappingPath == "" {
		return buildPGVectorRecordArtifactOptions{}, errors.New("record-mapping is required")
	}
	if options.OutputPath == "" {
		return buildPGVectorRecordArtifactOptions{}, errors.New("output path is required")
	}
	return options, nil
}

func runBuildPGVectorRecordArtifactWithReader(ctx context.Context, args []string, reader pgvectorFullRecordReader) error {
	options, err := parseBuildPGVectorRecordArtifactOptions(args)
	if err != nil {
		return err
	}
	mapping, err := loadSinglePassingRecordMapping(options.RecordMappingPath)
	if err != nil {
		return err
	}
	records, err := reader.ReadPGVectorFullRecords(ctx, migration.PGVectorFullRecordReadRequestFromMapping(mapping))
	if err != nil {
		return fmt.Errorf("read pgvector full records: %w", err)
	}
	return writePGVectorFullRecordArtifact(options, mapping, records)
}

func writePGVectorFullRecordArtifact(options buildPGVectorRecordArtifactOptions, mapping migration.CollectionRecordMapping, records []migration.VectorMigrationRecord) error {
	artifact, err := migration.BuildFullRecordArtifact(records, migration.FullRecordArtifactBuildOptions{System: "pgvector", Collection: mapping.TargetTable, RecordMappingPath: options.RecordMappingPath})
	if err != nil {
		return err
	}
	if err := writeFullRecordArtifact(options.OutputPath, artifact); err != nil {
		return err
	}
	fmt.Printf("pgvector full-record artifact written\n")
	fmt.Printf("output: %s\n", options.OutputPath)
	fmt.Printf("table: %s\n", mapping.TargetTable)
	fmt.Printf("records: %d\n", len(artifact.Records))
	return nil
}

type pgvectorFullRecordReader interface {
	ReadPGVectorFullRecords(ctx context.Context, request migration.PGVectorFullRecordReadRequest) ([]migration.VectorMigrationRecord, error)
}
