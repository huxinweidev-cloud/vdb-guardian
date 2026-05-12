# migrate-and-verify CLI 命令

`vdbg migrate-and-verify` 命令用于执行首个单次 (one-shot) 本地 Milvus 到 pgvector 的“迁移-一致性验证”闭环流程。

它组合了现有的、经过充分测试的各个命令与模块边界：

```text
将数据从 Milvus 迁移到 pgvector
构建 Milvus 源端指纹产物
构建 pgvector 目标端指纹产物
通过 Python 引擎对产物进行验证比对
```

该命令假设源和目标数据库均已在运行并可连通。它不会主动启动 Docker 或配置周边服务。

## 命令示例

```bash
go run ./cmd/vdbg migrate-and-verify \
  --fixture testdata/migration/synthetic-small.json \
  --milvus-address localhost:19530 \
  --source-collection items \
  --milvus-id-field id \
  --milvus-vector-field embedding \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --pgvector-id-column id \
  --pgvector-vector-column embedding \
  --artifact-dir /tmp/vdb-guardian-run \
  --job-id migrate-and-verify-smoke \
  --dimension 8 \
  --batch-size 100 \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

## 输出示例

成功运行后会打印出关于迁移和验证状态的摘要：

```text
migrate-and-verify completed
source_collection: items
target_table: items
dimension: 8
records_read: 100
records_written: 100
consistency_score: 1.000000
fingerprint_distance: 0.000000
matched_queries: 10
source_fingerprint: /tmp/vdb-guardian-run/migrate-and-verify-smoke-source-fingerprint.json
target_fingerprint: /tmp/vdb-guardian-run/migrate-and-verify-smoke-target-fingerprint.json
result: /tmp/vdb-guardian-run/migrate-and-verify-smoke-result.json
```

## 必填标志 (Required flags)

- `--fixture`: 包含验证测试查询向量的合成固定数据源。
- `--milvus-address`: Milvus gRPC 终端点。
- `--pgvector-connection-url`: PostgreSQL 连接 URL。请在日志和文档中对该信息进行脱敏处理。
- `--artifact-dir`: 用于存放源端、目标端和比对结果产物文件的目录。
- `--dimension`: 预期的向量维度。

## 默认值 (Defaults)

- `--source-collection`: `items`
- `--target-table`: `items`
- `--milvus-id-field`: `id`
- `--milvus-vector-field`: `embedding`
- `--pgvector-id-column`: `id`
- `--pgvector-vector-column`: `embedding`
- `--job-id`: `migrate-and-verify`
- `--batch-size`: `100`
- `--top-k`: `3`
- `--expand-k`: `5`
- `--stable-k`: `2`
- `--boundary-k`: `1`
- `--metric`: `cosine`

## 支持范围 (Scope)

目前已实现：

- 真实的端到端数据迁移。
- 自动生成源端 (Milvus) 指纹产物。
- 自动生成目标端 (pgvector) 指纹产物。
- 通过 Python 引擎自动比对产物。
- 包含数据量与主要一致性指标的汇总输出。
- 为整体编排和失败短路（异常阻断）逻辑编写的注入式步骤单元测试。

## 本地冒烟验证示例

针对 `testdata/migration/synthetic-small.json` 在本地迁移环境栈上运行的一次元冒烟测试结果如下：

```text
records_read: 100
records_written: 100
consistency_score: 1.000000
fingerprint_distance: 0.000000
matched_queries: 10
missing_source_queries: 0
missing_target_queries: 0
```

生成的 JSON 结果产物结构如下：

```json
{
  "job_id": "migrate-and-verify-smoke",
  "state": "SUCCEEDED",
  "consistency_score": 1,
  "metrics": {
    "FingerprintDistance": 0,
    "StableNeighborDistance": 0,
    "BoundaryCandidateDistance": 0,
    "BoundaryFlipRate": 0,
    "MatchedQueryCount": 10,
    "MissingSourceQueryCount": 0,
    "MissingTargetQueryCount": 0
  }
}
```

尚未实现：

- 生产环境级别的断点续传 (Checkpointing)。
- 元数据列映射。
- Milvus 分区支持。
- 自动清理源/目标端的无效数据。
- 除了现有的结果产物之外，生成内容更为丰富的 Markdown/JSON 格式诊断报告。

## 安全提示

请优先在本地迁移环境栈或临时测试数据库上运行此命令。

迁移步骤使用了 pgvector 的 upsert 语义，且**不会删除**目标端陈旧的无效记录。为了达成严苛的生产环境数据一致性，未来的迭代将引入显式的数据清理/检查点语义，并加入对元数据/分区的全面支持。

## 测试命令

```bash
go test ./cmd/vdbg -run 'TestParseMigrateAndVerify|TestRunMigrateAndVerify' -v
```

提交前完整检查：

```bash
make fmt
make lint
make test
git diff --check
```
