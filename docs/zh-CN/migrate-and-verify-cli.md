# migrate-and-verify CLI 命令

`vdbg migrate-and-verify` 命令用于执行首个单次 (one-shot) 本地 Milvus 到 pgvector 的“迁移-一致性验证”闭环流程。

它组合了现有的、经过充分测试的各个命令与模块边界：

```text
将数据从 Milvus 迁移到 pgvector
构建 Milvus 源端指纹产物
构建 pgvector 目标端指纹产物
通过 Python 引擎对产物进行验证比对
生成 Markdown 报告
生成机器可读的诊断 JSON 报告
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
  --metric cosine \
  --reset-target \
  --strict-count \
  --min-consistency-score 0.999 \
  --max-fingerprint-distance 0.001
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
report: /tmp/vdb-guardian-run/migrate-and-verify-smoke-report.md
diagnostic_report: /tmp/vdb-guardian-run/migrate-and-verify-smoke-diagnostic-report.json
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
- `--reset-target`: `false`。启用后会在迁移前清空 pgvector 目标表。
- `--strict-count`: `false`。启用后，如果迁移后的 pgvector 目标表行数不等于 `records_written`，命令会失败。
- `--min-consistency-score`: `0`。生成报告后，如果 `consistency_score` 低于该阈值，命令会失败。
- `--max-fingerprint-distance`: `1`。生成报告后，如果 `fingerprint_distance` 高于该阈值，命令会失败。

## 支持范围 (Scope)

目前已实现：

- 真实的端到端数据迁移。
- 自动生成源端 (Milvus) 指纹产物。
- 自动生成目标端 (pgvector) 指纹产物。
- 通过 Python 引擎自动比对产物。
- 在 `<artifact-dir>/<job-id>-report.md` 生成 Markdown 报告。
- 在 `<artifact-dir>/<job-id>-diagnostic-report.json` 生成机器可读的诊断 JSON 报告。
- 包含数据量与主要一致性指标的汇总输出。
- 可选 `--reset-target` 清理能力：迁移前清空 pgvector 目标表。
- 可选 `--strict-count` 校验能力：迁移后目标表行数不匹配时直接失败。
- 可选 `--min-consistency-score` 与 `--max-fingerprint-distance` 质量门禁：生成 Markdown 报告后，如果指标不达标则使命令失败。
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

生成的诊断 JSON 报告产物结构如下：

```json
{
  "schema_version": "v1",
  "job_id": "migrate-and-verify-smoke",
  "state": "SUCCEEDED",
  "migration": {
    "source_collection": "items",
    "target_table": "items",
    "dimension": 8,
    "records_read": 100,
    "records_written": 100
  },
  "verification": {
    "consistency_score": 1,
    "metrics": {
      "fingerprint_distance": 0,
      "matched_query_count": 10,
      "missing_source_query_count": 0,
      "missing_target_query_count": 0
    }
  },
  "artifacts": {
    "source_fingerprint": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-source-fingerprint.json",
    "target_fingerprint": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-target-fingerprint.json",
    "result_json": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-result.json"
  },
  "safety": {
    "reset_target": true,
    "strict_count": true
  },
  "quality_gates": {
    "min_consistency_score": 0.999,
    "max_fingerprint_distance": 0.001,
    "passed": true
  }
}
```

尚未实现：

- 生产环境级别的断点续传 (Checkpointing)。
- 元数据列映射。
- Milvus 分区支持。
- 自动清理源/目标端的无效数据。

## 安全提示

请优先在本地迁移环境栈或临时测试数据库上运行此命令。

默认情况下，迁移步骤使用 pgvector 的 upsert 语义，且**不会删除**目标端陈旧的无效记录。对于一次性本地冒烟或临时测试库，可以传入 `--reset-target`，让命令在迁移前清空目标表。除非明确需要破坏性清理，否则不要在生产表上启用该选项。

如果需要在迁移后强制校验 pgvector 目标表行数必须等于 `records_written`，可以传入 `--strict-count`。该选项最适合与 `--reset-target` 组合用于干净的冒烟验证；如果不清理目标端，陈旧行可能会按预期触发 strict count 失败。

如果要把该命令接入自动化质量门禁，可以传入 `--min-consistency-score` 和/或 `--max-fingerprint-distance`。这两个阈值会在 Markdown 报告写入之后再校验，因此失败的运行仍会留下可人工阅读的诊断报告。

为了达成严苛的生产环境数据一致性，未来的迭代将引入显式的检查点语义，并加入对元数据/分区的全面支持。

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
