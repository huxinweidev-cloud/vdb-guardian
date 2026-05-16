# Full-record comparison CLI

`vdbg compare-full-records` 用于对比两份本地 full-record artifact，并写出机器可读的一致性报告。

该命令只处理本地 artifact：它不会连接 Milvus、PostgreSQL 或 pgvector，也不会修改任何系统。它适合放在 `vdbg migrate --record-mapping` 的 mapping-driven migration 之后，用于对 `vdbg build-milvus-record-artifact` 和 `vdbg build-pgvector-record-artifact` 生成的 source / target full-record artifact 做逐字段一致性校验。

## 用法

```bash
go run ./cmd/vdbg compare-full-records \
  --source /tmp/vdb-guardian-source-records.json \
  --target /tmp/vdb-guardian-target-records.json \
  --output /tmp/vdb-guardian-full-record-compare.json
```

使用仓库内置示例 artifact：

```bash
go run ./cmd/vdbg compare-full-records \
  --source testdata/migration/source-full-record-artifact.json \
  --target testdata/migration/target-full-record-artifact.json \
  --output /tmp/vdb-guardian-full-record-compare.json
```

输出 report 使用 `0600` 权限写入。

## Artifact schema

输入 artifact 使用 schema version `v1`：

```json
{
  "schema_version": "v1",
  "system": "milvus",
  "collection": "items",
  "record_mapping_path": "/tmp/vdb-guardian-record-mapping.json",
  "records": [
    {
      "id": "sku-1",
      "vector_hash": "sha256:...",
      "vector_dimension": 8,
      "scalars": {"title": "First", "price": 9.5},
      "dynamic_metadata": {"brand": "acme", "tags": ["sale"]},
      "partition": "tenant_a"
    }
  ]
}
```

artifact 记录 vector hash 和 dimension，而不是原始向量，避免文件过大，并保持报告确定性。

## Report schema

报告包含端点摘要、缺失 ID 列表、字段级 mismatch 以及汇总计数：

```json
{
  "schema_version": "v1",
  "status": "pass",
  "source": {"system": "milvus", "collection": "items", "record_count": 100},
  "target": {"system": "pgvector", "collection": "items", "record_count": 100},
  "summary": {
    "matched_records": 100,
    "missing_source_records": 0,
    "missing_target_records": 0,
    "mismatched_records": 0,
    "scalar_mismatches": 0,
    "dynamic_metadata_mismatches": 0,
    "partition_mismatches": 0,
    "vector_mismatches": 0
  },
  "missing_source_ids": [],
  "missing_target_ids": [],
  "mismatches": []
}
```

## 状态语义

- `pass`: 所有 record ID 一致，且所有被比较字段一致。
- `fail`: 检测到 source-only row、target-only row、scalar mismatch、dynamic metadata mismatch、partition mismatch、vector hash mismatch 或 vector dimension mismatch。

当对比失败时，命令仍会写出诊断 JSON report，然后以非零状态退出。

## 当前边界

本阶段提供 artifact contract、live 只读 full-record artifact builders，以及本地 compare CLI。`migrate-and-verify --full-record-compare` 自动编排会在 builder 与 compare 契约稳定后的后续阶段实现。
