# compare-artifacts CLI

`vdbg compare-artifacts` 命令负责驱动 Python 指纹引擎，对比本地现有的源端产物与目标端产物，并最终生成一份结构化的标准比对结果 JSON 文件。

在本地迁移 MVP 阶段，这是**第一个真正将两大数据库行为连接在一起的桥梁命令**：

```text
Milvus 源端指纹产物
  +
pgvector 目标端指纹产物
  ->
进入 Python 产物比对引擎
  ->
输出 <artifact-dir>/<job-id>-result.json 最终结果
```

## 适用场景 (When to use it)

该命令的执行前提是，您已经成功为源端和目标端生成了兼容 Python 规范的指纹产物：

- 通过 `vdbg build-milvus-artifact` 生成的源端产物。
- 通过 `vdbg build-pgvector-artifact` 生成的目标端产物。

该命令**不会**主动连接 Milvus 或 PostgreSQL。它的核心逻辑纯粹是读取本地磁盘上的 JSON 产物文件，然后通过 `internal/engine.PythonRunner` 拉起子进程执行 Python 比对引擎。

## 使用示例 (Example)

```bash
go run ./cmd/vdbg compare-artifacts \
  --source /tmp/vdb-guardian-source-fingerprint.json \
  --target /tmp/vdb-guardian-target-fingerprint.json \
  --artifact-dir /tmp/vdb-guardian-compare \
  --job-id real-artifact-smoke
```

预期的终端输出格式如下：

```text
artifact comparison completed
job_id: real-artifact-smoke
consistency_score: 1.000000
fingerprint_distance: 0.000000
stable_neighbor_distance: 0.000000
boundary_candidate_distance: 0.000000
boundary_flip_rate: 0.000000
matched_queries: 10
missing_source_queries: 0
missing_target_queries: 0
source_fingerprint: /tmp/vdb-guardian-source-fingerprint.json
target_fingerprint: /tmp/vdb-guardian-target-fingerprint.json
result: /tmp/vdb-guardian-compare/real-artifact-smoke-result.json
```

## 命令行参数 (Flags)

| 参数 (Flag) | 默认值 | 描述 (Description) |
| --- | --- | --- |
| `--source` | 必填 | 源端指纹产物的 JSON 文件路径。 |
| `--target` | 必填 | 目标端指纹产物的 JSON 文件路径。 |
| `--artifact-dir` | 必填 | 用于输出最终比对结果产物的目标目录。 |
| `--job-id` | `artifact-compare` | 验证作业的唯一标识符，将会拼接入结果文件的名称中。 |

## 比对结果产物 (Result artifact)

比对结果最终将写入：

```text
<artifact-dir>/<job-id>-result.json
```

该 JSON 文件封装了以下核心字段：

- `job_id`
- `state`
- `consistency_score` (整体一致性得分)
- `metrics.fingerprint_distance` (综合指纹距离)
- `metrics.stable_neighbor_distance` (稳定邻居距离)
- `metrics.boundary_candidate_distance` (边界候选者距离)
- `metrics.boundary_flip_rate` (边界反转率)
- `metrics.matched_query_count` (成功匹配的查询数)
- `metrics.missing_source_query_count` (源端缺失的查询数)
- `metrics.missing_target_query_count` (目标端缺失的查询数)

## 验证与容错 (Validation)

在拉起 Python 引擎之前，Go 命令端会进行前置校验：

- 确认 `source` 参数提供的路径确实指向一个存在的文件。
- 确认 `target` 参数提供的路径确实指向一个存在的文件。
- 确认 `artifact-dir` 所指向的目录已经存在。
- 确认 `job-id` 不为空。

随后，Python 引擎会在内部独立进行产物的 Schema 级合法性校验、重名查询排查、对缺失查询施加距离惩罚，并执行各项指标的数学聚合。

## 当前局限性 (Limitations)

- 这只是一个单纯的“比对工具”，并非自动化的“数据迁移工具”。
- 它在隐式地假设：用于比对的这两份文件，在生成时采用了相同的 `top-k`、`stable-k` 以及 `boundary-k` 参数口径。
- 尚未内置渲染和导出供人类直观阅读的 Markdown 格式的综合迁移报告。
- 强依赖于本地系统的 Python 环境，且其探测机制与 `offline-verify` 命令保持一致。