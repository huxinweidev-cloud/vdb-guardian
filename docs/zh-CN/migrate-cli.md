# migrate CLI 命令

`vdbg migrate` 命令用于执行首个真实的 Milvus 到 pgvector 记录迁移流程。

它既可以保持原有 `id + vector` 模式，也可以通过 `--record-mapping` 消费 `vdbg map-migration-records` 生成的 JSON artifact，把映射后的 scalar 字段、dynamic metadata 和 partition metadata 与主键/向量一起迁移。

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
  --batch-size 100 \
  --require-schema-match \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --output /tmp/vdb-guardian-migration-report.json \
  --job-id migration-smoke
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

当提供 `--output` 时，命令还会写出机器可读 JSON report，文件权限为 `0600`。该 report 会记录 job id、源集合、目标表、schema preflight 状态、可选 record-mapping summary metadata、向量维度、读取记录数和写入记录数，但不会包含 PostgreSQL 连接 URL。

迁移前如需验证 full-record mapping，请先对同一 schema plan 运行 `vdbg map-migration-records`。该命令只读取本地 artifact，不连接 Milvus 或 PostgreSQL。

## 可选 full-record mapping

使用 `--record-mapping` 可以让 `migrate` 消费 `vdbg map-migration-records` 的机器可读输出：

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --dimension 8 \
  --record-mapping /tmp/vdb-guardian-record-mapping.json
```

提供该参数后，mapping artifact 会成为源集合、目标表、主键、向量字段、scalar 列、dynamic metadata 和 partition metadata 的事实来源。如果 mapping status 不是 `pass`，如果 artifact 中不是恰好一个 collection mapping，或者缺少主键/向量 mapping，命令会在创建 runner 之前拒绝执行。读取 mapping artifact 是纯本地操作，不会连接 Milvus 或 PostgreSQL。

## 可选 schema preflight

使用 `--require-schema-match` 可以让 standalone migration 依赖 planned-vs-live schema drift gate：

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --source-collection items \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --dimension 1536 \
  --require-schema-match \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --output /tmp/vdb-guardian-migration-report.json \
  --job-id migration-smoke
```

启用 `--require-schema-match` 时，必须同时提供 `--schema-plan` 和 `--live-schema`。该命令复用 `vdbg compare-applied-schema` 的 artifact-only 对比逻辑；如果存在 blocking drift，迁移不会启动。

## 必填标志 (Required flags)

- `--milvus-address`: Milvus gRPC 终端点，例如 `localhost:19530`。
- `--pgvector-connection-url`: PostgreSQL 连接 URL。请在日志和文档中对该信息进行脱敏处理。
- `--dimension`: 预期的向量维度。运行器会针对该值校验每一个迁移的向量。

## 可选标志 (Optional flags)

- `--require-schema-match`: 要求 planned-vs-live schema 对比通过后才启动迁移。
- `--schema-plan`: pgvector schema plan JSON 路径。启用 `--require-schema-match` 时必填。
- `--live-schema`: live pgvector schema inspection JSON 路径。启用 `--require-schema-match` 时必填。
- `--record-mapping`: 可选的 `vdbg map-migration-records` JSON 路径，用于 mapping-driven full-record migration。artifact 必须为 `status: pass`，且只能包含一个 collection mapping。
- `--output`: 可选的 migration result report JSON 输出路径，文件权限为 `0600`。
- `--job-id`: 写入 migration result report 的可选任务标识。

## 默认值 (Defaults)

- `--source-collection`: `items`
- `--target-table`: `items`
- `--milvus-id-field`: `id`
- `--milvus-vector-field`: `embedding`
- `--pgvector-id-column`: `id`
- `--pgvector-vector-column`: `embedding`
- `--batch-size`: `100`

## 支持范围 (Scope)

`vdbg inspect-milvus` 可在记录迁移前根据 Milvus 元数据生成只读迁移规划 JSON 文档。`vdbg plan-pgvector-schema` 可进一步把 inspection plan 转换为 dry-run pgvector schema / DDL 规划，`vdbg compare-schema-plans` 会在任何 DDL apply 步骤前验证两份规划 artifact，`vdbg apply-pgvector-schema` 可 dry-run 或执行已经验证的 pgvector schema DDL，`vdbg inspect-pgvector-schema` 会在 apply 后 inventory live target schema，`vdbg compare-applied-schema` 会在记录迁移前验证 planned-vs-live schema drift。详见 `docs/zh-CN/inspect-milvus-cli.md`、`docs/zh-CN/plan-pgvector-schema-cli.md`、`docs/zh-CN/compare-schema-plans-cli.md`、`docs/zh-CN/apply-pgvector-schema-cli.md` 和 `docs/zh-CN/inspect-pgvector-schema-cli.md`。

目前已实现：

- 真实基于 Milvus SDK 的源端数据读取。
- 真实基于 pgx 的 pgvector 目标端 upsert（插入或更新）写入。
- 向量维度校验。
- CLI 标志解析与注入运行器的单元测试。
- 通过 `--require-schema-match` 可选执行 planned-vs-live schema preflight。
- 通过 `--output` 可选写出机器可读 migration result JSON report。
- 通过 `--record-mapping` 可选执行 mapping-driven full-record migration，从通过校验的本地 mapping artifact 迁移 scalar 字段、dynamic metadata 和 partition metadata。
- 包含已读取和已写入记录数的摘要输出。

尚未实现：

- 在此命令内生成源/目标指纹产物。
- 在此命令内生成比较结果产物。
- full-record equality comparison。
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
