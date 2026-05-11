# seed-milvus CLI

`vdbg seed-milvus` 命令通过官方的 Milvus Go SDK，将确定性的合成向量记录灌入到真实的 Milvus 集合中。

在 Milvus 向 pgvector 迁移并验证的 MVP 中，这是一个专门用于打通源端数据写入链路的核心命令。

## 命令用法 (Command)

```bash
go run ./cmd/vdbg seed-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --metric cosine
```

## 行为逻辑 (Behavior)

该命令会依次执行以下操作：

1. 加载合成测试固件的 JSON 文件。
2. 从固件对象的 `dataset.dimension` 字段推断出 Milvus 集合所需的向量维度。
3. 连接到指定且已处于运行状态的 Milvus 服务端点。
4. 如果目标集合已存在，**执行删除操作 (Drop)**。
5. 创建一个具备以下特性的极简集合：
   - 包含一个 `VarChar` 类型的主键字段；
   - 包含一个 `FloatVector` 类型的向量字段；
   - 关闭主键自动生成 (`AutoID=false`)。
6. 根据请求的距离指标，为集合创建 `FLAT` 算法的向量索引。
7. 将该集合加载 (Load) 进 Milvus 内存以准备读写。
8. 采用列式 (columnar) 格式，将固件中的所有记录插入 Milvus。
9. 刷写持久化 (Flush) 集合数据。
10. 打印一份精简的数据灌入摘要。

## 输出示例 (Output)

```text
milvus fixture seeded
fixture: testdata/migration/synthetic-small.json
collection: items
dimension: 8
records_total: 100
records_seeded: 100
```

## 生产安全底线 (Safety)

`seed-milvus` 每次执行都会**彻底重建**目标集合。这是一种刻意设计的**破坏性 (destructive)** 操作行为。因此，该命令**仅限**在本地开发环境或随用随弃的测试数据库上执行。

该命令**不会**自动启动 Docker 容器。请提前单独启动并校验本地技术栈：

```bash
make migration-stack-up
scripts/check-migration-stack.sh milvus-port
```

## 命令行参数 (Flags)

| 参数 (Flag) | 默认值 | 描述 (Description) |
| --- | --- | --- |
| `--fixture` | 必填 | 合成测试固件的 JSON 文件路径。 |
| `--address` | 必填 | Milvus gRPC 服务地址，例如 `localhost:19530`。 |
| `--collection` | `items` | 待被删除、重建并灌入数据的集合名称。 |
| `--id-field` | `id` | VarChar 类型的主键字段名。 |
| `--vector-field` | `embedding` | FloatVector 类型的向量字段名。 |
| `--metric` | `cosine` | 向量距离指标，目前支持 `cosine` 或 `l2`。 |

## 验证与容错 (Validation)

底层的 `MilvusSeeder` 会执行严格的逻辑校验：

- 维度必须在 `1..2000` 范围内；
- 固件维度必须与灌入器的期望维度一致；
- 记录的 ID 不能为空；
- 记录中的向量长度必须匹配规定的维度；
- 不允许存在 `NaN` 或无穷大的向量值；
- 集合及字段标识符必须合法安全；
- 支持的距离指标。

安全标识符必须符合以下正则表达式：

```text
^[A-Za-z_][A-Za-z0-9_]*$
```

## 当前局限性 (Current limitations)

- 不支持分区 (partitions)。
- 不支持元数据字段 (metadata fields)。
- 不支持直接写入查询向量 (query vector insertion)。
- 尚未优化针对生产环境的批量导入 (bulk import) 路径。
- 该命令始终会强制重建目标集合。

源端的下一步迭代计划，是实现真实的 Milvus 检索冒烟 CLI 工具以及源端指纹产物生成 CLI。