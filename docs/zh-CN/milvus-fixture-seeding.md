# Milvus 测试数据灌入 (Milvus Fixture Seeding)

Milvus 测试数据灌入机制 (Fixture Seeding) 旨在为特定的 Milvus 集合准备确定性的合成向量记录。在 Milvus 到 pgvector 的迁移与验证 MVP 中，这是数据源端极其关键的数据准备步骤。

## 当前范围 (Current scope)

已实现的能力：

- 校验 Milvus 灌入器 (seeder) 配置的合法性。
- 校验合成测试固件 (synthetic fixture) 的维度及记录向量是否合规。
- 通过注入的适配器 (injected adapter)，构建出用于创建集合的最小边界。
- 通过注入的适配器，执行合成记录的写入操作。
- 通过 `vdbg seed-milvus` CLI 命令，利用真实的 Milvus Go SDK 向本地 Milvus 灌入数据。
- 返回结构化的灌入摘要，包含集合名、维度及成功写入的行数。

尚未实现：

- 针对 Docker 迁移技术栈的完整自动化集成测试。
- 分区 (Partitions) 及元数据字段 (metadata fields) 的支持。
- 查询向量 (Query vector) 的直接插入或保存。
- 针对生产环境的批量导入 (bulk import) 优化路径。

该灌入器的核心逻辑被巧妙地封装在了适配器驱动 (adapter-driven) 模式之后。这种设计确保了即便在不启动 Docker 且未连接真实 Milvus 服务的情况下，其内部行为逻辑依然能被充分进行单元测试。而对外暴露的真实 CLI，则会在适配器边界后方老老实实地调用官方的 Milvus Go SDK。

## API 设计 (API)

灌入器相关的代码位于 `internal/migration` 包内：

```go
type MilvusSeederConfig struct {
    Collection  string
    IDField     string
    VectorField string
    Dimension   int
    Metric      string
}

type MilvusSeeder struct {
    // 需通过 NewMilvusSeeder 函数进行构造
}

func NewMilvusSeeder(config MilvusSeederConfig, db milvusSeedDB) (MilvusSeeder, error)
func (s MilvusSeeder) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (MilvusSeedResult, error)
```

默认配置值：

```text
Collection:  items
IDField:     id
VectorField: embedding
Metric:      cosine
```

其中 `Dimension` (维度) 是必填项，且必须与正在处理的合成测试固件中声明的维度严格匹配。

## 适配器行为 (Adapter behavior)

灌入器会按照以下严谨的顺序调用注入的适配器：

```text
CreateCollection(集合名, ID字段名, 向量字段名, 维度, 距离指标)
InsertRecords(集合名, ID字段名, 向量字段名, 待插入记录集合)
```

当使用真实的 SDK 适配器时，这一边界会被展开为以下更为底层的具体操作：

```text
1. 检查是否存在该集合 (HasCollection)
2. 如果存在，则将其删除 (DropCollection)
3. 创建新集合 (CreateCollection)，并指定 VarChar 类型的自增主键与 FloatVector 字段
4. 基于 FLAT 算法及指定的 cosine/L2 距离指标，创建索引 (CreateIndex)
5. 将集合加载至内存中就绪 (LoadCollection)
6. 采用列式 (columnar) 格式批量插入 ID/向量 数据 (Insert)
7. 刷写持久化 (Flush)
```

在数据被移交给适配器之前，系统会对所有的记录执行一次拷贝 (copied)。这样做是为了防止调用方在后续不慎修改原始固件数据时，意外污染已经提交或正在处理中的批量写入批次。

## 验证与容错 (Validation)

灌入器会主动拒绝并拦截以下情况：

- 缺失必要的数据库适配器。
- `Dimension` 的值超出 `1..2000` 的合法范围。
- 使用了当前不支持的距离指标 (metrics)。
- `Dimension` 配置与实际数据集中声明的维度不匹配。
- 存在空的记录 ID。
- 记录中的向量长度与配置的 `Dimension` 不一致。
- 向量包含非数字 (`NaN`) 或无穷大 (infinite) 的非法值。
- 集合名称或字段名称使用了不安全的标识符。

标识符必须符合以下正则表达式规范：

```text
^[A-Za-z_][A-Za-z0-9_]*$
```

支持的标识符示例：

```text
items
id
embedding
items_2026
```

被拦截拒绝的非法示例：

```text
items;drop
public.items
"items"
items-name
```

## MVP 中的角色 (MVP role)

预期的完整迁移闭环如下：

```text
合成的测试固件记录 (synthetic fixture records)
        ↓
MilvusSeeder (执行源端灌入)
        ↓
Milvus 数据库内的 Collection
        ↓
MilvusConnector.Search(使用固件中的查询向量执行检索)
        ↓
生成源端指纹产物 (source fingerprint artifact)
```

配合 `pgvector` 端的灌入流程，该模块为后续正式引入完整的真实 SDK 适配器、Docker 集成测试以及最终的“一键迁移并验证” CLI 命令之前，在源端与目标端均铺设了非常稳固的数据准备基石。