package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/connectors"
)

type searchPGVectorOptions struct {
	FixturePath   string
	ConnectionURL string
	Collection    string
	TopK          int
	ExpandK       int
	QueryIndex    int
	Metric        string
}

type pgvectorSearchConnector interface {
	Connect(ctx context.Context) error
	Count(ctx context.Context, collection string) (int64, error)
	Search(ctx context.Context, req connectors.SearchRequest) (connectors.SearchResponse, error)
	Close() error
}

// runSearchPGVectorCommand runs a real pgvector connector search against one
// query from a synthetic fixture.
//
// It is a smoke command for the target-side migration MVP. The command validates
// pgvector connectivity, counts seeded records, executes one normalized search,
// and prints ranked hits without starting Docker or writing data.
//
// runSearchPGVectorCommand 使用真实的 pgvector 连接器，针对合成固件中的单条查询执行检索。
//
// 这是用于目标端迁移 MVP 的冒烟测试命令。该命令用于验证 pgvector 连通性、统计已灌入的记录数、
// 执行一次规范化的向量检索，并打印出带排名的命中结果。它绝对不会启动 Docker，
// 也绝对不会写入任何数据。
func runSearchPGVectorCommand(ctx context.Context, args []string) error {
	return runSearchPGVectorWithFactory(ctx, args, newPGVectorSearchConnector)
}

func runSearchPGVectorWithFactory(ctx context.Context, args []string, factory func(string, connectors.PGVectorConfig) (pgvectorSearchConnector, error)) error {
	options, err := parseSearchPGVectorOptions(args)
	if err != nil {
		return err
	}
	dataset, err := loadSyntheticDatasetFile(options.FixturePath)
	if err != nil {
		return err
	}
	if options.QueryIndex >= len(dataset.Queries) {
		return fmt.Errorf("query-index %d is out of range for %d fixture queries", options.QueryIndex, len(dataset.Queries))
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
	count, err := connector.Count(ctx, options.Collection)
	if err != nil {
		return err
	}
	query := dataset.Queries[options.QueryIndex]
	response, err := connector.Search(ctx, connectors.SearchRequest{
		Collection:  options.Collection,
		QueryVector: query.Vector,
		TopK:        options.TopK,
		ExpandK:     options.ExpandK,
	})
	if err != nil {
		return err
	}
	fmt.Printf("pgvector search smoke ok\n")
	fmt.Printf("fixture: %s\n", options.FixturePath)
	fmt.Printf("table: %s\n", options.Collection)
	fmt.Printf("records_count: %d\n", count)
	fmt.Printf("query_id: %s\n", query.ID)
	fmt.Printf("top_k: %d\n", options.TopK)
	fmt.Printf("expand_k: %d\n", options.ExpandK)
	fmt.Printf("hits: %d\n", len(response.Hits))
	for _, hit := range response.Hits {
		fmt.Printf("hit rank=%d id=%s score=%g\n", hit.Rank, hit.ID, hit.Score)
	}
	return nil
}

func parseSearchPGVectorOptions(args []string) (searchPGVectorOptions, error) {
	flagSet := flag.NewFlagSet("search-pgvector", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var options searchPGVectorOptions
	flagSet.StringVar(&options.FixturePath, "fixture", "", "path to a synthetic fixture JSON file")
	flagSet.StringVar(&options.ConnectionURL, "connection-url", "", "PostgreSQL connection URL for pgvector search")
	flagSet.StringVar(&options.Collection, "table", "items", "pgvector table to search")
	flagSet.IntVar(&options.TopK, "top-k", 3, "business-visible topK result count")
	flagSet.IntVar(&options.ExpandK, "expand-k", 5, "expanded result count for boundary smoke checks")
	flagSet.IntVar(&options.QueryIndex, "query-index", 0, "zero-based fixture query index")
	flagSet.StringVar(&options.Metric, "metric", connectors.PGVectorMetricCosine, "pgvector metric: cosine or l2")
	if err := flagSet.Parse(args); err != nil {
		return searchPGVectorOptions{}, err
	}
	if options.FixturePath == "" {
		return searchPGVectorOptions{}, errors.New("fixture path is required")
	}
	if options.ConnectionURL == "" {
		return searchPGVectorOptions{}, errors.New("connection-url is required")
	}
	if options.TopK <= 0 {
		return searchPGVectorOptions{}, errors.New("top-k must be positive")
	}
	if options.ExpandK < options.TopK {
		return searchPGVectorOptions{}, errors.New("expand-k must be greater than or equal to top-k")
	}
	if options.QueryIndex < 0 {
		return searchPGVectorOptions{}, errors.New("query-index must be non-negative")
	}
	if options.Metric != connectors.PGVectorMetricCosine && options.Metric != connectors.PGVectorMetricL2 {
		return searchPGVectorOptions{}, fmt.Errorf("unsupported pgvector metric %q", options.Metric)
	}
	return options, nil
}

func newPGVectorSearchConnector(connectionURL string, config connectors.PGVectorConfig) (pgvectorSearchConnector, error) {
	config.ConnectionURL = connectionURL
	return connectors.NewPGVectorConnector(config, nil)
}
