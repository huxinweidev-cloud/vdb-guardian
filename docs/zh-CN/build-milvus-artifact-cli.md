# build-milvus-artifact CLI

`vdbg build-milvus-artifact` 命令负责基于真实的 Milvus 检索结果，生成一份与 Python 引擎完全兼容的源端指纹产物 (source fingerprint artifact)。

在 Milvus 向 pgvector 迁移并验证的 MVP 中，该命令扮演着源端产物桥梁的关键角色。

## 命令用法 (Command)

```bash
go run ./cmd/vdbg build-milvus-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --output /tmp/vdb-guardian-source-fingerprint.json \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

## 前置条件 (Prerequisites)

在执行产物构建之前，必须先将测试数据灌入对应的 Milvus 集合中：

```bash
go run ./cmd/vdbg seed-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --metric cosine
```

本构建命令**不会**启动 Docker，**不会**主动创建集合，也**绝对不会**篡改 Milvus 中的任何数据。

## 行为逻辑 (Behavior)

该命令会依次执行以下操作：

1. 加载合成测试固件的 JSON 文件。
2. 校验固件中至少包含一条查询。
3. 通过真实的连接器 SDK 适配器连接至 Milvus。
4. 遍历固件中的**所有**查询，并将 `expand-k` 作为数据库的检索上限执行搜索。
5. 将连接器返回的原生命中记录，转换为指纹构建器所需的标准结构。
6. 调用 `internal/fingerprints` 模块，推导并构建出 `stable_neighbors` (稳定邻居)、`boundary_candidates` (边界候选者) 以及 `top_k_ids` (可见 TopK 标识)。
7. 将最终生成的源端指纹产物持久化为 JSON 文件。
8. 打印一份精简摘要，包含固件路径、输出路径、集合名称、查询总数以及各种窗口参数的配置。

参数说明：`top-k` 是业务侧实际可见的比较窗口；`expand-k` 是为了观测边界变动而预留的、更宽广的搜索窗口；`stable-k` 定义了占据头部绝对优势的稳定邻居集合大小；`boundary-k` 则划定了围绕业务 TopK 截断点两侧的排位窗口宽度。

## 输出产物 (Output artifact)

输出的 JSON 文件严格遵循项目中统一的指纹产物 Schema 规范，详情请参阅：

```text
docs/fingerprint-artifact-format.md
```

产物结构示例：

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

针对代码库自带的小型固件，预期生成的产物中应完整包含 `10` 个查询级指纹。

## 命令行参数 (Flags)

| 参数 (Flag) | 默认值 | 描述 (Description) |
| --- | --- | --- |
| `--fixture` | 必填 | 合成测试固件的 JSON 文件路径。 |
| `--address` | 必填 | Milvus gRPC 服务地址，例如 `localhost:19530`。 |
| `--output` | 必填 | 源端指纹产物 JSON 文件的预期输出路径。 |
| `--collection` | `items` | 待检索的 Milvus 集合名称。 |
| `--id-field` | `id` | 检索结果中应返回的主键字段名。 |
| `--vector-field` | `embedding` | 执行检索的 FloatVector 向量字段名。 |
| `--top-k` | `3` | 业务侧可见的 TopK 结果数量。 |
| `--expand-k` | `5` | 产物构建所需的扩展搜索数量。必须 `>= top-k + boundary-k`。 |
| `--stable-k` | `2` | 用于构建 `stable_neighbors` 的头部命中数量。必须 `<= top-k`。 |
| `--boundary-k` | `1` | 围绕 TopK 截断点两侧的排位窗口宽度。 |
| `--metric` | `cosine` | 检索所使用的指标，目前支持 `cosine` 或 `l2`。 |

## 当前局限性 (Current limitations)

- 依赖于给定的 Milvus 集合已被成功灌入数据并加载 (loaded) 到内存中。
- 仅支持遍历执行固件中预设的查询，尚未支持直接对生产环境的查询流量进行采样。
- 该命令不会自行执行产物比对；它需要配合 `build-pgvector-artifact` 生成的目标端产物，并交由 Python 比对引擎来完成最终的较量。
- 该命令不包含任何数据迁移逻辑；它单纯只负责对源端检索行为进行快照“捕获”。