package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/h3xwave/vdb-guardian/internal/fixtures"
	"github.com/h3xwave/vdb-guardian/internal/migration"
)

type seedMilvusOptions struct {
	FixturePath  string
	Address      string
	SeederConfig migration.MilvusSeederConfig
}

type milvusSeedRunner interface {
	Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (migration.MilvusSeedResult, error)
}

type closableMilvusSeedRunner interface {
	milvusSeedRunner
	Close() error
}

// runSeedMilvusCommand seeds a Milvus collection from a synthetic fixture.
//
// The command performs real Milvus writes through the Go SDK. It drops and
// recreates the configured collection, so it is intended for local migration MVP
// smoke checks rather than production data loading.
//
// runSeedMilvusCommand 根据合成测试固件向 Milvus 集合中灌入数据。
//
// 该命令通过官方 Go SDK 执行真实的 Milvus 写入操作。它会先删除并重新创建已配置的集合，
// 因此它专为本地迁移 MVP 的冒烟测试而设计，绝不可用于生产环境的数据导入。
func runSeedMilvusCommand(ctx context.Context, args []string) error {
	return runSeedMilvusWithFactory(ctx, args, newMilvusSeedRunner)
}

func runSeedMilvusWithFactory(ctx context.Context, args []string, factory func(string, migration.MilvusSeederConfig) (milvusSeedRunner, error)) error {
	options, err := parseSeedMilvusOptions(args)
	if err != nil {
		return err
	}
	dataset, err := loadSyntheticDatasetFile(options.FixturePath)
	if err != nil {
		return err
	}
	options.SeederConfig.Dimension = dataset.Dimension
	runner, err := factory(options.Address, options.SeederConfig)
	if err != nil {
		return err
	}
	if closer, ok := runner.(closableMilvusSeedRunner); ok {
		defer closer.Close()
	}
	result, err := runner.Seed(ctx, dataset)
	if err != nil {
		return err
	}
	fmt.Printf("milvus fixture seeded\n")
	fmt.Printf("fixture: %s\n", options.FixturePath)
	fmt.Printf("collection: %s\n", result.Collection)
	fmt.Printf("dimension: %d\n", result.Dimension)
	fmt.Printf("records_total: %d\n", result.RecordsTotal)
	fmt.Printf("records_seeded: %d\n", result.RecordsSeeded)
	return nil
}

func parseSeedMilvusOptions(args []string) (seedMilvusOptions, error) {
	flagSet := flag.NewFlagSet("seed-milvus", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var fixturePath string
	var address string
	var collection string
	var idField string
	var vectorField string
	var metric string
	flagSet.StringVar(&fixturePath, "fixture", "", "path to a synthetic fixture JSON file")
	flagSet.StringVar(&address, "address", "", "Milvus gRPC address for fixture seeding")
	flagSet.StringVar(&collection, "collection", "items", "Milvus collection to recreate and seed")
	flagSet.StringVar(&idField, "id-field", "id", "text primary key field name")
	flagSet.StringVar(&vectorField, "vector-field", "embedding", "Milvus float vector field name")
	flagSet.StringVar(&metric, "metric", fixtures.MetricCosine, "Milvus vector metric: cosine or l2")
	if err := flagSet.Parse(args); err != nil {
		return seedMilvusOptions{}, err
	}
	if fixturePath == "" {
		return seedMilvusOptions{}, errors.New("fixture path is required")
	}
	if address == "" {
		return seedMilvusOptions{}, errors.New("address is required")
	}
	return seedMilvusOptions{
		FixturePath: fixturePath,
		Address:     address,
		SeederConfig: migration.MilvusSeederConfig{
			Collection:  collection,
			IDField:     idField,
			VectorField: vectorField,
			Metric:      metric,
		},
	}, nil
}

func newMilvusSeedRunner(address string, config migration.MilvusSeederConfig) (milvusSeedRunner, error) {
	db := migration.NewMilvusSDKSeedDB(address)
	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}
	seeder, err := migration.NewMilvusSeeder(config, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return milvusSeedRunnerWithClose{MilvusSeeder: seeder, closer: db}, nil
}

type milvusSeedRunnerWithClose struct {
	migration.MilvusSeeder
	closer interface{ Close() error }
}

func (runner milvusSeedRunnerWithClose) Close() error {
	return runner.closer.Close()
}
