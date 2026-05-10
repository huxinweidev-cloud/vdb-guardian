package fixtures

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
)

const (
	// MetricCosine identifies cosine similarity fixtures. Generated vectors are
	// L2-normalized so Milvus and pgvector can use comparable cosine behavior.
	MetricCosine = "cosine"

	// MetricL2 identifies Euclidean distance fixtures. Generated vectors are left
	// unnormalized to exercise distance-based source and target connectors.
	MetricL2 = "l2"

	maxVectorDimension = 2000
)

// SyntheticOptions describes the deterministic synthetic dataset to generate.
//
// The options intentionally use the pgvector `vector` type limit as the first
// migration MVP boundary because Milvus to pgvector verification must fit the
// target database. The seed makes generated records and queries reproducible so
// integration runs can be compared across machines and commits.
type SyntheticOptions struct {
	Seed        int64  `json:"seed"`
	Dimension   int    `json:"dimension"`
	RecordCount int    `json:"record_count"`
	QueryCount  int    `json:"query_count"`
	Metric      string `json:"metric"`
}

// SyntheticDataset is the JSON fixture consumed by future database seeders and
// migration verification commands.
//
// Records are intended to be inserted into the source vector database, while
// Queries are used to collect comparable search results from source and target
// databases before fingerprint artifacts are built.
type SyntheticDataset struct {
	Seed        int64             `json:"seed"`
	Dimension   int               `json:"dimension"`
	RecordCount int               `json:"record_count"`
	QueryCount  int               `json:"query_count"`
	Metric      string            `json:"metric"`
	Records     []SyntheticVector `json:"records"`
	Queries     []SyntheticVector `json:"queries"`
}

// SyntheticVector stores a stable vector identifier and its dense vector values.
//
// IDs are generated with deterministic prefixes so later Milvus and pgvector
// seeders can map records and queries without relying on database-generated IDs.
type SyntheticVector struct {
	ID     string    `json:"id"`
	Vector []float64 `json:"vector"`
}

// GenerateSyntheticDataset creates deterministic record and query vectors for
// local Milvus to pgvector migration experiments.
//
// It validates the requested dimension, record count, query count, and metric.
// Cosine fixtures are normalized to unit length so source and target databases
// receive comparable vectors even if their score conventions differ.
func GenerateSyntheticDataset(options SyntheticOptions) (SyntheticDataset, error) {
	if err := validateSyntheticOptions(options); err != nil {
		return SyntheticDataset{}, err
	}

	rng := rand.New(rand.NewSource(options.Seed))
	records := make([]SyntheticVector, options.RecordCount)
	for index := range records {
		records[index] = SyntheticVector{
			ID:     fmt.Sprintf("vec-%06d", index+1),
			Vector: generateVector(rng, options.Dimension, options.Metric),
		}
	}

	queries := make([]SyntheticVector, options.QueryCount)
	for index := range queries {
		queries[index] = SyntheticVector{
			ID:     fmt.Sprintf("query-%06d", index+1),
			Vector: generateVector(rng, options.Dimension, options.Metric),
		}
	}

	return SyntheticDataset{
		Seed:        options.Seed,
		Dimension:   options.Dimension,
		RecordCount: options.RecordCount,
		QueryCount:  options.QueryCount,
		Metric:      options.Metric,
		Records:     records,
		Queries:     queries,
	}, nil
}

// WriteSyntheticDataset writes a synthetic dataset as indented JSON.
//
// The parent directory is created automatically so CLI commands can write
// fixtures into `testdata`, `/tmp`, or later artifact directories using the same
// helper.
func WriteSyntheticDataset(path string, dataset SyntheticDataset) error {
	if path == "" {
		return errors.New("synthetic dataset path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create synthetic dataset directory: %w", err)
	}
	content, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		return fmt.Errorf("encode synthetic dataset: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write synthetic dataset: %w", err)
	}
	return nil
}

func validateSyntheticOptions(options SyntheticOptions) error {
	if options.Dimension < 1 {
		return errors.New("synthetic dimension must be at least 1")
	}
	if options.Dimension > maxVectorDimension {
		return fmt.Errorf("synthetic dimension must be <= %d for pgvector vector compatibility", maxVectorDimension)
	}
	if options.RecordCount < 1 {
		return errors.New("synthetic record_count must be at least 1")
	}
	if options.QueryCount < 1 {
		return errors.New("synthetic query_count must be at least 1")
	}
	if options.Metric != MetricCosine && options.Metric != MetricL2 {
		return fmt.Errorf("unsupported synthetic metric %q", options.Metric)
	}
	return nil
}

func generateVector(rng *rand.Rand, dimension int, metric string) []float64 {
	vector := make([]float64, dimension)
	var normSquared float64
	for index := range vector {
		value := rng.Float64()*2 - 1
		vector[index] = value
		normSquared += value * value
	}
	if metric == MetricCosine {
		normalizeVector(vector, normSquared)
	}
	return vector
}

func normalizeVector(vector []float64, normSquared float64) {
	if normSquared == 0 {
		vector[0] = 1
		return
	}
	norm := math.Sqrt(normSquared)
	for index := range vector {
		vector[index] = vector[index] / norm
	}
}
