# pgvector 连接器 (pgvector Connector)

轻量级的 pgvector 连接器实现了通用的 `connectors.Connector` 接口，专门用于安装了 pgvector 扩展的 PostgreSQL 数据库。它是 Milvus 向 pgvector 迁移并验证的最简可行产品 (MVP) 中的核心组件。

## 当前范围 (Current scope)

已实现的能力：

- 校验连接器配置。
- 通过 `pgx` 依据连接 URL (connection URL) 创建 PostgreSQL 适配器。
- 执行 PostgreSQL 数据库的连通性测试 (Ping)。
- 检查数据库中是否已安装 `vector` 扩展。
- 统计指定配置表中的数据行数。
- 使用 `TopK` / `ExpandK` 参数执行向量搜索请求。
- 返回标准化的 `connectors.SearchResponse`。

尚未实现：

- 自动化的 Schema/表创建机制。
- 向 pgvector 灌入测试固件数据 (fixture seeding)。
- 元数据过滤 (Metadata filters)。
- 包含 Schema 限定前缀的标识符 (Schema-qualified identifiers) 支持。
- HNSW / IVFFlat 索引的自动化管理。
- 针对运行中 Docker 环境的集成测试。

## 配置 (Configuration)

该连接器通过 `PGVectorConfig` 进行配置：

```go
type PGVectorConfig struct {
    Name          string
    ConnectionURL string
    DefaultTable  string
    IDColumn      string
    VectorColumn  string
    Metric        string
}
```

默认值：

```text
Name:         pgvector
DefaultTable: items
IDColumn:     id
VectorColumn: embedding
Metric:       cosine
```

`ConnectionURL` 应由本地配置或运行时参数提供。请勿将真实的凭证信息提交到版本库中。

## 搜索行为 (Search behavior)

连接器将 `SearchRequest.Collection` 的值作为表名。如果该字段为空，则回退使用 `DefaultTable`。

对于余弦 (cosine) 相似度搜索：

```sql
SELECT id, 1 - (embedding <=> $1::vector) AS score
FROM items
ORDER BY embedding <=> $1::vector
LIMIT $2;
```

对于 L2 距离搜索：

```sql
SELECT id, -(embedding <-> $1::vector) AS score
FROM items
ORDER BY embedding <-> $1::vector
LIMIT $2;
```

连接器将 `ExpandK` 参数映射为 SQL 语句中的 `LIMIT` 条件，这样指纹构建器就能观测到 topK 截断点附近的边界候选者 (boundary candidates)。

## 安全的 SQL 标识符 (Safe SQL identifiers)

PostgreSQL 的表名和列名无法作为 SQL 参数进行传递。因此，该连接器严防 SQL 注入，仅接受最简单的标识符：

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
public.items
items;drop
"items"
```

对于带 Schema 限定前缀的名称 (Schema-qualified names) 或带引号的标识符 (quoted identifiers)，将在后续通过更明确的结构化配置来支持。

## 向量字面量 (Vector literals)

初版实现在执行查询时，会将查询向量格式化为 pgvector 支持的文本字面量：

```text
[0.1,0.2,0.3]
```

所有的向量值必须为有限数值。空向量 (empty vectors)、非数字 (NaN) 及无穷大 (Inf) 将在执行 SQL 之前被主动拦截并拒绝。

## MVP 中的角色 (MVP role)

该连接器在迁移 MVP 中担任目标端的搜索连接器。预期的后续工作流程如下：

```text
合成的测试固件记录 (synthetic fixture records)
        ↓
灌入 (seed) pgvector 表
        ↓
执行搜索 (query vectors)
        ↓
返回 connectors.SearchResponse
        ↓
指纹产物构建器 (fingerprint artifact builder)
        ↓
验证运行器 (verification runner)
```