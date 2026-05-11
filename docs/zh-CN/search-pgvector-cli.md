# search-pgvector CLI

`vdbg search-pgvector` 命令负责对目标端的 pgvector 执行检索冒烟测试，检索的对象必须是事先通过合成固件灌入的数据。

该命令的功能边界被刻意收窄。它仅仅用于验证真实的 pgvector 连接器能否成功连接数据库、统计数据行数、执行标准化的向量检索，并针对固件中的单条查询打印出排名结果。它**不会**自动启动 Docker，也**不会**向数据库写入任何数据。

## 使用示例 (Example)

```bash
go run ./cmd/vdbg search-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

## 参数选项 (Options)

| 选项 (Option) | 是否必填 | 默认值 | 描述 (Description) |
| --- | --- | --- | --- |
| `--fixture` | 是 | 无 | 包含查询向量的合成测试固件 JSON 文件。 |
| `--connection-url` | 是 | 无 | 目标 pgvector 数据库的 PostgreSQL 连接 URL。 |
| `--table` | 否 | `items` | 待统计与检索的表名。 |
| `--top-k` | 否 | `3` | 业务侧可见的 TopK 结果数量。 |
| `--expand-k` | 否 | `5` | 用于边界冒烟测试的扩展结果数量。必须 `>= top-k`。 |
| `--query-index` | 否 | `0` | 固件查询列表中的从零开始的索引。 |
| `--metric` | 否 | `cosine` | pgvector 检索指标：`cosine` 或 `l2`。 |

## 输出示例 (Output)

该命令会在终端打印出一份精简、易读的摘要信息：

```text
pgvector search smoke ok
fixture: testdata/migration/synthetic-small.json
table: items
records_count: 100
query_id: query-000001
top_k: 3
expand_k: 5
hits: 5
hit rank=1 id=vec-000084 score=0.8460551500320435
```

注意：实际返回的命中记录数量 (`hits`) 取决于 `expand-k` 而非 `top-k`，这是因为指纹构建器需要这一扩展的边界窗口来执行差异分析。

## 配合本地迁移技术栈的冒烟测试 (Local migration stack smoke check)

在您明确允许执行 Docker 容器等副作用操作后，请先启动或复用本地的 pgvector 服务并灌入测试数据：

```bash
docker compose -f deployments/docker-compose.migration.yml up -d postgres-pgvector
scripts/check-migration-stack.sh postgres
go run ./cmd/vdbg seed-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --id-column id \
  --vector-column embedding
```

随后，执行该检索冒烟测试命令：

```bash
go run ./cmd/vdbg search-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

对于已提交在代码库中的 `synthetic-small.json`，预期的数据行数应为 `100`，且当传入 `--expand-k 5` 时，命令应精确输出 `5` 条命中记录。

## 生产安全底线 (Safety)

- 连接 URL 仅限在运行时作为参数传入，严禁提交到代码仓库中。
- 该命令仅执行**只读**操作：检查 pgvector 扩展状态、统计行数、以及向量检索。
- 该命令**绝不会**隐式地在后台帮您启动 Docker 容器。
- 该命令的职责仅局限于验证目标端 pgvector 的检索可用性；它并不能证明源端 Milvus 已就绪，也不能作为端到端迁移正确性的最终背书。

## 当前局限性 (Current limitations)

已实现：

- 接入了基于底层 `pgx` 驱动的真实 pgvector 连接器。
- 支持加载合成测试固件中的查询数据。
- 行数统计的冒烟测试。
- 针对单条查询的标准向量检索。
- 借助注入连接器工厂 (connector factory) 实现的单元测试。

尚未实现：

- 针对多条查询的批量冒烟测试。
- 根据 pgvector 的真实检索结果写入指纹产物 JSON。
- 针对 Docker 技术栈的自动化集成测试。
- 针对源端 Milvus 的真实检索冒烟测试打通。
- 一键式、端到端的 “迁移并验证” (migrate-and-verify) 命令。