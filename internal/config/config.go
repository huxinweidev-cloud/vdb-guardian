package config

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the typed root configuration for a vdb-guardian verification job.
// It groups the enterprise control-plane fields needed to load a job, connect to
// source and target vector databases, collect comparable query results, run the
// fingerprint engine, and render reports.
type Config struct {
	// Job contains human-readable job metadata.
	Job JobConfig `yaml:"job"`
	// Runtime contains local execution settings such as artifact storage.
	Runtime RuntimeConfig `yaml:"runtime"`
	// Source describes the source vector database connector.
	Source ConnectorConfig `yaml:"source"`
	// Target describes the target vector database connector.
	Target ConnectorConfig `yaml:"target"`
	// Query describes topK, expanded search, sampling, and filter behavior.
	Query QueryConfig `yaml:"query"`
	// Fingerprint describes boundary-candidate and weighted distance settings.
	Fingerprint FingerprintConfig `yaml:"fingerprint"`
	// Report describes desired report output formats.
	Report ReportConfig `yaml:"report"`
}

// JobConfig contains stable metadata for a verification job.
type JobConfig struct {
	// Name is the human-readable job name used in logs, reports, and artifacts.
	Name string `yaml:"name"`
}

// RuntimeConfig contains process-level execution settings for local and future
// service deployments.
type RuntimeConfig struct {
	// ArtifactStore describes where intermediate fingerprints and reports are stored.
	ArtifactStore ArtifactStoreConfig `yaml:"artifact_store"`
}

// ArtifactStoreConfig selects the artifact store backend and its local path when
// applicable. The first scaffold supports validation for local and memory stores.
type ArtifactStoreConfig struct {
	// Type identifies the artifact store backend, such as local or memory.
	Type string `yaml:"type"`
	// Path identifies the local artifact directory when Type is local.
	Path string `yaml:"path"`
}

// ConnectorConfig contains normalized connector settings. Individual connector
// implementations can interpret Params while the core configuration loader keeps
// common fields typed and validated.
type ConnectorConfig struct {
	// Type identifies the connector implementation, such as milvus or pgvector.
	Type string `yaml:"type"`
	// Address contains host and port style endpoints used by connectors such as Milvus.
	Address string `yaml:"address,omitempty"`
	// Collection identifies a source or target collection when the connector uses collections.
	Collection string `yaml:"collection,omitempty"`
	// DSN contains a database connection string. It must not be logged with secrets exposed.
	DSN string `yaml:"dsn,omitempty"`
	// Table identifies a relational table for connectors such as pgvector.
	Table string `yaml:"table,omitempty"`
	// Params contains opt-in connector-specific settings that are not part of the shared schema.
	Params map[string]string `yaml:"params,omitempty"`
}

// QueryConfig describes query sampling and retrieval boundaries used to collect
// comparable source and target search behavior.
type QueryConfig struct {
	// TopK is the business-visible result cutoff used for ordinary retrieval comparison.
	TopK int `yaml:"top_k"`
	// ExpandK is the larger search window used to observe topK boundary candidates.
	ExpandK int `yaml:"expand_k"`
	// SampleSize is the number of verification queries to collect or generate.
	SampleSize int `yaml:"sample_size"`
	// Filters controls whether metadata-filter behavior should be included.
	Filters FilterConfig `yaml:"filters"`
}

// FilterConfig describes whether metadata filter behavior participates in the
// verification job. Detailed filter predicates will be introduced with connector
// implementations.
type FilterConfig struct {
	// Enabled indicates whether metadata filter behavior should be sampled.
	Enabled bool `yaml:"enabled"`
}

// FingerprintConfig contains algorithm settings for retrieval behavior
// fingerprint construction and distance aggregation.
type FingerprintConfig struct {
	// Boundary controls how topK boundary candidates are selected.
	Boundary BoundaryConfig `yaml:"boundary"`
	// Weights assigns non-negative weights to individual fingerprint distance components.
	Weights map[string]float64 `yaml:"weights"`
}

// BoundaryConfig configures boundary candidate extraction around the topK cutoff.
type BoundaryConfig struct {
	// RankBeforeK includes this many ranks before topK in the boundary observation window.
	RankBeforeK int `yaml:"rank_before_k"`
	// Delta is the maximum score difference from the K-th result for boundary candidates.
	Delta float64 `yaml:"delta"`
}

// ReportConfig selects report output formats generated after a verification job.
type ReportConfig struct {
	// Formats lists report formats such as json and markdown.
	Formats []string `yaml:"formats"`
}

// LoadFile reads a YAML configuration file, decodes it into Config, and validates
// it before returning. The returned error includes the path so callers can report
// actionable diagnostics when operator-provided configuration fails.
func LoadFile(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer file.Close()

	cfg, err := LoadReader(file)
	if err != nil {
		return Config{}, fmt.Errorf("load config file %q: %w", path, err)
	}
	return cfg, nil
}

// LoadReader decodes YAML from reader into Config and validates the result. It is
// used by tests, CLI code, API handlers, and future job runners that may load
// configuration from files, request bodies, or artifact storage.
func LoadReader(reader io.Reader) (Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config yaml: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks the semantic rules required before a verification job can run.
// It catches missing job metadata, invalid query bounds, invalid fingerprint
// weights, unsupported report formats, and unsupported artifact store values
// before any connector opens a database connection.
func (c Config) Validate() error {
	if c.Job.Name == "" {
		return fmt.Errorf("job.name must not be empty")
	}
	if c.Source.Type == "" {
		return fmt.Errorf("source.type must not be empty")
	}
	if c.Target.Type == "" {
		return fmt.Errorf("target.type must not be empty")
	}
	if err := c.Runtime.ArtifactStore.Validate(); err != nil {
		return err
	}
	if err := c.Query.Validate(); err != nil {
		return err
	}
	if err := c.Fingerprint.Validate(); err != nil {
		return err
	}
	if err := c.Report.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate checks artifact store settings that affect where job artifacts will be
// written. Empty settings are allowed in early local scaffolding and can later be
// resolved to defaults by the runner.
func (c ArtifactStoreConfig) Validate() error {
	switch c.Type {
	case "", "local", "memory":
		return nil
	default:
		return fmt.Errorf("runtime.artifact_store.type %q is not supported", c.Type)
	}
}

// Validate checks query bounds used by source and target searches. These rules
// ensure the expanded search window can observe at least the visible topK results
// and that the job samples a positive number of queries.
func (c QueryConfig) Validate() error {
	if c.TopK <= 0 {
		return fmt.Errorf("query.top_k must be greater than zero")
	}
	if c.ExpandK < c.TopK {
		return fmt.Errorf("query.expand_k must be greater than or equal to query.top_k")
	}
	if c.SampleSize <= 0 {
		return fmt.Errorf("query.sample_size must be greater than zero")
	}
	return nil
}

// Validate checks fingerprint algorithm settings before they are sent to the
// Python engine. It ensures boundary parameters are non-negative and weighted
// distance components can produce a meaningful normalized score.
func (c FingerprintConfig) Validate() error {
	if c.Boundary.RankBeforeK < 0 {
		return fmt.Errorf("fingerprint.boundary.rank_before_k must not be negative")
	}
	if c.Boundary.Delta < 0 {
		return fmt.Errorf("fingerprint.boundary.delta must not be negative")
	}
	if len(c.Weights) == 0 {
		return fmt.Errorf("fingerprint.weights must not be empty")
	}

	totalWeight := 0.0
	for name, weight := range c.Weights {
		if weight < 0 {
			return fmt.Errorf("fingerprint.weights.%s must not be negative", name)
		}
		totalWeight += weight
	}
	if totalWeight <= 0 {
		return fmt.Errorf("fingerprint.weights total must be greater than zero")
	}
	return nil
}

// Validate checks report output formats. Empty formats are allowed so future
// runners can default to JSON, while explicit formats must be supported and safe
// for local artifact generation.
func (c ReportConfig) Validate() error {
	for _, format := range c.Formats {
		switch format {
		case "json", "markdown":
			continue
		default:
			return fmt.Errorf("report.formats contains unsupported format %q", format)
		}
	}
	return nil
}
