package config

import (
	"strings"
	"testing"
)

const validConfigYAML = `
job:
  name: unit-test-job
runtime:
  artifact_store:
    type: local
    path: ./artifacts
source:
  type: milvus
  address: localhost:19530
  collection: source_items
target:
  type: pgvector
  dsn: postgresql://postgres:[REDACTED]@localhost:5433/postgres
  table: target_items
query:
  top_k: 10
  expand_k: 20
  sample_size: 100
  filters:
    enabled: true
fingerprint:
  boundary:
    rank_before_k: 2
    delta: 0.03
  weights:
    stable_diff: 0.25
    boundary_flip: 0.40
    curve_diff: 0.20
    filter_diff: 0.15
report:
  formats:
    - json
    - markdown
`

func TestLoadReaderParsesValidConfig(t *testing.T) {
	cfg, err := LoadReader(strings.NewReader(validConfigYAML))
	if err != nil {
		t.Fatalf("expected valid config to load: %v", err)
	}

	if cfg.Job.Name != "unit-test-job" {
		t.Fatalf("expected job name unit-test-job, got %q", cfg.Job.Name)
	}
	if cfg.Source.Type != "milvus" {
		t.Fatalf("expected source type milvus, got %q", cfg.Source.Type)
	}
	if cfg.Target.Type != "pgvector" {
		t.Fatalf("expected target type pgvector, got %q", cfg.Target.Type)
	}
	if cfg.Query.TopK != 10 || cfg.Query.ExpandK != 20 {
		t.Fatalf("expected top_k=10 and expand_k=20, got top_k=%d expand_k=%d", cfg.Query.TopK, cfg.Query.ExpandK)
	}
	if cfg.Fingerprint.Weights["boundary_flip"] != 0.40 {
		t.Fatalf("expected boundary_flip weight 0.40, got %f", cfg.Fingerprint.Weights["boundary_flip"])
	}
}

func TestLoadFileReturnsErrorForMissingFile(t *testing.T) {
	_, err := LoadFile("/tmp/vdb-guardian-missing-config.yaml")
	if err == nil {
		t.Fatal("expected missing config file to return an error")
	}
	if !strings.Contains(err.Error(), "vdb-guardian-missing-config.yaml") {
		t.Fatalf("expected error to include config path, got %v", err)
	}
}

func TestConfigValidateRejectsMissingJobName(t *testing.T) {
	cfg := validConfig(t)
	cfg.Job.Name = ""

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "job.name") {
		t.Fatalf("expected missing job.name error, got %v", err)
	}
}

func TestConfigValidateRejectsInvalidQueryBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "top_k", mutate: func(cfg *Config) { cfg.Query.TopK = 0 }, want: "query.top_k"},
		{name: "expand_k", mutate: func(cfg *Config) { cfg.Query.ExpandK = 5 }, want: "query.expand_k"},
		{name: "sample_size", mutate: func(cfg *Config) { cfg.Query.SampleSize = 0 }, want: "query.sample_size"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %s validation error, got %v", tt.want, err)
			}
		})
	}
}

func TestConfigValidateRejectsInvalidFingerprintWeights(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "empty weights", mutate: func(cfg *Config) { cfg.Fingerprint.Weights = map[string]float64{} }, want: "fingerprint.weights"},
		{name: "negative weight", mutate: func(cfg *Config) { cfg.Fingerprint.Weights["stable_diff"] = -0.1 }, want: "fingerprint.weights.stable_diff"},
		{name: "zero total", mutate: func(cfg *Config) {
			cfg.Fingerprint.Weights = map[string]float64{"stable_diff": 0, "boundary_flip": 0}
		}, want: "fingerprint.weights"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %s validation error, got %v", tt.want, err)
			}
		})
	}
}

func TestConfigValidateRejectsUnsupportedReportFormat(t *testing.T) {
	cfg := validConfig(t)
	cfg.Report.Formats = []string{"json", "pdf"}

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "report.formats") {
		t.Fatalf("expected unsupported report format error, got %v", err)
	}
}

func TestConfigValidateAcceptsRepositoryExampleConfigs(t *testing.T) {
	paths := []string{
		"../../configs/local.yaml",
		"../../configs/milvus-to-pgvector.example.yaml",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			cfg, err := LoadFile(path)
			if err != nil {
				t.Fatalf("expected example config to load: %v", err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("expected example config to validate: %v", err)
			}
		})
	}
}

func validConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := LoadReader(strings.NewReader(validConfigYAML))
	if err != nil {
		t.Fatalf("expected valid config fixture to load: %v", err)
	}
	return cfg
}
