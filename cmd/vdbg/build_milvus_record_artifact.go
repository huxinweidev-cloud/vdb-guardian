package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/connectors"
	"github.com/h3xwave/vdb-guardian/internal/migration"
)

type buildMilvusRecordArtifactOptions struct {
	Address           string
	RecordMappingPath string
	OutputPath        string
}

// runBuildMilvusRecordArtifactCommand builds a source full-record artifact from
// live Milvus records according to a passing record mapping artifact.
func runBuildMilvusRecordArtifactCommand(ctx context.Context, args []string) error {
	options, err := parseBuildMilvusRecordArtifactOptions(args)
	if err != nil {
		return err
	}
	mapping, err := loadSinglePassingRecordMapping(options.RecordMappingPath)
	if err != nil {
		return err
	}
	source, err := migration.NewMilvusVectorMigrationSource(connectors.MilvusConfig{Address: options.Address}, nil)
	if err != nil {
		return err
	}
	records, err := source.WithRecordMapping(mapping).ReadRecords(ctx, mapping.SourceCollection)
	if err != nil {
		return fmt.Errorf("read Milvus full records: %w", err)
	}
	return writeMilvusFullRecordArtifact(options, mapping, records)
}

func parseBuildMilvusRecordArtifactOptions(args []string) (buildMilvusRecordArtifactOptions, error) {
	flagSet := flag.NewFlagSet("build-milvus-record-artifact", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})
	var options buildMilvusRecordArtifactOptions
	flagSet.StringVar(&options.Address, "milvus-address", "", "Milvus gRPC address to read source full records from")
	flagSet.StringVar(&options.RecordMappingPath, "record-mapping", "", "map-migration-records JSON path")
	flagSet.StringVar(&options.OutputPath, "output", "", "path to write source full-record artifact JSON")
	if err := flagSet.Parse(args); err != nil {
		return buildMilvusRecordArtifactOptions{}, err
	}
	if options.Address == "" {
		return buildMilvusRecordArtifactOptions{}, errors.New("milvus-address is required")
	}
	if options.RecordMappingPath == "" {
		return buildMilvusRecordArtifactOptions{}, errors.New("record-mapping is required")
	}
	if options.OutputPath == "" {
		return buildMilvusRecordArtifactOptions{}, errors.New("output path is required")
	}
	return options, nil
}

func runBuildMilvusRecordArtifactWithReader(ctx context.Context, args []string, reader milvusFullRecordReader) error {
	options, err := parseBuildMilvusRecordArtifactOptions(args)
	if err != nil {
		return err
	}
	return runBuildMilvusRecordArtifactWithReaderAndOptions(ctx, options, reader)
}

func runBuildMilvusRecordArtifactWithReaderAndOptions(ctx context.Context, options buildMilvusRecordArtifactOptions, reader milvusFullRecordReader) error {
	mapping, err := loadSinglePassingRecordMapping(options.RecordMappingPath)
	if err != nil {
		return err
	}
	request, err := migration.MilvusReadRequestFromRecordMapping(mapping)
	if err != nil {
		return err
	}
	records, err := reader.ReadMilvusMigrationRecordsWithMapping(ctx, request)
	if err != nil {
		return fmt.Errorf("read Milvus full records: %w", err)
	}
	return writeMilvusFullRecordArtifact(options, mapping, records)
}

func writeMilvusFullRecordArtifact(options buildMilvusRecordArtifactOptions, mapping migration.CollectionRecordMapping, records []migration.VectorMigrationRecord) error {
	artifact, err := migration.BuildFullRecordArtifact(records, migration.FullRecordArtifactBuildOptions{System: "milvus", Collection: mapping.SourceCollection, RecordMappingPath: options.RecordMappingPath})
	if err != nil {
		return err
	}
	if err := writeFullRecordArtifact(options.OutputPath, artifact); err != nil {
		return err
	}
	fmt.Printf("Milvus full-record artifact written\n")
	fmt.Printf("output: %s\n", options.OutputPath)
	fmt.Printf("collection: %s\n", mapping.SourceCollection)
	fmt.Printf("records: %d\n", len(artifact.Records))
	return nil
}

type milvusFullRecordReader interface {
	ReadMilvusMigrationRecordsWithMapping(ctx context.Context, request migration.MilvusMigrationReadRequest) ([]migration.VectorMigrationRecord, error)
}

func loadSinglePassingRecordMapping(path string) (migration.CollectionRecordMapping, error) {
	var plan migration.RecordMappingPlan
	if err := readMigrateJSONFile(path, &plan); err != nil {
		return migration.CollectionRecordMapping{}, fmt.Errorf("read record mapping: %w", err)
	}
	if plan.Status != migration.RecordMappingStatusPass {
		return migration.CollectionRecordMapping{}, fmt.Errorf("record mapping status is %q", plan.Status)
	}
	if len(plan.Mappings) != 1 {
		return migration.CollectionRecordMapping{}, fmt.Errorf("record mapping must contain exactly one collection, got %d", len(plan.Mappings))
	}
	mapping := plan.Mappings[0]
	if mapping.PrimaryKey == nil || mapping.Vector == nil {
		return migration.CollectionRecordMapping{}, fmt.Errorf("record mapping is missing primary key or vector mapping")
	}
	return mapping, nil
}

func writeFullRecordArtifact(path string, artifact migration.FullRecordArtifact) error {
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal full-record artifact: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create full-record artifact output dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write full-record artifact %q: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod full-record artifact %q: %w", path, err)
	}
	return nil
}
