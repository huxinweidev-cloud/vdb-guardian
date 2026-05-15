# inspect-pgvector-schema CLI

`vdbg inspect-pgvector-schema` 会连接 PostgreSQL/pgvector，并以只读方式输出目标端 live schema 的 JSON inspection artifact。

这是 Milvus→pgvector 规划链路中的第五阶段验证层：

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
  -> inspect-pgvector-schema
```

该命令只读取 PostgreSQL catalog 元数据。它不会 create、alter、drop、truncate、delete，也不会迁移数据。

## 命令

写出 artifact：

```bash
go run ./cmd/vdbg inspect-pgvector-schema \
  --pgvector-connection-url '[REDACTED]' \
  --target-schema public \
  --output /tmp/vdb-guardian-live-pgvector-schema.json
```

直接输出 JSON 到 stdout：

```bash
go run ./cmd/vdbg inspect-pgvector-schema \
  --pgvector-connection-url '[REDACTED]' \
  --target-schema public
```

## 参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--pgvector-connection-url` | 必填 | PostgreSQL/pgvector 连接串；不会写入 stdout 或 artifact。 |
| `--target-schema` | `public` | 要检查的 PostgreSQL schema。 |
| `--output` | 空 | 可选 JSON 输出路径；不传时 JSON 写到 stdout。 |

## 输出 artifact

传入 `--output` 时，文件权限为 `0600`，因为 live schema / topology 元数据可能较敏感。

示例结构：

```json
{
  "schema_version": "v1",
  "target": {
    "type": "pgvector",
    "schema": "public"
  },
  "extension": {
    "name": "vector",
    "installed": true,
    "version": "0.8.0"
  },
  "tables": [
    {
      "target_table": "items",
      "columns": [
        {
          "name": "id",
          "type": "bigint",
          "formatted_type": "bigint",
          "nullable": false,
          "primary_key": true
        },
        {
          "name": "embedding",
          "type": "vector",
          "formatted_type": "vector(1536)",
          "nullable": false,
          "primary_key": false,
          "vector_dimension": 1536
        }
      ],
      "indexes": [
        {
          "name": "items_embedding_hnsw_idx",
          "method": "hnsw",
          "definition": "CREATE INDEX items_embedding_hnsw_idx ON public.items USING hnsw (embedding vector_cosine_ops)"
        }
      ]
    }
  ],
  "summary": {
    "table_count": 1,
    "column_count": 2,
    "vector_column_count": 1,
    "index_count": 1,
    "warning_count": 0
  }
}
```

## 采集的元数据

- 从 `pg_extension` 读取 pgvector extension 是否安装及版本；
- 从 `information_schema.columns` 和 `pg_catalog.format_type` 读取 table / column / formatted type；
- 从 `vector(1536)` 等 formatted type 中解析 vector dimension；
- 从 `information_schema.table_constraints` 和 `key_column_usage` 读取 primary key；
- 从 `pg_index`、`pg_class`、`pg_namespace`、`pg_am` 读取 index 名称、访问方法和定义。

## 安全说明

- 该命令只读取 PostgreSQL catalog 元数据；
- schema 名通过查询参数传入，不拼接进 SQL；
- 不检查行级 payload；
- 不执行 DDL 或 DML；
- 不修复 schema drift；
- 不迁移数据；
- 连接串不会打印，也不会写入 JSON。

## 当前局限性

- 该命令只做 live schema inventory，不会与 schema plan 对比；
- 暂不解析 index operator class，仅保留 `pg_get_indexdef` 原始定义；
- 不验证 lock setting、transaction policy 或 table ownership。

后续可通过 `compare-applied-schema` 阶段对比 live inspection artifact 与 planned pgvector schema artifact。
