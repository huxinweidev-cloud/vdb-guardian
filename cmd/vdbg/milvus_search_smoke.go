package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/connectors"
)

type searchMilvusOptions struct {
	FixturePath string
	Address     string
	Collection  string
	IDField     string
	VectorField string
	TopK        int
	ExpandK     int
	QueryIndex  int
	Metric      string
}

type milvusSearchConnector interface {
	Connect(ctx context.Context) error
	Count(ctx context.Context, collection string) (int64, error)
	Search(ctx context.Context, req connectors.SearchRequest) (connectors.SearchResponse, error)
	Close() error
}

// runSearchMilvusCommand runs a real Milvus connector search against one query
// from a synthetic fixture.
//
// It is a smoke command for the source-side migration MVP. The command validates
// Milvus connectivity, counts seeded records, executes one normalized search,
// and prints ranked hits without starting Docker or writing data.
//
// runSearchMilvusCommand 使用真实的 Milvus 连接器，针对合成固件中的单条查询执行检索。
//
// 这是用于源端迁移 MVP 的冒烟测试命令。该命令用于验证 Milvus 连通性、统计已灌入的记录数、
// 执行一次规范化的向量检索，并打印出带排名的命中结果。它绝对不会启动 Docker，
// 也绝对不会写入任何数据。
func runSearchMilvusCommand(ctx context.Context, args []string) error {
	return runSearchMilvusWithFactory(ctx, args, newMilvusSearchConnector)
}

func runSearchMilvusWithFactory(ctx context.Context, args []string, factory func(string, connectors.MilvusConfig) (milvusSearchConnector, error)) error {
	options, err := parseSearchMilvusOptions(args)
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
	fmt.Printf("milvus search smoke ok\n")
	fmt.Printf("fixture: %s\n", options.FixturePath)
	fmt.Printf("collection: %s\n", options.Collection)
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

func parseSearchMilvusOptions(args []string) (searchMilvusOptions, error) {
	flagSet := flag.NewFlagSet("search-milvus", flag.ContinueOnError)
	flagSet.SetOutput(discardFlagOutput{})

	var options searchMilvusOptions
	flagSet.StringVar(&options.FixturePath, "fixture", "", "path to a synthetic fixture JSON file")
	flagSet.StringVar(&options.Address, "address", "", "Milvus gRPC address for search")
	flagSet.StringVar(&options.Collection, "collection", "items", "Milvus collection to search")
	flagSet.StringVar(&options.IDField, "id-field", "id", "Milvus primary key field returned in search results")
	flagSet.StringVar(&options.VectorField, "vector-field", "embedding", "Milvus float vector field name")
	flagSet.IntVar(&options.TopK, "top-k", 3, "business-visible topK result count")
	flagSet.IntVar(&options.ExpandK, "expand-k", 5, "expanded result count for boundary smoke checks")
	flagSet.IntVar(&options.QueryIndex, "query-index", 0, "zero-based fixture query index")
	flagSet.StringVar(&options.Metric, "metric", connectors.MilvusMetricCosine, "Milvus metric: cosine or l2")
	if err := flagSet.Parse(args); err != nil {
		return searchMilvusOptions{}, err
	}
	if options.FixturePath == "" {
		return searchMilvusOptions{}, errors.New("fixture path is required")
	}
	if options.Address == "" {
		return searchMilvusOptions{}, errors.New("address is required")
	}
	if options.TopK <= 0 {
		return searchMilvusOptions{}, errors.New("top-k must be positive")
	}
	if options.ExpandK < options.TopK {
		return searchMilvusOptions{}, errors.New("expand-k must be greater than or equal to top-k")
	}
	if options.QueryIndex < 0 {
		return searchMilvusOptions{}, errors.New("query-index must be non-negative")
	}
	if options.Metric != connectors.MilvusMetricCosine && options.Metric != connectors.MilvusMetricL2 {
		return searchMilvusOptions{}, fmt.Errorf("unsupported Milvus metric %q", options.Metric)
	}
	return options, nil
}

func newMilvusSearchConnector(address string, config connectors.MilvusConfig) (milvusSearchConnector, error) {
	config.Address = address
	return connectors.NewMilvusConnector(config, nil)
}
