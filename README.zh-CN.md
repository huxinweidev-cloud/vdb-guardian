# vdb-guardian

[![CI](https://github.com/h3xwave/vdb-guardian/workflows/CI/badge.svg)](https://github.com/h3xwave/vdb-guardian/actions)
[![codecov](https://codecov.io/gh/h3xwave/vdb-guardian/branch/main/graph/badge.svg)](https://codecov.io/gh/h3xwave/vdb-guardian)
[![Go Report Card](https://goreportcard.com/badge/github.com/h3xwave/vdb-guardian)](https://goreportcard.com/report/github.com/h3xwave/vdb-guardian)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

`vdb-guardian` 是一个面向企业级部署的异构向量数据库迁移一致性验证平台。

它关注的不是“数据有没有搬过去”或“目标库跑得快不快”，而是：迁移后，目标向量数据库是否保持了源向量数据库的检索行为。

项目核心方法是“检索行为指纹”：通过稳定近邻集合、边界候选集合、边界候选翻转率、指纹距离和一致性评分，量化 Milvus、pgvector 等异构向量数据库之间的迁移一致性。

English: [README.md](README.md)

## 项目定位

`vdb-guardian` 旨在构建一个可长期演进的企业级工具，用于：

- 验证异构向量数据库迁移后的检索行为一致性；
- 发现普通 topK 重合率、数据计数或 benchmark 难以发现的边界结果漂移；
- 为迁移验收、灰度验证、审计报告和专利实验提供可复现指标；
- 支持从 Milvus 到 pgvector 的第一阶段验证，并为 Qdrant、Weaviate、Elastic/OpenSearch、Pinecone 等连接器预留扩展能力。

本项目强调：

```text
验证的是迁移后的检索行为层一致性，而不仅是数据完整性或性能指标。
```

## 架构概览

第一阶段采用 Go + Python monorepo 架构：

```text
Go 控制面 / CLI / API-ready / Job / Connector / Artifact Store
        |
        | 稳定 JSON / artifact 协议
        v
Python 检索行为指纹算法引擎
```

职责划分：

- Go 负责企业级控制面：CLI、服务入口、任务状态、连接器接口、引擎接口、artifact 存储抽象。
- Python 负责算法引擎：边界候选集合、集合距离、边界翻转率、加权指纹距离和后续一致性评分。

这种设计让项目从第一天就具备企业级边界，同时避免过早引入复杂微服务。

## 当前已实现能力

当前分支已经包含第一版企业级骨架：

- Go module；
- CLI 入口：`cmd/vdbg`；
- 服务端入口骨架：`cmd/vdb-guardian-server`；
- Job 状态模型：`internal/jobs`；
- 本地 verification job runner：`internal/jobs`；
- 类型化 YAML 任务配置加载与校验：`internal/config`；
- 向量数据库连接器接口：`internal/connectors`；
- memory connector：`internal/connectors`；
- 最小 Milvus connector 与真实 Go SDK adapter：`internal/connectors`；
- 最小 pgvector connector：`internal/connectors`；
- 本地 offline verification pipeline：`internal/pipeline`；
- offline-verify fixture CLI 命令；
- 本地 Milvus / pgvector migration Docker Compose 环境；
- Milvus 合成 fixture 写入器；
- `vdbg seed-milvus` 真实 Milvus fixture 写入 CLI；
- `vdbg search-milvus` 真实 Milvus 检索冒烟 CLI；
- `vdbg build-milvus-artifact` 真实 Milvus 指纹 artifact CLI；
- pgvector 合成 fixture 写入器；
- `vdbg seed-pgvector` 真实 pgvector fixture 写入 CLI；
- `vdbg search-pgvector` 真实 pgvector 检索冒烟 CLI；
- `vdbg build-pgvector-artifact` 真实 pgvector 指纹 artifact CLI；
- `vdbg compare-artifacts` 真实源/目标指纹 artifact 对比 CLI；
- 合成向量数据生成器；
- 指纹 artifact builder：`internal/fingerprints`；
- 指纹引擎接口：`internal/engine`；
- Python 子进程引擎 Runner；
- Go / Python JSON 引擎协议；
- 内存 Artifact Store：`internal/artifacts`；
- Python 指纹算法包：`python/vdb_fingerprint_engine`；
- Artifact-backed 指纹对比；
- 边界候选集合选择；
- Jaccard distance；
- boundary flip rate；
- weighted fingerprint distance；
- Go / Python 单元测试；
- `Makefile` 质量门禁；
- 架构文档和配置示例。

## 暂未实现能力

以下能力在 roadmap 中，当前还不是已完成功能：

- Milvus seed CLI 针对本地 migration stack 的集成测试；
- pgvector seed CLI 针对本地 migration stack 的集成测试；
- 真实迁移与对比 CLI；
- HTTP API 路由；
- 持久化 Job Store；
- 完整 Markdown / JSON 报告生成；
- Kubernetes / Helm 部署；
- Web UI。

## 快速开始

### 1. 克隆仓库

```bash
git clone git@github.com:huxinweidev-cloud/vdb-guardian.git
cd vdb-guardian
```

### 2. 运行完整质量门禁

```bash
make fmt
make lint
make test
```

### 3. Go CLI 冒烟检查

```bash
go run ./cmd/vdbg --version
```

预期输出类似：

```text
vdb-guardian dev
```

### 4. Go 服务入口冒烟检查

```bash
go run ./cmd/vdb-guardian-server
```

预期输出类似：

```text
vdb-guardian server scaffold dev
```

### 5. Offline Verify fixture 冒烟检查

```bash
go run ./cmd/vdbg offline-verify \
  --fixture testdata/offline/basic.json \
  --artifact-dir /tmp/vdb-guardian-offline
```

该命令不会连接真实数据库，只会基于 fixture 跑通 memory connector、fingerprint artifact builder、Python engine 和 result artifact 写出流程。

### 6. Python 引擎检查

```bash
cd python
uv sync
uv run pytest
uv run python -m vdb_fingerprint_engine.cli --version
```

### 7. Python 引擎协议冒烟检查

```bash
cd python
printf '{"fingerprints":[{"query_id":"q-1","stable_neighbors":["a","b","c"],"boundary_candidates":["d","e"],"top_k_ids":["a","b","c","d"]}]}' > /tmp/vdb-source-fingerprint.json
printf '{"fingerprints":[{"query_id":"q-1","stable_neighbors":["a","b","x"],"boundary_candidates":["d","f"],"top_k_ids":["a","b","x","f"]}]}' > /tmp/vdb-target-fingerprint.json
printf '{"job_id":"job-1","source_fingerprint_path":"/tmp/vdb-source-fingerprint.json","target_fingerprint_path":"/tmp/vdb-target-fingerprint.json"}' > /tmp/vdb-engine-input.json
uv run python -m vdb_fingerprint_engine.cli compare --input /tmp/vdb-engine-input.json --output /tmp/vdb-engine-output.json
cat /tmp/vdb-engine-output.json
```

完整 artifact 格式见：

```text
docs/fingerprint-artifact-format.md
```

### 8. Memory Connector

Go memory connector 位于：

```text
internal/connectors
```

它使用预置 ranked hits 返回标准化 `SearchResponse`，用于无数据库本地验证。

详细说明见：

```text
docs/memory-connector.md
```

### 9. Milvus Connector

最小 Milvus connector 位于：

```text
internal/connectors
```

它会校验 Milvus 配置，通过真实 Milvus Go SDK adapter 执行连接、collection 统计和向量检索，并把 SDK 调用隔离在 adapter 边界后面，供后续迁移 MVP 接入。

详细说明见：

```text
docs/milvus-connector.md
```

### 10. pgvector Connector

最小 pgvector connector 位于：

```text
internal/connectors
```

它会校验 pgvector 配置、检查 `vector` extension、统计表记录数，并通过 PostgreSQL + pgx 执行 cosine / L2 向量检索，返回标准化 `SearchResponse`。

详细说明见：

```text
docs/pgvector-connector.md
```

### 11. 指纹 Artifact Builder

Go 指纹 artifact builder 位于：

```text
internal/fingerprints
```

它接收标准化 search results，并生成 Python 引擎可直接消费的 fingerprint artifact：

```text
search results -> source-fingerprint.json / target-fingerprint.json
```

详细说明见：

```text
docs/fingerprint-artifact-builder.md
```

### 12. 本地 Verification Runner

Go 本地任务 runner 位于：

```text
internal/jobs
```

它接收 source / target fingerprint artifact 路径，调用 `engine.Engine`，并写出：

```text
<artifact-dir>/<job-id>-result.json
```

详细说明见：

```text
docs/local-verification-runner.md
```

### 13. 本地 Offline Pipeline

Go 本地 offline pipeline 位于：

```text
internal/pipeline
```

它将 source / target connector、fingerprint artifact builder、verification runner 串成无数据库端到端验证链路。

详细说明见：

```text
docs/local-offline-pipeline.md
```

### 14. Offline Verify Fixture CLI

`vdbg offline-verify` 命令可以从 JSON fixture 跑通无数据库本地验证链路，并写出 source/target fingerprint artifact 与 result artifact。

详细说明见：

```text
docs/offline-verify-fixture.md
```

### 15. 本地 Migration Stack

本地 migration stack 定义了 Milvus standalone 与 PostgreSQL pgvector 服务，用于后续基础迁移对比 MVP。

只校验 Compose 文件、不启动容器：

```bash
make migration-stack-config
```

启动容器、网络和卷：

```bash
make migration-stack-up
```

详细说明见：

```text
docs/local-migration-stack.md
```

### 16. Milvus Fixture 写入器

Milvus fixture 写入器位于：

```text
internal/migration
```

它负责准备最小 collection 边界，并通过 adapter 插入合成 records。`vdbg seed-milvus` 已将该写入器接到真实 Milvus Go SDK 连接：

```bash
go run ./cmd/vdbg seed-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --metric cosine
```

详细说明见：

```text
docs/milvus-fixture-seeding.md
docs/seed-milvus-cli.md
```

### 17. Milvus 检索冒烟 CLI

`vdbg search-milvus` 会复用真实 Milvus connector，对已写入的 collection 执行记录数统计和单个 fixture query 的向量检索：

```bash
go run ./cmd/vdbg search-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

详细说明见：

```text
docs/search-milvus-cli.md
```

### 18. Milvus 指纹 Artifact CLI

`vdbg build-milvus-artifact` 会复用真实 Milvus connector，对 fixture 中每个 query 执行向量检索，并写出 Python 引擎可消费的 source fingerprint artifact：

```bash
go run ./cmd/vdbg build-milvus-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --output /tmp/vdb-guardian-source-fingerprint.json \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

详细说明见：

```text
docs/build-milvus-artifact-cli.md
```

### 19. pgvector Fixture 写入器与真实写入 CLI

pgvector fixture 写入器位于：

```text
internal/migration
```

它负责创建 pgvector extension/table，并通过 adapter upsert 合成 records。`vdbg seed-pgvector` 已将该写入器接到真实 pgx PostgreSQL 连接：

```bash
go run ./cmd/vdbg seed-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --id-column id \
  --vector-column embedding
```

详细说明见：

```text
docs/pgvector-fixture-seeding.md
docs/seed-pgvector-cli.md
```

### 20. pgvector 检索冒烟 CLI

`vdbg search-pgvector` 会复用真实 pgvector connector，对已写入的表执行记录数统计和单个 fixture query 的向量检索：

```bash
go run ./cmd/vdbg search-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

详细说明见：

```text
docs/search-pgvector-cli.md
```

### 21. pgvector 指纹 Artifact CLI

`vdbg build-pgvector-artifact` 会复用真实 pgvector connector，对 fixture 中每个 query 执行向量检索，并写出 Python 引擎可消费的 target fingerprint artifact：

```bash
go run ./cmd/vdbg build-pgvector-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --output /tmp/vdb-guardian-target-fingerprint.json \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

详细说明见：

```text
docs/build-pgvector-artifact-cli.md
```

### 22. 真实源/目标 Artifact 对比 CLI

`vdbg compare-artifacts` 会读取已有的 source / target fingerprint artifact，调用 Python 指纹引擎，并写出标准化 result artifact：

```bash
go run ./cmd/vdbg compare-artifacts \
  --source /tmp/vdb-guardian-source-fingerprint.json \
  --target /tmp/vdb-guardian-target-fingerprint.json \
  --artifact-dir /tmp/vdb-guardian-compare \
  --job-id real-artifact-smoke
```

详细说明见：

```text
docs/compare-artifacts-cli.md
```

### 23. 合成向量 Fixture

`vdbg generate-synthetic-fixture` 命令可以生成固定 seed 的 records 与 query vectors，供后续 Milvus 写入、pgvector 写入和迁移对比使用。

```bash
go run ./cmd/vdbg generate-synthetic-fixture \
  --output testdata/migration/synthetic-small.json \
  --seed 42 \
  --dimension 8 \
  --records 100 \
  --queries 10 \
  --metric cosine
```

第一版支持维度范围：

```text
1..2000
```

详细说明见：

```text
docs/synthetic-vector-fixtures.md
```

## 本地开发要求

开发前必须阅读：

```text
CLAUDE.md
```

核心要求：

- 严格 TDD：先写失败测试，再写实现；
- 每个 public Go 类型、函数、方法必须写 Go doc 注释；
- 每个 public Python class/function/method 必须写 docstring；
- 每个新增方法都必须有单元测试；
- 提交前必须运行格式化、lint 和测试；
- README、docs、配置示例必须随功能同步更新；
- 不得提交真实 token、密码、私钥、数据库连接串或业务数据。

## 常用命令

仓库根目录：

```bash
make fmt       # 格式化 Go 和 Python 代码
make lint      # 运行 go vet 和 ruff check
make test      # 运行 Go 和 Python 单元测试
make migration-stack-config  # 校验本地 migration Docker Compose 配置，不启动容器
```

Go：

```bash
make test-go
make lint-go
go test ./...
```

Python：

```bash
cd python
uv sync
uv run pytest
uv run ruff format .
uv run ruff check .
```

## Python 依赖管理

本项目使用 `uv` 作为 Python 子项目的标准依赖管理工具。

推荐：

```bash
cd python
uv sync
uv run pytest
uv run ruff check .
```

不推荐直接使用全局：

```bash
pip install ...
```

`pip` 仅作为系统兼容、故障排查或安装底层工具时的备用手段，不作为项目依赖的标准安装入口。

## 目录结构

```text
vdb-guardian/
├── cmd/
│   ├── vdbg/
│   └── vdb-guardian-server/
├── internal/
│   ├── artifacts/
│   ├── config/
│   ├── connectors/
│   ├── engine/
│   ├── fingerprints/
│   ├── jobs/
│   ├── pipeline/
│   └── version/
├── python/
│   ├── pyproject.toml
│   ├── uv.lock
│   ├── tests/
│   └── vdb_fingerprint_engine/
├── configs/
├── docs/
├── CLAUDE.md
├── Makefile
└── README.md
```

## 核心概念

### 检索行为指纹

检索行为指纹用于描述一个向量数据库面对一组查询时表现出的检索行为，包括稳定近邻集合、边界候选集合、相似度衰减特征、过滤条件影响和 topK 变化等。

### 边界候选集合

边界候选集合指位于 topK 检索结果边界附近，并且与第 K 位结果相似度差值小于阈值的候选向量集合。

这些候选项对迁移一致性非常敏感，因为它们最容易因索引实现、距离计算、量化、过滤语义或排序规则差异而进入或退出 topK。

### 边界候选翻转率

边界候选翻转率用于衡量边界候选在源库和目标库中进入或退出 topK 的比例。

它可以发现普通 topK 重合率较高时仍然存在的边界漂移问题。

### 指纹距离

指纹距离用于综合多个检索行为差异指标，输出一个归一化距离。距离越低，说明源库和目标库的检索行为越接近。

## 配置示例

示例配置位于：

```text
configs/local.yaml
configs/milvus-to-pgvector.example.yaml
```

示例配置会通过 `internal/config` 中的类型化配置加载器进行解析和校验。当前校验内容包括：

- `job.name` 必填；
- `source.type` 和 `target.type` 必填；
- `query.top_k` 必须大于 0；
- `query.expand_k` 必须大于或等于 `query.top_k`；
- `query.sample_size` 必须大于 0；
- 指纹权重不能为空，且总权重大于 0；
- 显式报告格式当前支持 `json` 和 `markdown`。

完整配置说明见：

```text
docs/config-spec.md
```

示例配置不得包含真实凭据。敏感值必须使用：

```text
[REDACTED]
```

## 开发工作流

推荐分支命名：

```text
feat/<description>
fix/<description>
docs/<description>
test/<description>
chore/<description>
```

提交前必须执行：

```bash
make fmt
make lint
make test
git diff --check
```

提交信息采用 Conventional Commits，例如：

```text
feat(engine): add boundary candidate metrics

- Add boundary candidate selection based on topK expanded results
- Add boundary flip rate calculation
- Add unit tests for empty, identical, and partially overlapping sets
```

## Roadmap

### Phase 1：企业级骨架

- [x] Go + Python monorepo；
- [x] CLI / server 入口；
- [x] Connector / Engine / Artifact Store 接口；
- [x] Python 指纹算法最小实现；
- [x] 单元测试和质量门禁；
- [x] 架构文档。

### Phase 2：配置和本地任务执行

- [x] 类型化 Job 配置加载；
- [x] 配置校验；
- [x] 本地 artifact store；
- [x] Python subprocess engine runner；
- [x] JSON 输入输出协议；
- [x] 本地 verification job runner。

### Phase 3：Milvus 到 pgvector 实验链路

- [x] Artifact-backed 指纹对比；
- [x] Search results 到 fingerprint artifact 构建；
- [x] Memory connector 本地验证；
- [x] 本地 offline verification pipeline；
- [x] offline-verify fixture CLI；
- [x] Milvus connector；
- [x] pgvector connector；
- [x] Milvus fixture 写入器；
- [x] pgvector fixture 写入器；
- [x] 合成数据生成；
- [ ] 检索结果采集；
- [ ] 指纹距离报告；
- [x] Docker Compose 本地实验环境；
- [ ] 基础迁移对比 CLI。

### Phase 4：企业级部署能力

- [ ] API server；
- [ ] 持久化 job store；
- [ ] Prometheus metrics；
- [ ] structured logging；
- [ ] Docker 镜像；
- [ ] Kubernetes / Helm。

## 安全说明

- 不要提交真实数据库连接串；
- 不要提交 GitHub token、云服务密钥或私钥；
- 不要上传真实业务数据；
- 默认使用合成数据进行测试；
- Docker 操作必须先说明容器、端口、volume、network 和清理方式。

## 许可证

请查看仓库中的 `LICENSE` 文件。
