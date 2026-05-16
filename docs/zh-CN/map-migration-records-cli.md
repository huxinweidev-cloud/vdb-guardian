# map-migration-records CLI

`vdbg map-migration-records` 用于验证 planned pgvector schema 如何驱动后续 Milvus→pgvector full-record migration。

该命令只读取 `vdbg plan-pgvector-schema` 生成的本地 JSON artifact，并写出确定性的 record mapping report。

## 在迁移链路中的位置

```text
inspect-milvus
  -> plan-pgvector-schema
  -> compare-schema-plans
  -> apply-pgvector-schema
  -> inspect-pgvector-schema
  -> compare-applied-schema
  -> map-migration-records
  -> migrate / future full-record migration execution
```

## 命令示例

```bash
go run ./cmd/vdbg map-migration-records \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --output /tmp/vdb-guardian-record-mapping.json
```

不传 `--output` 时，JSON report 会输出到 stdout：

```bash
go run ./cmd/vdbg map-migration-records \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json
```

## 安全边界

该命令：

- 不连接 Milvus；
- 不连接 PostgreSQL；
- 不执行 DDL 或 DML；
- 不读取或迁移行数据；
- 不需要也不会打印连接串；
- `--output` artifact 使用 `0600` 权限写出。

## Mapping 规则

report 会把 planned columns 分类为：

- `primary_key`: planned primary key column；
- `vector`: target type 以 `vector` 开头的 dense pgvector column；
- `scalar`: 普通 source scalar field 到 target column；
- `dynamic_metadata`: `_milvus_dynamic` metadata JSON column；
- `partition_metadata`: `_milvus_partition` metadata column。

blocking issues 包括：

- 缺少 primary key mapping；
- 缺少 vector mapping；
- 多个 primary key mapping；
- 多个 vector mapping；
- unsupported planned column。

当存在 blocking issues 时，命令会先写出 report，再返回非零。

## Report 示例结构

```json
{
  "schema_version": "v1",
  "schema_plan": "/tmp/vdb-guardian-pgvector-schema-plan.json",
  "status": "pass",
  "mappings": [
    {
      "source_collection": "items",
      "target_schema": "public",
      "target_table": "items",
      "primary_key": {
        "kind": "primary_key",
        "source_field": "id",
        "target_column": "id",
        "target_type": "text",
        "nullable": false,
        "support_level": "supported"
      },
      "vector": {
        "kind": "vector",
        "source_field": "embedding",
        "target_column": "embedding",
        "target_type": "vector(1536)",
        "nullable": false,
        "support_level": "supported"
      }
    }
  ],
  "summary": {
    "collection_count": 1,
    "scalar_mapping_count": 0,
    "dynamic_metadata_mapping_count": 0,
    "partition_metadata_mapping_count": 0,
    "issue_count": 0,
    "blocking_issue_count": 0
  }
}
```

## 当前局限

本阶段只验证 mapping 形状，还没有把 scalar、dynamic field 或 partition payload extraction 接入真实 `vdbg migrate` writer。真实 full-record execution 会放在下一阶段实现。

## 测试命令

```bash
go test ./internal/migration ./cmd/vdbg -run 'TestBuildRecordMappingPlan|TestRunMapMigrationRecords' -v
```
