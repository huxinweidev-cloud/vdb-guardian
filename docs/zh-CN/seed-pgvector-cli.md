# seed-pgvector CLI

`vdbg seed-pgvector` 命令负责将确定性的合成测试固件记录写入到基于 pgvector 扩展的 PostgreSQL 真实数据表中。

在 Milvus 向 pgvector 迁移并验证的 MVP 中，这是项目中**第一个能够对真实数据库产生写入行为**的命令。它**不会**自动启动 Docker 容器或创建后台服务；它仅仅依靠传入的连接 URL，尝试连接到一个已经处于运行状态的 PostgreSQL 实例。

## 命令用法 (Command)

```bash
go run ./cmd/vdbg seed-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --id-column id \
  --vector-column embedding
```

必填参数：

- `--fixture`: 合成测试固件 JSON 文件的相对/绝对路径。
- `--connection-url`: 目标 pgvector 数据库的 PostgreSQL 连接 URL。

可选参数：

- `--table`: 目标数据表名称，默认为 `items`。
- `--id-column`: 文本类型主键列的名称，默认为 `id`。
- `--vector-column`: 存储 pgvector 数据的列名称，默认为 `embedding`。

**严禁**将真实的连接 URL 提交到代码库中。在任何文档、Issue 评论以及可被共享的日志流中，均须使用 `[REDACTED]` 替代真实凭据进行脱敏。

## 行为逻辑 (Behavior)

该命令会依次执行以下操作：

1. 加载并解析合成测试固件 JSON 文件。
2. 提取固件内的 `dimension` (维度) 字段，并将其作为 pgvector 列的维度约束。
3. 实例化一个底层由 `pgx` 驱动的数据灌入适配器 (seeding adapter)。
4. 启动 `migration.PGVectorSeeder` 执行核心的灌入流程。
5. 在终端打印一份精简的灌入结果摘要。

灌入器在数据库底层执行的实际 SQL 如下：

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

```sql
CREATE TABLE IF NOT EXISTS "items" (
  "id" TEXT PRIMARY KEY,
  "embedding" vector(8) NOT NULL
);
```

```sql
INSERT INTO "items" ("id", "embedding")
VALUES ($1, $2::vector)
ON CONFLICT ("id")
DO UPDATE SET "embedding" = EXCLUDED."embedding";
```

得益于按 ID 执行的更新式写入 (`upsert`)，针对同一份固件重复执行该命令是绝对幂等 (idempotent) 的。

## 生产安全底线 (Safety)

在拼接并执行任何 SQL 之前，该命令会对目标表名和列名执行严格的安全校验。合法的标识符必须匹配以下正则表达式：

```text
^[A-Za-z_][A-Za-z0-9_]*$
```

它将主动拦截并拒绝类似以下的注入级或不合规名称：

```text
items;drop
public.items
"items"
items-name
```

向量数据会被编码为 pgvector 支持的文本字面量 (literals)。任何空向量、`NaN` 或是无穷大的数值都会被主动拒绝。

## 配合本地迁移技术栈的冒烟测试 (Local migration stack smoke check)

在明确批准启动 Docker 服务的前提下，您可以通过配合本地技术栈来验证该命令的可行性：

```bash
make migration-stack-config
docker compose -f deployments/docker-compose.migration.yml up -d postgres-pgvector
scripts/check-migration-stack.sh postgres
```

紧接着，使用仅限本地环境的连接 URL 执行数据灌入：

```bash
go run ./cmd/vdbg seed-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --id-column id \
  --vector-column embedding
```

随后，进入 PostgreSQL 容器内部手动校验数据行数与写入向量的实际维度：

```bash
docker compose -f deployments/docker-compose.migration.yml exec -T postgres-pgvector psql \
  -U vdb_guardian \
  -d vdb_guardian \
  -c "SELECT COUNT(*) AS seeded_records FROM items; SELECT id, vector_dims(embedding) AS dims FROM items ORDER BY id LIMIT 3;"
```

针对代码库中自带的 `testdata/migration/synthetic-small.json` 固件，预期结果应为：

```text
seeded_records = 100
first sample dimensions = 8
```

如果因为镜像仓库或网络代理等原因导致完整的 Milvus 技术栈无法顺利拉取 (pull) MinIO 或 Milvus 镜像，只要 `postgres-pgvector` 这个容器能启动，该 pgvector 的测试就依然可以独立运作。它能够单独校验目标端的数据写入链路，而无需强求源端 Milvus 立刻就绪。

## 当前局限性 (Current limitations)

已实现：

- 接入了由真实 `pgx` 驱动的 PostgreSQL 执行引擎。
- 合成测试固件加载逻辑打通。
- 自动完成 pgvector 扩展及极简数据表的创建。
- 记录的幂等更新写入 (Upsert)。
- 实现了 CLI 参数解析与完整的单元测试。
- 可搭配本地 Docker pgvector 环境执行手工冒烟验证。

尚未实现：

- 自动化的一键式 Docker 技术栈编排启动。
- 针对本地迁移栈的全自动化集成测试套件。
- 针对源端 Milvus 的真实 SDK 数据灌入命令 (已在其他文档中规划)。
- 一键式、端到端的 “迁移并验证” (migrate-and-verify) 命令。
- 自动构建如 HNSW 或 IVFFlat 等高级索引。
- 支持元数据列 (Metadata columns) 或复杂的 Schema 映射。

## MVP 中的角色 (MVP role)

该命令的问世，标志着目标端真实数据库的写入闭环已彻底打通：

```text
合成测试固件 JSON 文件
        ↓
使用 vdbg seed-pgvector 命令
        ↓
写入到 PostgreSQL pgvector 数据表
        ↓
调用 PGVectorConnector.Search (执行检索)
        ↓
生成目标端指纹产物 (target fingerprint artifact)
```

当源端的 Milvus 灌入命令以及最终的 migrate-and-verify CLI 完成后，这个从源到目标的跨库闭环链条才算真正闭合。