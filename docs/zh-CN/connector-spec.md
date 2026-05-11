# 连接器规范 (Connector Specification)

连接器负责对各种不同向量数据库的行为进行规范化 (normalize)，以便 Go 控制平面 (control plane) 和 Python 指纹引擎 (fingerprint engine) 能够以统一的方式与其进行交互。

## 行为要求 (Required behavior)

任何一个连接器都必须具备以下能力：

1. 建立带有上下文取消机制 (context cancellation) 的数据库连接。
2. 统计指定集合 (collection) 或表中的数据行数。
3. 执行规范化的向量搜索请求。
4. 返回带有稳定标识符、排名、得分以及可选元数据 (metadata) 的命中结果 (ranked hits)。
5. 安全地关闭并释放资源。

## 规范化规则 (Normalization rule)

严禁将特定于某一数据库的 SDK 对象 (SDK objects) 泄露到核心业务包中。无论是 Milvus、pgvector，还是未来的其他连接器，都必须在适配器边界内将原生的检索结果转换为统一的 `SearchResponse` 结构，然后再将其移交给系统的其他组件。

## 规划中的连接器 (Future connectors)

未来计划支持的连接器包括：

- Milvus
- pgvector
- Qdrant
- Weaviate
- Elastic/OpenSearch
- Pinecone