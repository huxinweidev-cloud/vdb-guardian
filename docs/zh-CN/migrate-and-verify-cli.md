# migrate-and-verify CLI 命令

`vdbg migrate-and-verify` 命令用于执行首个单次 (one-shot) 本地 Milvus 到 pgvector 的“迁移-一致性验证”闭环流程。

它组合了现有的、经过充分测试的各个命令与模块边界：

```text
将数据从 Milvus 迁移到 pgvector
构建 Milvus 源端指纹产物
构建 pgvector 目标端指纹产物
通过 Python 引擎对产物进行验证比对
可选：构建 Milvus/pgvector live full-record artifact，并执行本地 full-record equality 对比
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
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
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
  --full-record-compare \
  --checkpoint-path /tmp/vdb-guardian-run/migrate-and-verify-smoke-checkpoint.json \
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
source_full_records: /tmp/vdb-guardian-run/migrate-and-verify-smoke-source-full-records.json
target_full_records: /tmp/vdb-guardian-run/migrate-and-verify-smoke-target-full-records.json
full_record_compare: /tmp/vdb-guardian-run/migrate-and-verify-smoke-full-record-compare.json
```

启用 checkpoint 后，Markdown 报告会包含 `Checkpoint / resume` 小节，diagnostic JSON 会包含：

```json
"checkpoint": {
  "path": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-checkpoint.json",
  "resume_from": ""
}
```

## 必填标志 (Required flags)

- `--fixture`: 包含验证测试查询向量的合成固定数据源。
- `--milvus-address`: Milvus gRPC 终端点。
- `--pgvector-connection-url`: PostgreSQL 连接 URL。请在日志和文档中对该信息进行脱敏处理。
- `--artifact-dir`: 用于存放源端、目标端和比对结果产物文件的目录。
- `--dimension`: 预期的向量维度。
- `--record-mapping`: 可选的 `vdbg map-migration-records` JSON 路径。提供后，迁移步骤会在指纹验证前根据 mapping artifact 执行 full-record migration。启用 `--full-record-compare` 时该参数必填。

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
- `--full-record-compare`: `false`。启用后，会基于 `--record-mapping` 构建 Milvus/pgvector live full-record artifact，执行本地 `compare-full-records`，并在 Markdown / diagnostic JSON 中记录相关路径；即使 equality gate 失败，也会先保留诊断报告再返回非零错误。
- `--checkpoint-path`: 空。启用后，迁移步骤会在每个成功的 pgvector 写入批次后写出 `0600` checkpoint；如果某个写入批次失败，也会先写出 failed checkpoint 再返回错误。
- `--resume-from`: 空。启用后，迁移步骤会在创建真实数据库 runner 前加载并校验 checkpoint。如果省略 `--checkpoint-path`，后续进度会写回同一个 checkpoint 文件。
- `--min-consistency-score`: `0`。生成报告后，如果 `consistency_score` 低于该阈值，命令会失败。
- `--max-fingerprint-distance`: `1`。生成报告后，如果 `fingerprint_distance` 高于该阈值，命令会失败。

## 可选 checkpoint / resume

`migrate-and-verify` 会把 checkpoint/resume 参数透传给内部 migration 步骤，不改变 fingerprint 或 full-record compare 的语义：

```bash
go run ./cmd/vdbg migrate-and-verify \
  --fixture testdata/migration/synthetic-small.json \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --artifact-dir /tmp/vdb-guardian-run \
  --dimension 1536 \
  --checkpoint-path /secure/artifacts/migrate-and-verify-checkpoint.json
```

恢复时，把 checkpoint artifact 通过 `--resume-from` 传回：

```bash
go run ./cmd/vdbg migrate-and-verify \
  --fixture testdata/migration/synthetic-small.json \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --artifact-dir /tmp/vdb-guardian-run \
  --dimension 1536 \
  --resume-from /secure/artifacts/migrate-and-verify-checkpoint.json
```

Resume 校验会 fail closed：当 checkpoint 中的源集合、目标表、维度、batch size、schema-plan fingerprint、record-mapping fingerprint 或状态不安全时，会拒绝继续。已经 completed 的 checkpoint 不能用于 resume。`--reset-target` 不能与 `--resume-from` 同时使用；恢复迁移时不能在继续前清空目标表。

Checkpoint 文件权限为 `0600`，只包含非敏感的迁移身份、记录计数、完成/失败批次范围和 resume offset；不会包含 PostgreSQL connection URL、凭据、token、原始向量或行 payload。

MVP 限制：源端 Milvus reader 仍会先读取源结果集，然后再执行 pgvector 批量写入。当前 checkpoint 保护的是目标端写入批次进度和 resume offset；source cursor/page-level streaming 仍是后续工作。

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
- 可选 `--full-record-compare` equality gate：基于同一个 passing record mapping artifact 构建 source/target full-record artifacts 并执行本地 equality compare。
- 可选 `--checkpoint-path` 与 `--resume-from` 透传，用于 batch-level migration checkpoint/resume。
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
  "full_record_equality": {
    "enabled": true,
    "source_artifact": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-source-full-records.json",
    "target_artifact": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-target-full-records.json",
    "compare_report": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-full-record-compare.json"
  },
  "checkpoint": {
    "path": "/tmp/vdb-guardian-run/migrate-and-verify-smoke-checkpoint.json",
    "resume_from": ""
  },
  "quality_gates": {
    "min_consistency_score": 0.999,
    "max_fingerprint_distance": 0.001,
    "passed": true
  }
}
```

尚未实现：

- source cursor/page-level streaming，即 resume 时无需重新读取源结果集。
- 生产级 bulk import / COPY 路径。
- 自动 stale target row cleanup / reconciliation。

## 安全提示

请优先在本地迁移环境栈或临时测试数据库上运行此命令。

Full-record compare artifacts 可能包含 record IDs、scalar 字段、dynamic metadata、partition 值和 vector hashes。请只把 `--artifact-dir` 指向经过批准的安全目录，不要把生成的 `0600` artifact 内容粘贴到聊天或日志中，诊断完成后及时清理。

默认情况下，迁移步骤使用 pgvector 的 upsert 语义，且**不会删除**目标端陈旧的无效记录。对于一次性本地冒烟或临时测试库，可以传入 `--reset-target`，让命令在迁移前清空目标表。除非明确需要破坏性清理，否则不要在生产表上启用该选项。

不要把 `--reset-target` 和 `--resume-from` 组合使用；命令会在迁移开始前拒绝该组合。

如果需要在迁移后强制校验 pgvector 目标表行数必须等于 `records_written`，可以传入 `--strict-count`。该选项最适合与 `--reset-target` 组合用于干净的冒烟验证；如果不清理目标端，陈旧行可能会按预期触发 strict count 失败。

如果要把该命令接入自动化质量门禁，可以传入 `--min-consistency-score` 和/或 `--max-fingerprint-distance`。这两个阈值会在 Markdown 报告写入之后再校验，因此失败的运行仍会留下可人工阅读的诊断报告。

为了达成严苛的生产环境数据一致性，未来的迭代还需要加入源端 streaming cursor、bulk import 策略和 stale-row reconciliation。

## 测试命令

```bash
go test ./cmd/vdbg -run 'TestParseMigrateAndVerify|TestRunMigrateAndVerify' -v
go test ./internal/reporting -v
```

提交前完整检查：

```bash
make fmt
make lint
make test
make coverage-check
git diff --check
```

对于迁移关键路径变更，还应运行 opt-in 本地 Docker 冒烟：

```bash
make smoke-migration-checkpoint
```

该冒烟会启动/检查一次性迁移栈，seed 已提交的小型 Milvus fixture，执行 schema/mapping gates，运行带 checkpoint 的迁移，再通过 `migrate-and-verify` 走 resume 路径，验证目标端 100 行数据、`0600` report/checkpoint 权限，并扫描生成 artifact 中的明显 secret marker。它依赖 Docker 和本地端口，因此不会放入默认 `make test`；不要把它指向生产数据库。
