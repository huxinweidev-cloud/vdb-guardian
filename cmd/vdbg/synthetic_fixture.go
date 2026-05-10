package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/fixtures"
)

type generateSyntheticFixtureOptions struct {
	OutputPath       string
	SyntheticOptions fixtures.SyntheticOptions
}

// runGenerateSyntheticFixture generates a deterministic synthetic vector dataset
// fixture for local Milvus to pgvector migration experiments.
//
// The command writes JSON only and does not connect to Docker, Milvus, or
// PostgreSQL. The generated file becomes the stable input for later database
// seeders and migrate-and-verify commands.
func runGenerateSyntheticFixture(args []string) error {
	options, err := parseGenerateSyntheticFixtureOptions(args)
	if err != nil {
		return err
	}
	dataset, err := fixtures.GenerateSyntheticDataset(options.SyntheticOptions)
	if err != nil {
		return err
	}
	if err := fixtures.WriteSyntheticDataset(options.OutputPath, dataset); err != nil {
		return err
	}
	fmt.Printf("synthetic fixture generated\n")
	fmt.Printf("output: %s\n", options.OutputPath)
	fmt.Printf("dimension: %d\n", dataset.Dimension)
	fmt.Printf("records: %d\n", len(dataset.Records))
	fmt.Printf("queries: %d\n", len(dataset.Queries))
	fmt.Printf("metric: %s\n", dataset.Metric)
	return nil
}

func parseGenerateSyntheticFixtureOptions(args []string) (generateSyntheticFixtureOptions, error) {
	flagSet := flag.NewFlagSet("generate-synthetic-fixture", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var outputPath string
	var seed int64
	var dimension int
	var recordCount int
	var queryCount int
	var metric string
	flagSet.StringVar(&outputPath, "output", "", "path to write the synthetic dataset JSON")
	flagSet.Int64Var(&seed, "seed", 42, "deterministic random seed")
	flagSet.IntVar(&dimension, "dimension", 8, "dense vector dimension, 1..2000")
	flagSet.IntVar(&recordCount, "records", 100, "number of database records to generate")
	flagSet.IntVar(&queryCount, "queries", 10, "number of query vectors to generate")
	flagSet.StringVar(&metric, "metric", fixtures.MetricCosine, "similarity metric: cosine or l2")
	if err := flagSet.Parse(args); err != nil {
		return generateSyntheticFixtureOptions{}, err
	}
	if outputPath == "" {
		return generateSyntheticFixtureOptions{}, errors.New("output path is required")
	}
	return generateSyntheticFixtureOptions{
		OutputPath: outputPath,
		SyntheticOptions: fixtures.SyntheticOptions{
			Seed:        seed,
			Dimension:   dimension,
			RecordCount: recordCount,
			QueryCount:  queryCount,
			Metric:      metric,
		},
	}, nil
}

type discardFlagOutput struct{}

func (discardFlagOutput) Write(p []byte) (int, error) {
	return len(p), nil
}
