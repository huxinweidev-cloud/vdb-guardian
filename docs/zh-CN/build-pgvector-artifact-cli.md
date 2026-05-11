# build-pgvector-artifact CLI

`vdbg build-pgvector-artifact` 命令负责遍历合成测试固件中的每一条查询，对真实的 pgvector 数据表执行检索，并将结果打包输出为一份与 Python 引擎完全兼容的指纹产物 JSON 文件。

在目标端，该命令充当着将真实数据库检索结果转化为引擎可读指纹的桥梁：

```text
合成测试固件的查询集合 (synthetic fixture queries)
    -> 调用 PGVectorConnector.Search 执行检索
    -> 交由 fingerprints.BuildArtifact 构建结构
    -> 最终生成 target-fingerprint.json 目标端产物
```

该命令执行纯读取操作，**不会**启动 Docker，也**不会**对数据库中的数据进行任何篡改。

## 使用示例 (Example)

```bash
go run ./cmd/vdbg build-pgvector-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --output /tmp/vdb-guardian-target-fingerprint.json \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

## 参数选项 (Options)

| 选项 (Option) | 是否必填 | 默认值 | 描述 (Description) |
| --- | --- | --- | --- |
| `--fixture` | 是 | 无 | 包含查询向量的合成测试固件 JSON 文件路径。 |
| `--connection-url` | 是 | 无 | 目标 pgvector 数据库的 PostgreSQL 连接 URL。 |
| `--output` | 是 | 无 | 目标端指纹产物 JSON 文件的预期输出路径。 |
| `--table` | 否 | `items` | 待检索的 pgvector 表名。 |
| `--top-k` | 否 | `3` | 可见的 TopK 数量，将写入 `top_k_ids` 字段。 |
| `--expand-k` | 否 | `5` | 底层 SQL 检索的上限 (LIMIT)。必须至少 `>= top-k + boundary-k`。 |
| `--stable-k` | 否 | `2` | 头部稳定结果数量，将写入 `stable_neighbors` 字段。必须 `<= top-k`。 |
| `--boundary-k` | 否 | `1` | 围绕 TopK 截断点两侧的排位窗口宽度。 |
| `--metric` | 否 | `cosine` | pgvector 检索指标：`cosine` 或 `l2`。 |

## 输出结构 (Output)

该命令会生成一份符合以下结构的 JSON 产物文件：

```json
{
  "fingerprints": [
    {
      "query_id": "query-000001",
      "stable_neighbors": ["vec-000033", "vec-000096"],
      "boundary_candidates": ["vec-000005", "vec-000012"],
      "top_k_ids": ["vec-000033", "vec-000096", "vec-000005"]
    }
  ]
}
```

并在终端打印摘要信息：

```text
pgvector fingerprint artifact written
fixture: testdata/migration/synthetic-small.json
output: /tmp/vdb-guardian-target-fingerprint.json
table: items
queries: 10
top_k: 3
expand_k: 5
stable_k: 2
boundary_k: 1
```

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

随后，执行该命令生成目标端指纹产物：

```bash
go run ./cmd/vdbg build-pgvector-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --output /tmp/vdb-guardian-target-fingerprint.json \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

校验生成的产物文件结构是否合法：

```bash
python -m json.tool /tmp/vdb-guardian-target-fingerprint.json >/dev/null
```

针对自带的小型固件，预期生成的产物中应完整包含 `10` 个查询级指纹。

## 生产安全底线 (Safety)

- 连接 URL 仅限在运行时作为参数传入，严禁提交到代码仓库中。
- 该命令在数据库层面仅执行**读取**操作，副作用仅限于在本地文件系统中输出产物文件。
- 该命令**绝不会**隐式地在后台帮您启动 Docker 容器。
- 生成的产物仅能代表目标端的检索行为画像；它不能证明源端 Milvus 已就绪，也不能作为端到端迁移正确性的最终背书。

## 当前局限性 (Current limitations)

已实现：

- 接入了基于底层 `pgx` 驱动的真实 pgvector 连接器。
- 支持基于合成固件执行多查询遍历检索。
- 连接器原生结果向指纹构建器入参的标准化转换。
- 生成并输出完全兼容 Python 的目标端指纹产物。
- 借助注入连接器工厂 (connector factory) 实现的单元测试。

尚未实现：

- 生成源端 (Milvus) 真实的指纹产物 (由专属命令负责)。
- 直接触发源端与目标端产物的比对引擎调用。
- 针对 Docker 技术栈的自动化集成测试。
- 一键式、端到端的 “迁移并验证” (migrate-and-verify) 命令。