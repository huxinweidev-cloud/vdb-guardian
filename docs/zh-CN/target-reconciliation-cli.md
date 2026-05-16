# Target reconciliation 与 stale cleanup CLI

`vdbg reconcile-target` 和 `vdbg cleanup-target-stale` 提供“先审计、再显式删除”的安全流程，用于处理 upsert 式 Milvus → pgvector 迁移后，pgvector 中已经不再存在于 Milvus 源端的旧行。

两个命令刻意拆开：

1. `reconcile-target` 只读取本地 artifact，不连接 Milvus 或 pgvector。
2. `cleanup-target-stale` 是破坏性命令，必须显式确认，并且只删除 reconciliation report 中被分类为 stale 的 ID。

## Reconcile target artifacts

先构建 source/target full-record artifacts，再在本地做 reconciliation：

```bash
go run ./cmd/vdbg reconcile-target \
  --source /tmp/vdb-guardian-source-full-records.json \
  --target /tmp/vdb-guardian-target-full-records.json \
  --output /tmp/vdb-guardian-target-reconciliation.json
```

`reconcile-target` 会写出权限为 `0600` 的 JSON report，包含：

- `schema_version`
- `status`: `pass` 或 `fail`
- 从 full-record artifacts 复制的 `source` / `target` endpoint metadata
- `summary`: source、target、matched、stale、missing、changed 数量
- `stale_target_ids`
- `missing_target_ids`
- `changed_record_ids`

当存在 stale、missing 或 changed records 时，命令会返回非零退出码，但仍会先写出 report，便于排查和后续显式 cleanup。所有 ID list 都会排序，保证 diff 稳定。

## Cleanup stale target rows

只有 stale target IDs 可以被删除。Missing target IDs 和 changed record IDs 永远不会被该命令删除。

```bash
go run ./cmd/vdbg cleanup-target-stale \
  --reconcile-report /tmp/vdb-guardian-target-reconciliation.json \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --target-id-column id \
  --output /tmp/vdb-guardian-target-stale-cleanup.json \
  --confirm-delete-stale
```

必填参数：

- `--reconcile-report`: target reconciliation report JSON 路径。
- `--pgvector-connection-url`: PostgreSQL/pgvector connection URL。日志、文档、工单和 PR 中必须脱敏。
- `--target-table`: 要删除 stale rows 的 pgvector 目标表。
- `--output`: cleanup result JSON 路径。
- `--confirm-delete-stale`: 显式确认破坏性删除。

可选参数：

- `--target-id-column`: pgvector 目标 ID 列，默认 `id`。

Cleanup result 会以 `0600` JSON 写出，包含 target table、请求删除数量、实际删除数量和 deleted stale IDs，不会保存 connection URL。

## 安全边界

- Reconciliation 是只读、artifact-only。
- 当存在 stale IDs 时，如果没有 `--confirm-delete-stale`，cleanup 会 fail closed。
- Cleanup 只消费 report 中的 `stale_target_ids`。
- Cleanup 在执行 DML 前会校验 report schema、status、计数一致性和 source/target 聚合数量。
- pgvector 删除会校验/引用 identifier，并将 stale IDs 作为数组参数绑定，不会拼接进 SQL。
- 连接和删除错误会脱敏，避免泄露 connection URL、password 或 stale IDs。
- Reconciliation reports 和 cleanup results 都是敏感本地产物，写入权限为 `0600`。

## 本地 Docker smoke

针对 migration-critical reconciliation/cleanup 改动，运行 opt-in 本地 smoke：

```bash
make smoke-target-reconciliation-cleanup
```

该 smoke 会启动/检查一次性 migration stack，将提交的小 fixture seed 到 Milvus 与 pgvector，插入一条仅存在于目标端的 pgvector row，构建 source/target full-record artifacts，验证 `stale_target_count=1`，显式执行 stale cleanup，验证 pgvector 行数回到 `100`，重新构建并 reconcile target artifacts，验证 `stale_target_count=0`，检查 artifact 权限 `0600`，并扫描生成产物中的明显 secret marker。

该 smoke 需要 Docker 和本地端口，因此不会放进普通 `make test`。不要把它指向生产数据库。
