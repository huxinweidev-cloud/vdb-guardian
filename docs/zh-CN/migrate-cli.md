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
  --pgvector-write-mode batch-upsert \
  --dimension 8 \
  --batch-size 100 \
  --require-schema-match \
  --schema-plan /tmp/vdb-guardian-pgvector-schema-plan.json \
  --live-schema /tmp/vdb-guardian-live-pgvector-schema.json \
  --record-mapping /tmp/vdb-guardian-record-mapping.json \
  --checkpoint-path /tmp/vdb-guardian-migration-checkpoint.json \
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

当提供 `--output` 时，命令还会写出机器可读 JSON report，文件权限为 `0600`。该 report 会记录 job id、源集合、目标表、schema preflight 状态、可选 record-mapping summary metadata、可选 checkpoint summary、向量维度、读取记录数、写入记录数和 pgvector write-mode metrics，但不会包含 PostgreSQL 连接 URL。

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

## 可选 checkpoint / resume

使用 `--checkpoint-path` 可以在每个成功的 pgvector 写入批次后写出批次级进度；如果某个写入批次失败，也会先写出 failed checkpoint 再返回错误：

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --dimension 1536 \
  --batch-size 1000 \
  --checkpoint-path /secure/artifacts/migration-checkpoint.json
```

使用 `--resume-from` 可以从 failed 或 running checkpoint 继续：

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --dimension 1536 \
  --batch-size 1000 \
  --resume-from /secure/artifacts/migration-checkpoint.json
```

如果提供 `--resume-from` 但没有提供 `--checkpoint-path`，命令会把后续进度继续写回同一个 checkpoint 文件。Resume 校验会 fail closed：当 checkpoint 中的源集合、目标表、维度、batch size、schema-plan fingerprint、record-mapping fingerprint 或状态不安全时，会拒绝继续。已经 completed 的 checkpoint 不能用于 resume。

Checkpoint 文件权限为 `0600`。其中只包含非敏感的迁移身份、记录计数、完成/失败批次范围和 resume offset；不会包含 PostgreSQL connection URL、凭据、token、原始向量或行 payload。

MVP 限制：源端 Milvus reader 仍会先读取源结果集，然后再执行 pgvector 批量写入。当前 checkpoint 保护的是目标端写入批次进度和 resume offset；source cursor/page-level streaming 仍是后续工作。

## 可选 pgvector COPY 写入模式

使用 `--pgvector-write-mode` 可以选择每个迁移批次写入 pgvector 的方式：

```bash
go run ./cmd/vdbg migrate \
  --milvus-address localhost:19530 \
  --pgvector-connection-url '[REDACTED]' \
  --dimension 1536 \
  --batch-size 1000 \
  --pgvector-write-mode copy \
  --checkpoint-path /secure/artifacts/migration-checkpoint.json \
  --output /secure/artifacts/migration-report.json
```

支持的取值：

- `batch-upsert`：默认值，也是最安全的 legacy 行为。writer 对批次内记录使用参数化 `INSERT ... ON CONFLICT DO UPDATE` upsert。
- `copy`：显式 bulk 路径。writer 在事务中创建 staging table，通过 PostgreSQL `COPY` 流式写入批次，然后 merge/upsert 到目标表。显式 `copy` **不会** fallback 到 batch upsert；任何 COPY、schema、validation 或 context 失败都会让该批次失败。
- `auto`：先尝试同一 COPY 路径，仅当失败被判定为可恢复的 COPY execution failure 时，才 fallback 到 `batch-upsert`。该 fallback classifier 是保守的，不会对 validation error、schema error、不安全 identifier、context cancellation 或 deadline expiry 执行 fallback。

所有模式下 checkpoint 安全语义不变：只有整个目标端写入批次成功后，runner 才会推进批次进度。如果 COPY/staging/merge 失败，该批次会记录为 failed，不会写入该批次的 completed checkpoint。

Migration report 会包含这些写入指标：

```json
{
  "write_mode_requested": "auto",
  "write_mode_used": "mixed",
  "copy_batches": 2,
  "batch_upsert_batches": 1,
  "copy_fallbacks": 1
}
```

`write_mode_requested` 是 CLI 请求的模式。`write_mode_used` 是实际模式的聚合值：所有 completed batch 都走 COPY 时为 `copy`，都走 batch upsert 时为 `batch-upsert`，同时使用两条路径时为 `mixed`（例如发生 fallback 的 `auto` 运行）。`copy_batches`、`batch_upsert_batches` 和 `copy_fallbacks` 分别统计 completed batch 的写入路径和可恢复 auto fallback 次数。

## 必填标志 (Required flags)

- `--milvus-address`: Milvus gRPC 终端点，例如 `localhost:19530`。
- `--pgvector-connection-url`: PostgreSQL 连接 URL。请在日志和文档中对该信息进行脱敏处理。
- `--dimension`: 预期的向量维度。运行器会针对该值校验每一个迁移的向量。

## 可选标志 (Optional flags)

- `--require-schema-match`: 要求 planned-vs-live schema 对比通过后才启动迁移。
- `--schema-plan`: pgvector schema plan JSON 路径。启用 `--require-schema-match` 时必填。
- `--live-schema`: live pgvector schema inspection JSON 路径。启用 `--require-schema-match` 时必填。
- `--record-mapping`: 可选的 `vdbg map-migration-records` JSON 路径，用于 mapping-driven full-record migration。artifact 必须为 `status: pass`，且只能包含一个 collection mapping。
- `--pgvector-write-mode`: pgvector 批次写入模式：`batch-upsert`、`copy` 或 `auto`。
- `--checkpoint-path`: 可选的 migration checkpoint JSON 路径，文件权限为 `0600`。
- `--resume-from`: 可选的 checkpoint JSON 路径。若省略 `--checkpoint-path`，后续进度会默认写回同一个文件。
- `--output`: 可选的 migration result report JSON 输出路径，文件权限为 `0600`。
- `--job-id`: 写入 migration result report 和 checkpoint artifact 的可选任务标识。

## 默认值 (Defaults)

- `--source-collection`: `items`
- `--target-table`: `items`
- `--milvus-id-field`: `id`
- `--milvus-vector-field`: `embedding`
- `--pgvector-id-column`: `id`
- `--pgvector-vector-column`: `embedding`
- `--batch-size`: `100`
- `--pgvector-write-mode`: `batch-upsert`

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
- 通过 `--checkpoint-path` 和 `--resume-from` 可选执行 batch-level checkpoint/resume。
- 通过 `--pgvector-write-mode batch-upsert|copy|auto` 可选选择 pgvector 写入模式，包括 COPY report metrics 和保守 auto fallback。
- 包含已读取和已写入记录数的摘要输出。

尚未实现：

- 在此命令内生成源/目标指纹产物。
- 在此命令内生成比较结果产物。
- 在此命令内执行 full-record equality comparison；如需编排式 gate，请使用 `vdbg migrate-and-verify --full-record-compare`。
- 在此命令内执行 source/target reconciliation；如需显式 stale-row 工作流，请使用 `vdbg reconcile-target` 和 `vdbg cleanup-target-stale`。

## 安全提示

在明确加入生产级别的迁移语义之前，请仅针对本地迁移 MVP 服务或临时测试数据库运行此命令。

pgvector 写入器采用 upsert 语义：

```sql
INSERT ... ON CONFLICT (id) DO UPDATE
```

它**不会**删除目标表。如果目标表中包含 Milvus 源中不存在的旧记录，这个命令不会将其删除。

如果 `--pgvector-write-mode copy` 因 validation、schema、unsafe identifier 或 context 错误失败，不会 fallback。请修复 schema/mapping/context 问题，或改用 `--pgvector-write-mode batch-upsert` 重新运行。如果 `--pgvector-write-mode auto` 仍然失败，请将 fallback classifier 视为有意保守，并先检查脱敏后的错误，再判断是否适合使用 batch upsert。

Checkpoint 和 report artifact 可能包含 collection/table 名称、ID/range 和 artifact 路径。请只写入经过批准的安全目录，不要把生成的 artifact 内容贴到聊天或日志里。不要把凭据或 connection URL 放进 checkpoint 路径或 job id。

## 测试命令

```bash
go test ./internal/migration -run 'Test.*Checkpoint|Test.*Resume|TestVectorMigrationRunner' -v
go test ./cmd/vdbg -run 'TestParseMigrate|TestRunMigrate' -v
```

提交前完整检查：

```bash
make fmt
make lint
make test
make coverage-check
git diff --check
```

对于迁移关键路径变更，还应运行 opt-in 本地 Docker 冒烟：

```bash
make smoke-migration-checkpoint
```

该冒烟会启动/检查一次性迁移栈，seed 已提交的小型 Milvus fixture，执行 schema/mapping gates，运行带 checkpoint 的迁移，再通过 `migrate-and-verify` 走 resume 路径，验证目标端 100 行数据、`0600` report/checkpoint 权限，并扫描生成 artifact 中的明显 secret marker。它依赖 Docker 和本地端口，因此不会放入默认 `make test`；不要把它指向生产数据库。

针对 opt-in pgvector COPY 冒烟，运行：

```bash
make smoke-migration-copy
```

该 smoke 会启动/检查一次性迁移栈，通过 `vdbg migrate` 运行 COPY-mode migration，然后单独构建 full-record artifacts、执行 compare，并运行 target reconciliation。它会验证 COPY metrics、full-record compare、`stale_target_count=0` 的 target reconciliation、`0600` artifacts，以及生成 artifact 的 secret scan。Connection URL 和 secrets 不会被打印；生成的 smoke artifacts 会保留在 `/tmp` 下便于本地排查。它依赖 Docker 和本地端口，不要把它指向生产数据库。

针对 target reconciliation 和受保护的 stale cleanup，运行：

```bash
make smoke-target-reconciliation-cleanup
```

该 smoke 会 seed 一条仅存在于 pgvector 目标端的 row，验证 `stale_target_count=1`，执行 `cleanup-target-stale --confirm-delete-stale`，然后重新构建/比对目标端 artifact 并验证 `stale_target_count=0`。
