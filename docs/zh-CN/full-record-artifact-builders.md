# Full-record artifact builders

`vdbg build-milvus-record-artifact` 与 `vdbg build-pgvector-record-artifact` 会基于一个 `status: pass` 的 `map-migration-records` artifact，从 live 服务生成本地 full-record artifact。

这两个命令补齐 mapping-driven full-record migration 的校验链路：

```text
map-migration-records
-> migrate --record-mapping
-> build-milvus-record-artifact
-> build-pgvector-record-artifact
-> compare-full-records
```

## 安全边界

两个 builder 命令都是只读命令：

- 要求已有 `status: pass` 的 mapping artifact；
- 当前阶段只接受单 collection / table mapping；
- 会拒绝缺少 primary key 或 vector mapping 的 artifact；
- 不执行 DDL/DML；
- 输出 JSON 最终权限固定为 `0600`，包括覆盖已有文件时；
- 不在 stdout 打印 PostgreSQL connection URL。

## Milvus source artifact

```bash
go run ./cmd/vdbg build-milvus-record-artifact \
  --milvus-address localhost:19530 \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --output /tmp/vdb-guardian-source-full-records.json
```

该命令按 mapping 读取 Milvus source 字段：

- primary key source field；
- vector source field；
- 已映射的 scalar source fields；
- dynamic metadata source field（如果存在）；
- partition metadata source field（如果存在）。

输出元数据：

```json
{
  "schema_version": "v1",
  "system": "milvus",
  "collection": "items",
  "record_mapping_path": "/tmp/vdb-guardian-record-mapping.json",
  "records": []
}
```

## pgvector target artifact

```bash
go run ./cmd/vdbg build-pgvector-record-artifact \
  --pgvector-connection-url '[REDACTED]' \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --output /tmp/vdb-guardian-target-full-records.json
```

该命令读取 pgvector target columns，并把 scalar 值映射回 Milvus artifact 使用的 source field 名称，避免 `compare-full-records` 受到 target column 命名差异影响。

pgvector reader 使用经过校验和 quote 的 identifier 构造只读 `SELECT`，并按映射后的 ID column 排序，保证输出顺序稳定。

输出元数据：

```json
{
  "schema_version": "v1",
  "system": "pgvector",
  "collection": "items",
  "record_mapping_path": "/tmp/vdb-guardian-record-mapping.json",
  "records": []
}
```

## Vector 表示

artifact 不保存原始向量，只保存 vector hash 与维度：

```json
{
  "id": "sku-1",
  "vector_hash": "sha256:...",
  "vector_dimension": 8,
  "scalars": {"product_title": "First"},
  "dynamic_metadata": {"brand": "acme"},
  "partition": "tenant_a"
}
```

vector hash 会先通过确定性的 float32-compatible 表示规范化，从而让 Milvus float32 读取路径和 pgvector float64/text 读取路径在同一迁移向量上得到一致 hash。

## 对比 artifacts

```bash
go run ./cmd/vdbg compare-full-records \
  --source /tmp/vdb-guardian-source-full-records.json \
  --target /tmp/vdb-guardian-target-full-records.json \
  --output /tmp/vdb-guardian-full-record-compare.json
```

`compare-full-records` 仍然是 artifact-only：它不会连接 Milvus 或 PostgreSQL。

## 当前边界

本阶段新增 live read-only artifact builders 与本地 full-record equality compare 链路。`migrate-and-verify --full-record-compare` 自动编排仍留到后续阶段。
