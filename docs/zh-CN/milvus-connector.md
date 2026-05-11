# Milvus 连接器 (Milvus Connector)

轻量级的 Milvus 连接器实现了通用的 `connectors.Connector` 接口，在 Milvus 向 pgvector 迁移的最简可行产品 (MVP) 中，主要负责在数据源端采集检索行为。

## 当前范围 (Current scope)

已实现的能力：

- 校验 Milvus 连接器配置。
- 根据提供的地址创建真实的 Milvus Go SDK 适配器。
- 通过极简的适配器边界，实现连接、计数、搜索和关闭操作。
- 通过 SDK 提供的集合统计信息 (collection statistics)，读取 Milvus 集合的行数 (`row_count`)。
- 通过 SDK 执行单查询向量搜索，并将 Milvus 的命中结果转换为标准化的 `connectors.SearchResponse`。
- 标准化指标得分的方向，确保较大的 `SearchHit.Score` 代表更优的匹配结果。

尚未实现：

- 通过连接器包创建集合。
- 通过连接器包编排索引的创建或加载。
- 通过 CLI 向 Milvus 灌入测试固件数据 (fixture seeding)。
- 元数据过滤 (Metadata filters) 或 Milvus 布尔表达式支持。
- 针对 Docker 迁移技术栈的集成测试。

SDK 适配器被刻意设计得非常轻量，并隐藏在内部适配器边界之后。这种设计使得连接器的结果标准化、SDK 请求/结果转换等逻辑，均能在脱离 Docker 和网络状态的情况下进行单元测试。

## 配置 (Configuration)

该连接器通过 `MilvusConfig` 进行配置：

```go
type MilvusConfig struct {
    Name              string
    Address           string
    DefaultCollection string
    IDField           string
    VectorField       string
    Metric            string
}
```

默认值：

```text
Name:              milvus
DefaultCollection: items
IDField:           id
VectorField:       embedding
Metric:            cosine
```

本地 Docker 环境默认将 Milvus 暴露在 `localhost:19530`，但在生产或共享环境中，必须通过运行时配置提供相应的地址。请勿提交真实的凭证信息或私有服务端点 (endpoints)。

## 搜索行为 (Search behavior)

该连接器将通用的 `SearchRequest` 映射为 Milvus 适配器的请求格式：

```text
SearchRequest.Collection  -> 集合名称，若为空则使用 DefaultCollection
SearchRequest.QueryVector -> 查询向量
SearchRequest.ExpandK     -> Milvus 搜索上限 (limit)
SearchRequest.Params      -> 预留用于特定连接器的搜索参数
```

真实的 SDK 适配器目前使用了 Milvus SDK 中的 `IndexFlatSearchParam`，并针对单个查询向量返回一组结果。高级的 Milvus 搜索参数、元数据过滤、分区查询以及多查询批处理 (multi-query batching) 将被暂缓实现，直到源端的“种子灌入/搜索”循环 (seed/search loop) 足够稳定。

连接器按照 Milvus 返回结果的顺序，返回经过排序的命中记录：

```go
SearchResponse{
    Hits: []SearchHit{
        {ID: "vec-000001", Rank: 1, Score: 0.98},
    },
}
```

搜索上限参数采用了 `ExpandK` 而非 `TopK`，这样指纹构建器就能观测到业务 `TopK` 截断值附近的边界候选者 (boundary candidates)。

## 得分标准化 (Score normalization)

整个项目范围内通用的标准化得分规则是：

```text
SearchHit.Score 的值越大，表示匹配度越好
```

Milvus 的指标处理逻辑：

```text
cosine: 直接透传原得分
ip:     直接透传原得分
l2:     将距离 (distance) 转换为负分 (negative score)
```

这样可以确保 Milvus 的输出结果能与 pgvector 以及未来的其他连接器保持可比性。

## 安全标识符 (Safe identifiers)

Milvus 的集合和字段名称受到严格限制，仅允许使用简单的标识符：

```text
^[A-Za-z_][A-Za-z0-9_]*$
```

支持的示例：

```text
items
embedding
id
```

被拒绝的示例：

```text
items;drop
public.items
"items"
```

尽管 Milvus 使用的并非 SQL，但拒绝不安全的动态名称可以防止 SDK 的意外误用，并保持配置行为的可预测性。

## MVP 中的角色 (MVP role)

该连接器在迁移 MVP 中担任数据源端的搜索连接器。预期的后续工作流程如下：

```text
合成的测试固件记录 (synthetic fixture records)
        ↓
灌入 (seed) Milvus 集合
        ↓
Milvus 执行搜索 (query vectors)
        ↓
返回 connectors.SearchResponse
        ↓
指纹产物构建器 (fingerprint artifact builder)
        ↓
验证运行器 (verification runner)
```

在引入真实的 SDK 适配器并实现数据灌入之后，该连接器将把源端的检索行为数据输送到产物比对流水线中——这正是目前本地离线验证已经在使用的流程。