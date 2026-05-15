# apply-pgvector-schema CLI

`vdbg apply-pgvector-schema` 会执行或 dry-run 执行此前生成的 pgvector schema plan。

这是 Milvus→pgvector 规划链路中的第四阶段执行边界：

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
```

该命令默认安全：如果既没有传 `--dry-run` 也没有传 `--execute`，会以 dry-run 模式运行，不连接 PostgreSQL。

## 命令

不需要数据库凭据的 dry-run：

```bash
go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke
```

显式 dry-run：

```bash
go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke \
  --dry-run
```

执行 DDL：

```bash
go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --pgvector-connection-url '[REDACTED]' \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke \
  --execute
```

创建 extension/table，但跳过 index DDL：

```bash
go run ./cmd/vdbg apply-pgvector-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --pgvector-connection-url '[REDACTED]' \
  --artifact-dir /tmp/vdb-guardian-schema-apply \
  --job-id schema-apply-smoke \
  --execute \
  --skip-indexes
```

## 参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--schema-plan` | 必填 | `vdbg plan-pgvector-schema` 生成的 JSON artifact。 |
| `--artifact-dir` | `artifacts` | schema apply report 输出目录。 |
| `--job-id` | `pgvector-schema-apply` | 用于 report 文件名的任务 ID。 |
| `--dry-run` | 隐式默认 | 不连接 PostgreSQL，也不执行 SQL。 |
| `--execute` | false | 通过 PostgreSQL/pgvector 执行 schema DDL。 |
| `--pgvector-connection-url` | 仅 `--execute` 必填 | PostgreSQL 连接串；不会写入 report。 |
| `--skip-indexes` | false | execute 模式下创建表但跳过 index DDL。 |
| `--allow-unsupported` | false | schema plan 包含 unsupported feature 时仍允许 execute。 |

## 输出 artifact

命令会写出：

```text
<artifact-dir>/<job-id>-pgvector-schema-apply-report.json
```

report 文件权限为 `0600`，因为 schema artifact 可能包含拓扑或字段元数据。

示例结构：

```json
{
  "schema_version": "v1",
  "job_id": "schema-apply-smoke",
  "mode": "dry_run",
  "status": "planned",
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "target": {
    "type": "pgvector",
    "schema": "public"
  },
  "tables": [
    {
      "source_collection": "items",
      "target_table": "items",
      "create_table_sql": "CREATE EXTENSION IF NOT EXISTS vector;\nCREATE TABLE IF NOT EXISTS public.items (...);",
      "applied": false,
      "indexes": [
        {
          "source_field": "embedding",
          "target_index": "items_embedding_hnsw_idx",
          "create_index_sql": "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops);",
          "applied": false
        }
      ]
    }
  ],
  "summary": {
    "table_count": 1,
    "table_applied_count": 0,
    "index_count": 1,
    "index_applied_count": 0,
    "warning_count": 0,
    "unsupported_feature_count": 0
  }
}
```

状态：

- `planned`：dry-run 完成，没有执行 SQL。
- `applied`：execute 模式下所有选中的 table/index 语句均执行成功。
- `blocked`：执行前被阻断，例如存在 unsupported feature 且未传 `--allow-unsupported`。
- `failed`：SQL 执行失败，可能已有部分语句成功。

## 安全说明

- 默认模式是 dry-run。
- execute 模式必须显式传 `--execute` 和 `--pgvector-connection-url`。
- 连接串不会写入 stdout 或 JSON report。
- 命令只执行 schema plan 中的 `CREATE EXTENSION IF NOT EXISTS vector`、`CREATE TABLE IF NOT EXISTS ...`，以及可选的 `CREATE INDEX IF NOT EXISTS ...`。
- 不执行 drop、truncate、delete，也不会 alter 现有数据。
- unsupported feature 默认阻断 execute，除非显式传 `--allow-unsupported`。
- dry-run 不连接 PostgreSQL。

## 当前局限性

- 不检查或修复已有 table drift。
- 暂未暴露 transaction/lock 策略。
- 不迁移数据。
- schema apply 暂无 checkpoint/resume 语义。

后续可通过 `inspect-pgvector-schema` 和 `compare-applied-schema` 阶段验证实际 PostgreSQL schema。
