# 配置规范 (Configuration Specification)

`vdb-guardian` 采用 YAML 配置文件来描述验证作业。Go 控制平面负责将这些文件加载为强类型的结构体 (typed structs)，并在任何连接器开启数据库连接之前对其进行合法性校验。

## 支持的示例 (Supported examples)

示例文件存放在 `configs/` 目录下：

- `configs/local.yaml`
- `configs/milvus-to-pgvector.example.yaml`

示例文件中严禁包含真实的凭据信息。敏感字段必须使用 `[REDACTED]` 进行脱敏处理。

## 根字段 (Root fields)

```yaml
job:
  name: milvus-to-pgvector-demo

runtime:
  artifact_store:
    type: local
    path: ./artifacts

source:
  type: milvus
  address: localhost:19530
  collection: patent_demo

target:
  type: pgvector
  dsn: postgresql://postgres:[REDACTED]@localhost:5433/postgres
  table: items

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
```

## 校验规则 (Validation rules)

### 作业 (Job)

- `job.name` 不能为空。

### 运行时 (Runtime)

- `runtime.artifact_store.type` 可以为空、`local` 或 `memory`。
- 当存储类型为 `local` 时，需提供 `runtime.artifact_store.path`。

### 数据源与目标库 (Source and target)

- `source.type` 不能为空。
- `target.type` 不能为空。

在配置层面上，特定连接器的字段校验刻意保持了较高的宽容度。具体的连接器实现将为 Milvus、pgvector 以及未来的后端提供更深层次的校验逻辑。

### 查询 (Query)

- `query.top_k` 必须大于零。
- `query.expand_k` 必须大于或等于 `query.top_k`。
- `query.sample_size` 必须大于零。

### 指纹 (Fingerprint)

- `fingerprint.boundary.rank_before_k` 不能为负数。
- `fingerprint.boundary.delta` 不能为负数。
- `fingerprint.weights` 不能为空。
- 每个指纹权重必须大于或等于零。
- 指纹权重总和必须大于零。

### 报告 (Report)

- 允许 `report.formats` 为空，以便未来的运行器可应用默认格式。
- 目前显式支持的格式包括 `json` 和 `markdown`。

## 安全要求 (Security requirements)

切勿提交真实的令牌 (tokens)、密码、私钥、云凭据或生产环境的数据库连接字符串。在示例及文档中，请统一使用 `[REDACTED]`。

## Go API

强类型配置加载器位于 `internal/config` 包中。

```go
cfg, err := config.LoadFile("configs/milvus-to-pgvector.example.yaml")
```

该包对外暴露以下接口：

- `LoadFile(path string) (Config, error)`
- `LoadReader(reader io.Reader) (Config, error)`
- `Config.Validate() error`

`LoadFile` 与 `LoadReader` 在返回配置对象前，均会自动执行配置校验。