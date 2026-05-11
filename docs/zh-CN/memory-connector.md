# 内存连接器 (Memory Connector)

内存连接器 (Memory Connector) 是一种专为本地验证和测试设计的确定性 (deterministic) 连接器实现。它实现了通用的 `internal/connectors.Connector` 接口，但在这个过程中不会产生与真实向量数据库的任何网络交互。

## 目的 (Purpose)

在 Milvus 和 pgvector 真实连接器落地之前，该连接器赋予了项目对连接器行为规范化 (connector normalization) 进行独立验证的能力：

```text
预计算好的排序命中结果 (precomputed ranked hits)
        |
        v
MemoryConnector.Search (执行搜索)
        |
        v
返回 connectors.SearchResponse
        |
        v
指纹产物构建器 (fingerprint artifact builder)
        |
        v
验证运行器 (verification runner)
```

这种架构设计确保了本地端到端验证 (local end-to-end verification) 的纯粹性，使其完全隔离于 Docker 环境、SDK 变更、SQL 语法错误及网络层面的故障。

## 构造方式 (Construction)

```go
connector := connectors.NewMemoryConnector("memory-source", map[string][]connectors.SearchHit{
    "q-1": {
        {ID: "a", Rank: 1, Score: 0.99},
        {ID: "b", Rank: 2, Score: 0.95},
        {ID: "c", Rank: 3, Score: 0.90},
    },
})
```

构造器 (constructor) 内部会对传入的命中数据执行深度拷贝 (deep-copies) 与排名排序 (rank-sorts)。这样可以确保外部调用方对其持有的数据进行变动时，不会对连接器内部的状态与行为产生任何副作用。

## 搜索行为 (Search behavior)

目前的内存连接器将 `SearchRequest.Collection` 作为查询键 (query key) 来使用。这只是针对本地验证阶段的一种临时约定；在后续真实的连接器中，搜索行为将严格依赖 `QueryVector` 对数据库中的对应集合执行匹配。

```go
response, err := connector.Search(ctx, connectors.SearchRequest{
    Collection: "q-1",
    TopK:       2,
    ExpandK:    3,
})
```

连接器将返回按排名升序排列的前 `ExpandK` 条命中记录。

## 验证规则 (Validation)

`Search` 方法会主动拒绝以下情况：

- 上下文被取消 (canceled contexts)；
- `TopK <= 0`；
- `ExpandK <= 0`；
- `ExpandK < TopK`；
- 空的集合名或查询键；
- 找不到对应的查询键；
- 对应查询键的命中结果少于 `ExpandK` 指定的数量。

## 生命周期方法 (Lifecycle methods)

连接器完整实现了 `Connect`、`Count`、`Search` 及 `Close` 方法，以此满足企业级连接器契约 (enterprise connector contract) 的要求。需要注意的是，`Connect` 和 `Close` 被调用时不会触发任何实际的网络 I/O 操作。

## 局限性 (Limitations)

内存连接器并不是真实的迁移源或目标。它的使命仅局限于提供确定性的本地验证、单元测试，以及在真实的 Milvus 和 pgvector 接入之前充当离线端到端测试 (offline end-to-end tests) 的桥梁。