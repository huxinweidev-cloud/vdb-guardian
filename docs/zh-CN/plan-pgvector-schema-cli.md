# plan-pgvector-schema CLI

`vdbg plan-pgvector-schema` 会读取 Milvus inspection plan，并输出 dry-run PostgreSQL/pgvector schema plan，其中包含确定性的 DDL 预览。

这是面向更完整 Milvus→pgvector 迁移规划的第二阶段能力。该命令不会连接 PostgreSQL、不会执行 DDL、不会创建表、不会创建索引，也不会迁移记录。下一步可使用 `vdbg compare-schema-plans` 在 apply DDL 之前验证生成的 schema plan 与 source inspection plan 是否一致，然后使用 `vdbg apply-pgvector-schema` dry-run 或执行已经验证的 DDL。

## 命令

```bash
go run ./cmd/vdbg plan-pgvector-schema \
  --inspection-plan /tmp/vdb-guardian-milvus-plan.json \
  --output /tmp/vdb-guardian-pgvector-schema-plan.json
```

指定目标 schema：

```bash
go run ./cmd/vdbg plan-pgvector-schema \
  --inspection-plan /tmp/vdb-guardian-milvus-plan.json \
  --target-schema public \
  --output /tmp/vdb-guardian-pgvector-schema-plan.json
```

不传 `--output` 时，命令会把格式化后的 JSON 输出到 stdout。

## 输出摘要

设置 `--output` 后，成功运行会打印：

```text
schema plan completed
output: /tmp/vdb-guardian-pgvector-schema-plan.json
tables: 1
warnings: 0
unsupported_features: 0
```

## JSON 结构

输出计划使用 `v1` schema：

```json
{
  "schema_version": "v1",
  "source_plan": "/tmp/vdb-guardian-milvus-plan.json",
  "target": {
    "type": "pgvector",
    "schema": "public"
  },
  "tables": [
    {
      "source_collection": "items",
      "target_schema": "public",
      "target_table": "items",
      "columns": [
        {
          "source_field": "id",
          "target_column": "id",
          "target_type": "varchar(64)",
          "primary_key": true,
          "nullable": false,
          "support_level": "supported"
        },
        {
          "source_field": "embedding",
          "target_column": "embedding",
          "target_type": "vector(8)",
          "nullable": false,
          "support_level": "supported"
        }
      ],
      "create_table_sql": "CREATE EXTENSION IF NOT EXISTS vector;\nCREATE TABLE IF NOT EXISTS public.items (...);",
      "indexes": [
        {
          "source_field": "embedding",
          "target_schema": "public",
          "target_table": "items",
          "target_column": "embedding",
          "target_index": "items_embedding_hnsw_idx",
          "target_index_type": "hnsw",
          "target_ops": "vector_cosine_ops",
          "create_index_sql": "CREATE INDEX IF NOT EXISTS items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops);",
          "support_level": "degraded"
        }
      ]
    }
  ],
  "summary": {
    "table_count": 1,
    "warning_count": 0,
    "unsupported_feature_count": 0
  }
}
```

## 标识符规则

目标 schema、table、column 和 index 名称会由源名称转换成确定性的 PostgreSQL 安全标识符：

- 转为小写；
- 不支持的字符转为下划线；
- 数字开头时加 `t_` 前缀；
- PostgreSQL 保留字追加下划线；
- 多个源字段转换后重名时追加数字后缀。

示例：

| Source | Target |
|---|---|
| `Items.Collection` | `items_collection` |
| `User-Profile.Vector` | `user_profile_vector` |
| `123items` | `t_123items` |
| `select` | `select_` |

## Dynamic fields 与分区

- 如果 Milvus collection 开启 dynamic field，计划会增加 `_milvus_dynamic jsonb`，支持级别为 `degraded`。
- 如果 Milvus collection 存在分区，计划会增加 `_milvus_partition text`，支持级别为 `degraded`。
- 第二阶段不会创建 PostgreSQL declarative partition。

## 安全说明

- 命令只读取本地 inspection plan JSON。
- 命令不会连接 Milvus 或 PostgreSQL。
- 命令不会执行生成的 SQL。
- 输出文件使用 `0600` 权限，因为 schema / topology metadata 可能属于敏感信息。

## 测试命令

```bash
go test ./internal/schema ./cmd/vdbg -run 'TestSanitizePGIdentifier|TestBuildPGVectorSchemaPlan|TestRenderCreateTableSQL|TestRenderPGVectorIndexSQL|TestRunPlanPGVectorSchema|TestParsePlanPGVectorSchema' -v
```

提交前完整门禁：

```bash
make fmt
make lint
make test
git diff --check
```
