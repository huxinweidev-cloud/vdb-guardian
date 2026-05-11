# 合成向量测试固件 (Synthetic Vector Fixtures)

合成向量测试固件 (Synthetic vector fixtures) 为 Milvus 向 pgvector 迁移并验证的 MVP (最简可行产品) 提供了确定性的数据集。这种设计有效避开了真实的客户数据，并且使得本地的集成测试能够在不同机器间被完美复现。

## 生成命令 (Generator command)

执行以下命令来生成默认的小型测试固件：

```bash
go run ./cmd/vdbg generate-synthetic-fixture \
  --output testdata/migration/synthetic-small.json \
  --seed 42 \
  --dimension 8 \
  --records 100 \
  --queries 10 \
  --metric cosine
```

该命令仅负责生成 JSON 文件，它不会启动 Docker，不会连接 Milvus，也不会连接 PostgreSQL。

## JSON 格式 (JSON format)

```json
{
  "seed": 42,
  "dimension": 8,
  "record_count": 100,
  "query_count": 10,
  "metric": "cosine",
  "records": [
    {"id": "vec-000001", "vector": [0.1, 0.2]}
  ],
  "queries": [
    {"id": "query-000001", "vector": [0.3, 0.4]}
  ]
}
```

`records` (记录) 是预期被灌入到源 Milvus 集合中，并在稍后迁移至 pgvector 的数据。`queries` (查询) 则是用来在两个数据库上分别执行检索，以收集可用于横向比对的搜索结果。

## 支持的维度 (Supported dimensions)

初版迁移 MVP 支持并验证的向量维度范围为：

```text
1..2000
```

其上限 (2000) 与 pgvector 标准的 `vector` 类型兼容性边界保持一致。生成器默认采用 `8` 维向量，因为在串联最初的迁移闭环时，小型向量更容易通过肉眼进行调试和核对。

推荐的维度使用阶段：

| 阶段 (Stage) | 维度 (Dimension) | 目的 (Purpose) |
| --- | ---: | --- |
| MVP 调试 | 8 或 16 | 实现快速的本地验证循环，保证固件数据肉眼可读 |
| 基础真实性校验 | 128 | 针对真实的连接器进行早期验证 |
| 嵌入模型模拟 | 384 / 768 / 1536 | 模拟常见的 Embedding 模型维度 |
| 边界极限测试 | 2000 | 验证 pgvector `vector` 类型的存储上限边界 |

## 距离指标 (Metrics)

目前支持的距离测算指标：

- `cosine`: 向量数据会被生成器自动执行 L2 归一化 (L2-normalized) 处理。
- `l2`: 向量数据保持未归一化状态，专门用于欧氏距离 (Euclidean-distance) 测试。

## 确定性 (Determinism)

生成器依赖于固定的随机数种子 (fixed seed)。只要使用相同的参数选项再次运行相同的命令，必然会生成完全相同的记录和查询数据。这一特性对于在不同的代码提交 (commits) 和不同的开发环境间比对迁移结果至关重要。

## 当前局限性 (Current limitations)

目前该固件生成器尚未集成直接向 Milvus 或 pgvector 灌入数据的功能。真实的数据库数据灌入器 (seeders) 和连接器测试将在 MVP 后续的迭代中负责消费这些固件数据。