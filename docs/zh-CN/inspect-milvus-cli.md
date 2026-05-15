# inspect-milvus CLI

`vdbg inspect-milvus` 会以只读方式检查 Milvus collection 元数据，并输出机器可读的迁移规划 JSON 文档。

这是面向更完整 Milvus→pgvector 迁移规划的第一阶段能力。该命令不会迁移记录、不会创建 PostgreSQL 表、不会创建索引、不会启动 Docker，也不会修改 Milvus。下一步可使用 `vdbg plan-pgvector-schema` 将 inspection JSON 转换为 dry-run pgvector schema / DDL 规划。

## 命令

检查全部 collection：

```bash
go run ./cmd/vdbg inspect-milvus \
  --milvus-address localhost:19530 \
  --output /tmp/vdb-guardian-milvus-plan.json
```

检查单个 collection：

```bash
go run ./cmd/vdbg inspect-milvus \
  --milvus-address localhost:19530 \
  --collection items \
  --output /tmp/vdb-guardian-items-plan.json
```

不传 `--output` 时，命令会把格式化后的 JSON 输出到 stdout。

## 输出摘要

设置 `--output` 后，成功运行会打印：

```text
inspection completed
output: /tmp/vdb-guardian-milvus-plan.json
collections: 1
warnings: 0
unsupported_features: 0
```

## JSON 结构

输出计划使用 `v1` schema：

```json
{
  "schema_version": "v1",
  "source": {
    "type": "milvus",
    "address": "localhost:19530"
  },
  "collections": [
    {
      "name": "items",
      "row_count": 100,
      "description": "product embeddings",
      "auto_id": false,
      "dynamic_field_enabled": false,
      "primary_key": "id",
      "fields": [
        {
          "name": "id",
          "source_type": "VarChar",
          "target_type": "varchar(64)",
          "max_length": 64,
          "primary_key": true,
          "nullable": false,
          "support_level": "supported"
        },
        {
          "name": "embedding",
          "source_type": "FloatVector",
          "target_type": "vector(8)",
          "dimension": 8,
          "nullable": false,
          "support_level": "supported"
        }
      ],
      "indexes": [
        {
          "field": "embedding",
          "source_index_type": "HNSW",
          "source_metric": "COSINE",
          "target_index_type": "hnsw",
          "target_ops": "vector_cosine_ops",
          "support_level": "degraded"
        }
      ],
      "partitions": [
        {
          "name": "_default",
          "support_level": "degraded",
          "recommended_strategy": "metadata_column"
        }
      ]
    }
  ],
  "summary": {
    "collection_count": 1,
    "supported_collection_count": 1,
    "warning_count": 0,
    "unsupported_feature_count": 0
  }
}
```

## 类型映射

第一阶段只给出目标类型建议，不执行 DDL。

| Milvus 类型 | 目标建议 | 支持级别 |
|---|---|---|
| Bool | `boolean` | supported |
| Int8 / Int16 | `smallint` | supported |
| Int32 | `integer` | supported |
| Int64 | `bigint` | supported |
| Float | `real` | supported |
| Double | `double precision` | supported |
| VarChar | `varchar(n)` 或 `text` | supported |
| JSON | `jsonb` | supported |
| FloatVector | `vector(dim)` | supported |
| BinaryVector | `bytea` | degraded |
| SparseFloatVector | `jsonb` | degraded |
| Array | `jsonb` | degraded |

## 索引与分区规划

- Milvus `HNSW` 会建议映射为 pgvector `hnsw`，并在 metric 已知时给出对应 operator class。
- Milvus `IVF_FLAT` / `IVFFLAT` 会建议映射为 pgvector `ivfflat`，并在 metric 已知时给出对应 operator class。
- Milvus `FLAT` 表示为精确扫描 / 不创建近似索引。
- 未知索引类型会标记为 `unsupported` metadata-only 特性。
- 分区会作为元数据保留，并建议后续采用 `metadata_column` 策略；第一阶段不会创建 PostgreSQL declarative partition。

## 安全说明

- 命令只读取 collection 名称、schema 元数据、行数、向量索引元数据和分区名称。
- 命令不会读取向量 payload 或标量记录值。
- 命令不会写入 Milvus 或 PostgreSQL。
- 如果生产地址在你的环境里属于敏感信息，共享日志前应自行脱敏。

## 测试命令

```bash
go test ./internal/inspection ./cmd/vdbg -run 'TestMilvusInspector|TestMapMilvus|TestBuildMilvus|TestRunInspectMilvus|TestParseInspectMilvus' -v
```

提交前完整门禁：

```bash
make fmt
make lint
make test
git diff --check
```
