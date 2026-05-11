package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/fixtures"
	"github.com/huxinweidev-cloud/vdb-guardian/internal/migration"
)

type seedPGVectorOptions struct {
	FixturePath   string
	ConnectionURL string
	SeederConfig  migration.PGVectorSeederConfig
}

type pgvectorSeedRunner interface {
	Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (migration.PGVectorSeedResult, error)
}

type closablePGVectorSeedRunner interface {
	pgvectorSeedRunner
	Close() error
}

// runSeedPGVectorCommand seeds a pgvector table from a synthetic fixture.
//
// The command performs real PostgreSQL writes through a pgx-backed seeder adapter.
// It does not start Docker and does not create local services; callers must point
// it at an already-running local or test PostgreSQL instance with pgvector
// available.
//
// runSeedPGVectorCommand 根据合成测试固件向 pgvector 数据表中灌入数据。
//
// 该命令通过基于 pgx 的灌入适配器执行真实的 PostgreSQL 写入操作。
// 它不会启动 Docker，也不会创建本地服务；调用方必须提供一个已经处于运行状态、
// 且安装了 pgvector 扩展的本地或测试 PostgreSQL 实例的连接信息。
func runSeedPGVectorCommand(ctx context.Context, args []string) error {
	return runSeedPGVectorWithFactory(ctx, args, newPGVectorSeedRunner)
}

func runSeedPGVectorWithFactory(ctx context.Context, args []string, factory func(string, migration.PGVectorSeederConfig) (pgvectorSeedRunner, error)) error {
	options, err := parseSeedPGVectorOptions(args)
	if err != nil {
		return err
	}
	dataset, err := loadSyntheticDatasetFile(options.FixturePath)
	if err != nil {
		return err
	}
	options.SeederConfig.Dimension = dataset.Dimension
	runner, err := factory(options.ConnectionURL, options.SeederConfig)
	if err != nil {
		return err
	}
	if closer, ok := runner.(closablePGVectorSeedRunner); ok {
		defer closer.Close()
	}
	result, err := runner.Seed(ctx, dataset)
	if err != nil {
		return err
	}
	fmt.Printf("pgvector fixture seeded\n")
	fmt.Printf("fixture: %s\n", options.FixturePath)
	fmt.Printf("table: %s\n", result.Table)
	fmt.Printf("dimension: %d\n", result.Dimension)
	fmt.Printf("records_total: %d\n", result.RecordsTotal)
	fmt.Printf("records_seeded: %d\n", result.RecordsSeeded)
	return nil
}

func parseSeedPGVectorOptions(args []string) (seedPGVectorOptions, error) {
	flagSet := flag.NewFlagSet("seed-pgvector", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var fixturePath string
	var connectionURL string
	var table string
	var idColumn string
	var vectorColumn string
	flagSet.StringVar(&fixturePath, "fixture", "", "path to a synthetic fixture JSON file")
	flagSet.StringVar(&connectionURL, "connection-url", "", "PostgreSQL connection URL for pgvector seeding")
	flagSet.StringVar(&table, "table", "items", "pgvector table to create/upsert")
	flagSet.StringVar(&idColumn, "id-column", "id", "text ID column name")
	flagSet.StringVar(&vectorColumn, "vector-column", "embedding", "pgvector column name")
	if err := flagSet.Parse(args); err != nil {
		return seedPGVectorOptions{}, err
	}
	if fixturePath == "" {
		return seedPGVectorOptions{}, errors.New("fixture path is required")
	}
	if connectionURL == "" {
		return seedPGVectorOptions{}, errors.New("connection-url is required")
	}
	return seedPGVectorOptions{
		FixturePath:   fixturePath,
		ConnectionURL: connectionURL,
		SeederConfig: migration.PGVectorSeederConfig{
			Table:        table,
			IDColumn:     idColumn,
			VectorColumn: vectorColumn,
		},
	}, nil
}

func loadSyntheticDatasetFile(path string) (fixtures.SyntheticDataset, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return fixtures.SyntheticDataset{}, fmt.Errorf("read synthetic fixture: %w", err)
	}
	var dataset fixtures.SyntheticDataset
	if err := json.Unmarshal(content, &dataset); err != nil {
		return fixtures.SyntheticDataset{}, fmt.Errorf("decode synthetic fixture: %w", err)
	}
	return dataset, nil
}

func newPGVectorSeedRunner(connectionURL string, config migration.PGVectorSeederConfig) (pgvectorSeedRunner, error) {
	db := migration.NewPGXPGVectorSeedDB(connectionURL)
	seeder, err := migration.NewPGVectorSeeder(config, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return pgvectorSeedRunnerWithClose{PGVectorSeeder: seeder, closer: db}, nil
}

type pgvectorSeedRunnerWithClose struct {
	migration.PGVectorSeeder
	closer interface{ Close() error }
}

func (runner pgvectorSeedRunnerWithClose) Close() error {
	return runner.closer.Close()
}
