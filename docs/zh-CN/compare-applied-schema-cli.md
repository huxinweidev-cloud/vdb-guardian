# compare-applied-schema CLI

`vdbg compare-applied-schema` 会对比 pgvector schema plan artifact 与 live PostgreSQL/pgvector schema inspection artifact。

这是 Milvus→pgvector 规划链路中的第六阶段只读 drift gate：

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
  -> inspect-pgvector-schema
  -> compare-applied-schema
```

该命令只读取本地 JSON 文件，不连接 PostgreSQL，不执行 DDL/DML，也不修复 drift。

## 用法

```bash
go run ./cmd/vdbg compare-applied-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --output /tmp/vdb-guardian-applied-schema-compare-report.json
```

不传 `--output` 时，JSON report 会输出到 stdout：

```bash
go run ./cmd/vdbg compare-applied-schema \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json
```

## 参数

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `--schema-plan` | 是 | `plan-pgvector-schema` 生成的 JSON artifact 路径。 |
| `--live-schema` | 是 | `inspect-pgvector-schema` 生成的 JSON artifact 路径。 |
| `--output` | 否 | comparison report 输出路径；省略时写 stdout。 |

## Report 状态

| 状态 | 含义 |
| --- | --- |
| `pass` | planned schema 与 live schema 通过所有阻断检查。 |
| `warn` | 没有阻断 drift，但存在额外 live object 或 degraded planned feature，需要人工复核。 |
| `fail` | 发现阻断 drift。命令会先写出 report，然后返回非零。 |

## 阻断检查

以下情况会导致 comparison fail：

- schema plan artifact 版本不支持；
- live schema inspection artifact 版本不支持；
- planned target schema 与 live target schema 不一致；
- planned table 在 live schema 中缺失；
- planned column 在 live table 中缺失；
- planned column type 与 live 不一致；
- planned vector dimension 与 live 不一致；
- planned nullable 与 live 不一致；
- planned primary key 在 live 中不是 primary key；
- 规划了 vector column 但 pgvector extension 缺失；
- planned supported index 在 live 中缺失；
- planned supported index method 与 live 不一致。

## Warnings

以下情况会写入 warning，但不会单独导致命令失败：

- live 中存在 plan 未声明的额外 table；
- live table 中存在 plan 未声明的额外 column；
- live table 中存在 plan 未声明的额外 index；
- planned column/index 已携带的 warning 文本。

## Report 示例

```json
{
  "schema_version": "v1",
  "status": "pass",
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "live_schema": "/tmp/vdb-guardian-live-pgvector-schema.json",
  "summary": {
    "tables_checked": 1,
    "columns_checked": 2,
    "indexes_checked": 1,
    "mismatch_count": 0,
    "warning_count": 0
  },
  "tables": [
    {
      "target_table": "items",
      "status": "pass",
      "checks": [
        {
          "name": "table_present",
          "status": "pass",
          "source": "items",
          "target": "items"
        },
        {
          "name": "vector_dimension_preserved",
          "status": "pass",
          "source": "vector(1536)",
          "target": "vector(1536)",
          "detail": "embedding"
        }
      ]
    }
  ]
}
```

## 安全说明

- 命令只读取本地 JSON artifact。
- 不连接 PostgreSQL。
- 不执行 SQL。
- report 文件权限为 `0600`，因为 schema/topology 元数据可能敏感。
- 输入 artifact 协议不包含数据库连接串，输出也不会包含连接串。

## 当前局限

- 暂不检查 index operator class 等价性；第一版只检查 planned index name 与 method。
- 额外 live object 作为 warning，而不是阻断项。
- 不修复 drift。
- 不验证行数据或 vector payload。

该 gate 通过后，后续全量记录迁移阶段可以基于 planning/apply/live-inspection 链路建立的 schema 可信度继续推进。
