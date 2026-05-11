# 指纹产物构建器 (Fingerprint Artifact Builder)

指纹产物构建器负责将标准化的搜索结果转换为 Python 指纹比对引擎所需的 JSON 产物格式。

## 目的 (Purpose)

该构建器将数据库的连接逻辑与检索行为的比对逻辑彻底解耦：

```text
Milvus / pgvector 搜索结果
        |
        v
标准化的 SearchResult (搜索结果) 数据结构
        |
        v
Go 指纹产物构建器
        |
        v
source-fingerprint.json / target-fingerprint.json (源库与目标库指纹 JSON 文件)
        |
        v
Python 指纹比对引擎
```

这种架构设计使得开发人员在调试数据库连接问题时，完全不需要卷入指纹距离算法的复杂性之中，反之亦然。

## 所在模块 (Package)

具体的实现代码位于：

```text
internal/fingerprints
```

## 输入数据模型 (Input model)

```go
type SearchResult struct {
    QueryID string
    Hits    []SearchHit
}

type SearchHit struct {
    ID    string
    Rank  int
    Score float64
}
```

`Rank` (排名) 从 1 开始计算，数值越小表示匹配结果越好。目前的构建器会在衍生出产物字段之前，强制按照 `Rank` 对所有命中结果 (hits) 进行排序。

## 构建选项 (Build options)

```go
type BuildOptions struct {
    TopK      int
    StableK   int
    BoundaryK int
}
```

- `TopK`: 业务可见的 TopK 结果 ID 的数量，这些 ID 会被写入到 `top_k_ids` 字段中。
- `StableK`: 排在最前面的 ID 数量，这部分数据会被写入到 `stable_neighbors` (稳定邻居) 字段中。
- `BoundaryK`: 围绕业务 TopK 截断点两侧的排位窗口宽度，用于生成 `boundary_candidates` (边界候选者)。

初版构建器采用了基于排位窗口 (rank-window) 的边界选择策略，而非基于得分差值 (score-delta)。这样设计的原因是为了保证生成的产物具备跨数据库的通用性 (portable)，因为不同的向量数据库在得分指标上往往有着截然不同的刻度范围与方向体系。

## 边界候选规则 (Boundary candidate rule)

假设现有一组已按 `Rank` 排序的命中结果，并且配置参数为 `TopK = 3`，`StableK = 2`，`BoundaryK = 2`：

```text
命中结果排序: a, b, c, d, e
```

构建器将输出以下结构：

```json
{
  "query_id": "q-1",
  "stable_neighbors": ["a", "b"],
  "boundary_candidates": ["b", "c", "d", "e"],
  "top_k_ids": ["a", "b", "c"]
}
```

边界窗口 (boundary window) 会精准地捕获位于业务 TopK 截断点两侧的结果。在异构数据库迁移过程中，这些位于边缘地带的结果对漂移现象最为敏感；稍后，Python 引擎将利用这些数据计算出极其重要的边界反转率 (boundary flip rate)。

## 验证规则 (Validation)

构建器会主动拒绝以下情况：

- `TopK <= 0`；
- `StableK <= 0`；
- `StableK > TopK`；
- `BoundaryK <= 0`；
- 空的 `query_id`；
- 重复的 `query_id`；
- 空的命中 ID (hit IDs)；
- 排名为非正数 (non-positive ranks)；
- 对应查询的命中结果数量少于 `TopK`。

## 输出 (Output)

`WriteArtifact` 方法会将其序列化为与 Python 完全兼容的 JSON 产物文件：

```json
{
  "fingerprints": [
    {
      "query_id": "q-1",
      "stable_neighbors": ["a", "b"],
      "boundary_candidates": ["b", "c", "d", "e"],
      "top_k_ids": ["a", "b", "c"]
    }
  ]
}
```

关于引擎端消费该产物的详细契约规范，请参阅 `docs/fingerprint-artifact-format.md`。