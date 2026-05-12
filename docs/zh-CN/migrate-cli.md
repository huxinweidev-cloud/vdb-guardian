# migrate CLI 命令

`vdbg migrate` 命令用于执行首个真实的 Milvus 到 pgvector 记录迁移流程。

它通过 Milvus SDK 查询路径从 Milvus 源集合读取规范化记录，并通过基于 pgx 的写入器将它们写入 pgvector 目标表。

该命令不会启动 Docker、创建服务、构建指纹产物或比较检索行为。它假设本地迁移环境栈或同等的临时测试数据库已处于运行状态。

## 命令示例

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --source-collection items \
  --milvus-id-field id \
  --milvus-vector-field embedding \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --pgvector-id-column id \
  --pgvector-vector-column embedding \
  --dimension 8 \
  --batch-size 100
```

## 输出示例

成功运行后会打印一份简明的摘要：

```text
migration completed
source_collection: items
target_table: items
dimension: 8
records_read: 100
records_written: 100
```

## 必填标志 (Required flags)

- `--milvus-address`: Milvus gRPC 终端点，例如 `localhost:19530`。
- `--pgvector-connection-url`: PostgreSQL 连接 URL。请在日志和文档中对该信息进行脱敏处理。
- `--dimension`: 预期的向量维度。运行器会针对该值校验每一个迁移的向量。

## 默认值 (Defaults)

- `--source-collection`: `items`
- `--target-table`: `items`
- `--milvus-id-field`: `id`
- `--milvus-vector-field`: `embedding`
- `--pgvector-id-column`: `id`
- `--pgvector-vector-column`: `embedding`
- `--batch-size`: `100`

## 支持范围 (Scope)

目前已实现：

- 真实基于 Milvus SDK 的源端数据读取。
- 真实基于 pgx 的 pgvector 目标端 upsert（插入或更新）写入。
- 向量维度校验。
- CLI 标志解析与注入运行器的单元测试。
- 包含已读取和已写入记录数的摘要输出。

尚未实现：

- 在此命令内生成源/目标指纹产物。
- 在此命令内生成比较结果产物。
- 元数据字段。
- Milvus 分区。
- 增量检查点 (Checkpoints)。
- 生产环境级别的批量导入 (Bulk import)。

## 安全提示

在明确加入生产级别的迁移语义之前，请仅针对本地迁移 MVP 服务或临时测试数据库运行此命令。

pgvector 写入器采用 upsert 语义：

```sql
INSERT ... ON CONFLICT (id) DO UPDATE
```

它**不会**删除目标表。如果目标表中包含 Milvus 源中不存在的旧记录，这个首个 MVP 版本的命令不会将其删除。

## 测试命令

```bash
go test ./cmd/vdbg -run 'TestParseMigrate|TestRunMigrate' -v
```

提交前完整检查：

```bash
make fmt
make lint
make test
git diff --check
```
