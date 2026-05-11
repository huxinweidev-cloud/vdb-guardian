package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/connectors"
	"github.com/huxinweidev-cloud/vdb-guardian/internal/fingerprints"
)

type pgvectorArtifactOptions struct {
	FixturePath   string
	ConnectionURL string
	OutputPath    string
	Collection    string
	TopK          int
	ExpandK       int
	StableK       int
	BoundaryK     int
	Metric        string
}

// runPGVectorArtifactCommand builds a fingerprint artifact from real pgvector
// searches over synthetic fixture queries.
//
// It connects to an existing PostgreSQL/pgvector service, searches every query
// in the fixture, normalizes hits through the connector contract, and writes a
// Python-compatible fingerprint artifact. It does not start Docker or mutate the
// database.
//
// runPGVectorArtifactCommand 基于对合成固件查询的真实 pgvector 检索结果，
// 构建出一份目标端指纹产物。
//
// 它会连接到一个现有的 PostgreSQL/pgvector 服务，对固件中的每一条查询执行检索，
// 通过连接器契约将命中结果规范化，并最终写入一份与 Python 完全兼容的指纹产物文件。
// 它绝不会启动 Docker 容器，也绝对不会篡改数据库中的任何数据。
func runPGVectorArtifactCommand(ctx context.Context, args []string) error {
	return runPGVectorArtifactWithFactory(ctx, args, newPGVectorSearchConnector)
}

func runPGVectorArtifactWithFactory(ctx context.Context, args []string, factory func(string, connectors.PGVectorConfig) (pgvectorSearchConnector, error)) error {
	options, err := parsePGVectorArtifactOptions(args)
	if err != nil {
		return err
	}
	dataset, err := loadSyntheticDatasetFile(options.FixturePath)
	if err != nil {
		return err
	}
	if len(dataset.Queries) == 0 {
		return errors.New("synthetic fixture must contain at least one query")
	}
	connector, err := factory(options.ConnectionURL, connectors.PGVectorConfig{
		ConnectionURL: options.ConnectionURL,
		DefaultTable:  options.Collection,
		Metric:        options.Metric,
	})
	if err != nil {
		return err
	}
	defer connector.Close()
	if err := connector.Connect(ctx); err != nil {
		return err
	}
	results := make([]fingerprints.SearchResult, 0, len(dataset.Queries))
	for _, query := range dataset.Queries {
		response, err := connector.Search(ctx, connectors.SearchRequest{
			Collection:  options.Collection,
			QueryVector: query.Vector,
			TopK:        options.TopK,
			ExpandK:     options.ExpandK,
		})
		if err != nil {
			return fmt.Errorf("search pgvector for query %q: %w", query.ID, err)
		}
		results = append(results, fingerprints.SearchResult{
			QueryID: query.ID,
			Hits:    fingerprintHitsFromConnector(response.Hits),
		})
	}
	artifact, err := fingerprints.BuildArtifact(results, fingerprints.BuildOptions{
		TopK:      options.TopK,
		StableK:   options.StableK,
		BoundaryK: options.BoundaryK,
	})
	if err != nil {
		return err
	}
	if err := fingerprints.WriteArtifact(options.OutputPath, artifact); err != nil {
		return err
	}
	fmt.Printf("pgvector fingerprint artifact written\n")
	fmt.Printf("fixture: %s\n", options.FixturePath)
	fmt.Printf("output: %s\n", options.OutputPath)
	fmt.Printf("table: %s\n", options.Collection)
	fmt.Printf("queries: %d\n", len(dataset.Queries))
	fmt.Printf("top_k: %d\n", options.TopK)
	fmt.Printf("expand_k: %d\n", options.ExpandK)
	fmt.Printf("stable_k: %d\n", options.StableK)
	fmt.Printf("boundary_k: %d\n", options.BoundaryK)
	return nil
}

func parsePGVectorArtifactOptions(args []string) (pgvectorArtifactOptions, error) {
	flagSet := flag.NewFlagSet("build-pgvector-artifact", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var options pgvectorArtifactOptions
	flagSet.StringVar(&options.FixturePath, "fixture", "", "path to a synthetic fixture JSON file")
	flagSet.StringVar(&options.ConnectionURL, "connection-url", "", "PostgreSQL connection URL for pgvector search")
	flagSet.StringVar(&options.OutputPath, "output", "", "path to write the target fingerprint artifact")
	flagSet.StringVar(&options.Collection, "table", "items", "pgvector table to search")
	flagSet.IntVar(&options.TopK, "top-k", 3, "business-visible topK result count")
	flagSet.IntVar(&options.ExpandK, "expand-k", 5, "expanded result count for boundary artifact building")
	flagSet.IntVar(&options.StableK, "stable-k", 2, "leading hit count for stable_neighbors")
	flagSet.IntVar(&options.BoundaryK, "boundary-k", 1, "rank-window width around the topK cutoff")
	flagSet.StringVar(&options.Metric, "metric", connectors.PGVectorMetricCosine, "pgvector metric: cosine or l2")
	if err := flagSet.Parse(args); err != nil {
		return pgvectorArtifactOptions{}, err
	}
	if options.FixturePath == "" {
		return pgvectorArtifactOptions{}, errors.New("fixture path is required")
	}
	if options.ConnectionURL == "" {
		return pgvectorArtifactOptions{}, errors.New("connection-url is required")
	}
	if options.OutputPath == "" {
		return pgvectorArtifactOptions{}, errors.New("output path is required")
	}
	if options.TopK <= 0 {
		return pgvectorArtifactOptions{}, errors.New("top-k must be positive")
	}
	if options.StableK <= 0 || options.StableK > options.TopK {
		return pgvectorArtifactOptions{}, errors.New("stable-k must be positive and less than or equal to top-k")
	}
	if options.BoundaryK <= 0 {
		return pgvectorArtifactOptions{}, errors.New("boundary-k must be positive")
	}
	if options.ExpandK < options.TopK+options.BoundaryK {
		return pgvectorArtifactOptions{}, errors.New("expand-k must be greater than or equal to top-k plus boundary-k")
	}
	if options.Metric != connectors.PGVectorMetricCosine && options.Metric != connectors.PGVectorMetricL2 {
		return pgvectorArtifactOptions{}, fmt.Errorf("unsupported pgvector metric %q", options.Metric)
	}
	return options, nil
}

func fingerprintHitsFromConnector(hits []connectors.SearchHit) []fingerprints.SearchHit {
	converted := make([]fingerprints.SearchHit, len(hits))
	for index, hit := range hits {
		converted[index] = fingerprints.SearchHit{ID: hit.ID, Rank: hit.Rank, Score: hit.Score}
	}
	return converted
}
