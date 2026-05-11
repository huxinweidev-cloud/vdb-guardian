package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/connectors"
	"github.com/huxinweidev-cloud/vdb-guardian/internal/fingerprints"
)

type milvusArtifactOptions struct {
	FixturePath string
	Address     string
	OutputPath  string
	Collection  string
	IDField     string
	VectorField string
	TopK        int
	ExpandK     int
	StableK     int
	BoundaryK   int
	Metric      string
}

// runMilvusArtifactCommand builds a fingerprint artifact from real Milvus
// searches over synthetic fixture queries.
//
// It connects to an existing Milvus service, searches every query in the
// fixture, normalizes hits through the connector contract, and writes a
// Python-compatible source fingerprint artifact. It does not start Docker or
// mutate the database.
//
// runMilvusArtifactCommand 基于对合成固件查询的真实 Milvus 检索结果，
// 构建出一份源端指纹产物。
//
// 它会连接到一个现有的 Milvus 服务，对固件中的每一条查询执行检索，通过连接器契约
// 将命中结果规范化，并最终写入一份与 Python 完全兼容的源端指纹产物文件。
// 它绝不会启动 Docker 容器，也绝对不会篡改数据库中的任何数据。
func runMilvusArtifactCommand(ctx context.Context, args []string) error {
	return runMilvusArtifactWithFactory(ctx, args, newMilvusSearchConnector)
}

func runMilvusArtifactWithFactory(ctx context.Context, args []string, factory func(string, connectors.MilvusConfig) (milvusSearchConnector, error)) error {
	options, err := parseMilvusArtifactOptions(args)
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
	connector, err := factory(options.Address, connectors.MilvusConfig{
		Address:           options.Address,
		DefaultCollection: options.Collection,
		IDField:           options.IDField,
		VectorField:       options.VectorField,
		Metric:            options.Metric,
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
			return fmt.Errorf("search Milvus for query %q: %w", query.ID, err)
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
	fmt.Printf("Milvus fingerprint artifact written\n")
	fmt.Printf("fixture: %s\n", options.FixturePath)
	fmt.Printf("output: %s\n", options.OutputPath)
	fmt.Printf("collection: %s\n", options.Collection)
	fmt.Printf("queries: %d\n", len(dataset.Queries))
	fmt.Printf("top_k: %d\n", options.TopK)
	fmt.Printf("expand_k: %d\n", options.ExpandK)
	fmt.Printf("stable_k: %d\n", options.StableK)
	fmt.Printf("boundary_k: %d\n", options.BoundaryK)
	return nil
}

func parseMilvusArtifactOptions(args []string) (milvusArtifactOptions, error) {
	flagSet := flag.NewFlagSet("build-milvus-artifact", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var options milvusArtifactOptions
	flagSet.StringVar(&options.FixturePath, "fixture", "", "path to a synthetic fixture JSON file")
	flagSet.StringVar(&options.Address, "address", "", "Milvus gRPC address for search")
	flagSet.StringVar(&options.OutputPath, "output", "", "path to write the source fingerprint artifact")
	flagSet.StringVar(&options.Collection, "collection", "items", "Milvus collection to search")
	flagSet.StringVar(&options.IDField, "id-field", "id", "Milvus primary key field returned in search results")
	flagSet.StringVar(&options.VectorField, "vector-field", "embedding", "Milvus float vector field name")
	flagSet.IntVar(&options.TopK, "top-k", 3, "business-visible topK result count")
	flagSet.IntVar(&options.ExpandK, "expand-k", 5, "expanded result count for boundary artifact building")
	flagSet.IntVar(&options.StableK, "stable-k", 2, "leading hit count for stable_neighbors")
	flagSet.IntVar(&options.BoundaryK, "boundary-k", 1, "rank-window width around the topK cutoff")
	flagSet.StringVar(&options.Metric, "metric", connectors.MilvusMetricCosine, "Milvus metric: cosine or l2")
	if err := flagSet.Parse(args); err != nil {
		return milvusArtifactOptions{}, err
	}
	if options.FixturePath == "" {
		return milvusArtifactOptions{}, errors.New("fixture path is required")
	}
	if options.Address == "" {
		return milvusArtifactOptions{}, errors.New("address is required")
	}
	if options.OutputPath == "" {
		return milvusArtifactOptions{}, errors.New("output path is required")
	}
	if options.TopK <= 0 {
		return milvusArtifactOptions{}, errors.New("top-k must be positive")
	}
	if options.StableK <= 0 || options.StableK > options.TopK {
		return milvusArtifactOptions{}, errors.New("stable-k must be positive and less than or equal to top-k")
	}
	if options.BoundaryK <= 0 {
		return milvusArtifactOptions{}, errors.New("boundary-k must be positive")
	}
	if options.ExpandK < options.TopK+options.BoundaryK {
		return milvusArtifactOptions{}, errors.New("expand-k must be greater than or equal to top-k plus boundary-k")
	}
	if options.Metric != connectors.MilvusMetricCosine && options.Metric != connectors.MilvusMetricL2 {
		return milvusArtifactOptions{}, fmt.Errorf("unsupported Milvus metric %q", options.Metric)
	}
	return options, nil
}
