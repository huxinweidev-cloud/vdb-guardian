# search-milvus CLI

`vdbg search-milvus` 命令用于验证源端 Milvus 的读取链路。它会统计已灌入数据的集合行数，并针对合成测试固件中的一条特定查询向量执行搜索。

在 Milvus 向 pgvector 迁移并验证的 MVP 中，这是一个专门用于源端的只读冒烟测试命令。

## 命令用法 (Command)

```bash
go run ./cmd/vdbg search-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

## 前置条件 (Prerequisites)

在执行检索之前，请务必先向 Milvus 集合灌入测试数据：

```bash
go run ./cmd/vdbg seed-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --metric cosine
```

该检索命令**不会**自动启动 Docker，也**不会**主动创建集合。

## 行为逻辑 (Behavior)

该命令会依次执行以下操作：

1. 加载合成测试固件的 JSON 文件。
2. 根据 `--query-index` 提取指定的查询。
3. 通过连接器 SDK 适配器连接到真实的 Milvus 实例。
4. 统计目标集合的记录行数。
5. 将 `expand-k` 作为 SDK 的搜索上限 (limit)，对选定的查询向量执行检索。
6. 将行数统计结果及按排名排序的命中记录打印到终端。

`top-k` 是业务侧可见的比较窗口；而 `expand-k` 是一个更宽广的边界观测窗口，用于后续生成指纹产物。

## 输出示例 (Output)

```text
milvus search smoke ok
fixture: testdata/migration/synthetic-small.json
collection: items
records_count: 100
query_id: query-000001
top_k: 3
expand_k: 5
hits: 5
hit rank=1 id=vec-000033 score=0.8164
```

Milvus 连接器会在底层统一得分标准：**得分越大，表示匹配度越好**。对于 L2 距离搜索，连接器会自动将距离值转换为负分。

## 命令行参数 (Flags)

| 参数 (Flag) | 默认值 | 描述 (Description) |
| --- | --- | --- |
| `--fixture` | 必填 | 合成测试固件的 JSON 文件路径。 |
| `--address` | 必填 | Milvus gRPC 服务地址，例如 `localhost:19530`。 |
| `--collection` | `items` | 待统计与检索的 Milvus 集合名称。 |
| `--id-field` | `id` | 检索结果中应返回的主键字段名。 |
| `--vector-field` | `embedding` | 执行检索的 FloatVector 向量字段名。 |
| `--top-k` | `3` | 业务侧可见的 TopK 结果数量。 |
| `--expand-k` | `5` | 用于边界冒烟测试的扩展结果数量。必须 `>= top-k`。 |
| `--query-index` | `0` | 固件查询列表中的从零开始的索引。 |
| `--metric` | `cosine` | 检索所使用的指标，目前支持 `cosine` 或 `l2`。 |

## 当前局限性 (Current limitations)

- 每次只能针对固件中的**一条**查询执行检索。
- 该命令不会构建指纹产物；如需基于所有查询生成指纹产物，请使用即将推出的 Milvus 产物构建 CLI。
- 深度依赖由 `seed-milvus` 或类似初始化流程所预先创建的集合。