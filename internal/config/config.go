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
//
// Config 是 vdb-guardian 验证作业的强类型根配置。
// 它汇集了企业级控制平面所需的字段，用于加载作业、连接源端和目标端向量数据库、
// 收集可比对的查询结果、运行指纹引擎以及渲染报告。
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
//
// JobConfig 包含验证作业的稳定元数据。
type JobConfig struct {
	// Name is the human-readable job name used in logs, reports, and artifacts.
	Name string `yaml:"name"`
}

// RuntimeConfig contains process-level execution settings for local and future
// service deployments.
//
// RuntimeConfig 包含用于本地执行以及未来服务化部署的进程级执行设置。
type RuntimeConfig struct {
	// ArtifactStore describes where intermediate fingerprints and reports are stored.
	ArtifactStore ArtifactStoreConfig `yaml:"artifact_store"`
}

// ArtifactStoreConfig selects the artifact store backend and its local path when
// applicable. The first scaffold supports validation for local and memory stores.
//
// ArtifactStoreConfig 用于选择产物存储后端，并在适用时指定其本地路径。
// 初始的脚手架支持针对本地 (local) 和内存 (memory) 存储的验证。
type ArtifactStoreConfig struct {
	// Type identifies the artifact store backend, such as local or memory.
	Type string `yaml:"type"`
	// Path identifies the local artifact directory when Type is local.
	Path string `yaml:"path"`
}

// ConnectorConfig contains normalized connector settings. Individual connector
// implementations can interpret Params while the core configuration loader keeps
// common fields typed and validated.
//
// ConnectorConfig 包含规范化的连接器设置。各个独立的连接器实现负责解析 Params，
// 而核心配置加载器则负责保持通用字段的强类型和合法性校验。
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
//
// QueryConfig 描述了用于收集可比对的源端和目标端检索行为的查询采样和检索边界。
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
//
// FilterConfig 描述了元数据过滤行为是否参与本次验证作业。
// 具体的过滤谓词 (predicates) 将随着连接器实现的丰富而引入。
type FilterConfig struct {
	// Enabled indicates whether metadata filter behavior should be sampled.
	Enabled bool `yaml:"enabled"`
}

// FingerprintConfig contains algorithm settings for retrieval behavior
// fingerprint construction and distance aggregation.
//
// FingerprintConfig 包含用于构建检索行为指纹以及聚合距离的算法设置。
type FingerprintConfig struct {
	// Boundary controls how topK boundary candidates are selected.
	Boundary BoundaryConfig `yaml:"boundary"`
	// Weights assigns non-negative weights to individual fingerprint distance components.
	Weights map[string]float64 `yaml:"weights"`
}

// BoundaryConfig configures boundary candidate extraction around the topK cutoff.
//
// BoundaryConfig 配置了围绕 TopK 截断点提取边界候选者 (boundary candidates) 的规则。
type BoundaryConfig struct {
	// RankBeforeK includes this many ranks before topK in the boundary observation window.
	RankBeforeK int `yaml:"rank_before_k"`
	// Delta is the maximum score difference from the K-th result for boundary candidates.
	Delta float64 `yaml:"delta"`
}

// ReportConfig selects report output formats generated after a verification job.
//
// ReportConfig 用于选择在验证作业完成后需要生成的报告输出格式。
type ReportConfig struct {
	// Formats lists report formats such as json and markdown.
	Formats []string `yaml:"formats"`
}

// LoadFile reads a YAML configuration file, decodes it into Config, and validates
// it before returning. The returned error includes the path so callers can report
// actionable diagnostics when operator-provided configuration fails.
//
// LoadFile 读取 YAML 配置文件，将其解码为 Config 结构体，并在返回前进行校验。
// 返回的错误信息中包含文件路径，以便在操作人员提供的配置失效时，调用方能够报告具有可操作性的诊断信息。
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
//
// LoadReader 从 reader 中解码 YAML 为 Config 结构体，并验证其结果。
// 它被测试、CLI 代码、API 处理器以及未来可能从文件、请求体或产物存储中加载配置的作业运行器所使用。
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
//
// Validate 校验验证作业运行前所需的语义规则。
// 它会在任何连接器建立数据库连接之前，拦截缺失的作业元数据、无效的查询边界、
// 非法的指纹权重、不支持的报告格式以及不支持的产物存储配置。
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
//
// Validate 校验产物存储设置，该设置决定了作业产物的写入位置。
// 在早期的本地脚手架中允许设置为空，稍后将由运行器解析为默认值。
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
//
// Validate 校验供源端和目标端搜索使用的查询边界。
// 这些规则确保扩展的搜索窗口至少能够观测到业务可见的 TopK 结果，并且作业能够采样到正数个查询。
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
//
// Validate 在将指纹算法设置发送至 Python 引擎前对其进行校验。
// 它确保边界参数为非负数，且加权距离分量能够产出具有实际意义的归一化得分。
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
//
// Validate 校验报告的输出格式。允许提供空的格式列表以便未来的运行器默认使用 JSON；
// 若显式指定了格式，则该格式必须受支持且在生成本地产物时是安全的。
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
