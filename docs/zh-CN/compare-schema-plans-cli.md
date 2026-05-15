# compare-schema-plans CLI

`vdbg compare-schema-plans` 会在执行任何 PostgreSQL DDL 之前，对比只读 Milvus inspection plan 与 dry-run pgvector schema plan。

这是 Milvus→pgvector 规划链路中的第三阶段安全门禁：

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> 后续 apply-pgvector-schema
```

该命令不会连接 Milvus，不会连接 PostgreSQL，也不会执行 SQL。对比通过后，可使用 `vdbg apply-pgvector-schema` dry-run 或执行已经验证的 schema DDL，然后使用 `vdbg inspect-pgvector-schema` 与 `vdbg compare-applied-schema` inventory 并验证 live target schema。

## 命令

```bash
go run ./cmd/vdbg compare-schema-plans \
  --inspection-plan /tmp/vdb-guardian-milvus-plan.json \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --output /tmp/vdb-guardian-schema-compare-report.json
```

如果省略 `--output`，JSON report 会输出到 stdout。

## 输出

成功写入文件时，会打印简短摘要：

```text
schema comparison completed
output: /tmp/vdb-guardian-schema-compare-report.json
status: pass
mismatches: 0
warnings: 0
unsupported_features: 0
```

如果存在阻断性 mismatch，命令仍会先写出 JSON report，然后以非零状态退出，并返回 `schema comparison failed` 错误。

## JSON 结构

输出 report 使用 schema version `v1`：

```json
{
  "schema_version": "v1",
  "status": "pass",
  "inspection_plan": "/tmp/vdb-guardian-milvus-plan.json",
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "summary": {
    "collections_checked": 1,
    "tables_checked": 1,
    "fields_checked": 3,
    "columns_checked": 5,
    "mismatch_count": 0,
    "warning_count": 0,
    "unsupported_feature_count": 0
  },
  "collections": [
    {
      "source_collection": "items",
      "target_table": "items",
      "status": "pass",
      "checks": [
        {
          "name": "primary_key_preserved",
          "status": "pass",
          "source": "id",
          "target": "id"
        },
        {
          "name": "vector_dimension_preserved",
          "status": "pass",
          "source": "FloatVector(8)",
          "target": "vector(8)",
          "detail": "embedding"
        }
      ]
    }
  ]
}
```

## 对比规则

第一版会检查：

- 每个 Milvus collection 都有对应 target table plan；
- collection 名称经 sanitizer 后与 target table 名称一致；
- 每个 Milvus field 都有对应 target column；
- field target type 与 inspection recommendation 一致；
- primary key 被保留；
- nullability 被保留；
- dense float vector dimension 被保留为 `vector(N)`；
- `dynamic_field_enabled=true` 映射到 `_milvus_dynamic jsonb`；
- source partition 映射到 `_milvus_partition text`；
- 支持的非 FLAT index recommendation 有 target index plan 和 `create_index_sql`；
- FLAT index recommendation 被视为 exact scan / 不创建近似索引；
- unsupported feature 与 warning 会进入 summary，不会静默通过。

## 状态值

| Status | 含义 |
| --- | --- |
| `pass` | 没有发现阻断性 mismatch 或 warning。 |
| `warn` | 没有阻断性 mismatch，但存在 degraded/unsupported feature 或 warning，需要人工复核。 |
| `fail` | 存在阻断性 schema mismatch；不应在未复核的情况下 apply schema plan。 |

## 安全说明

- 命令只读取本地 JSON 文件。
- report 文件权限为 `0600`，因为 schema/topology metadata 可能敏感。
- report 不包含数据库连接串或凭据。
- 应在后续任何 `apply-pgvector-schema` 执行步骤前运行。
