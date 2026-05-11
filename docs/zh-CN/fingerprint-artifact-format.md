# 指纹产物格式规范 (Fingerprint Artifact Format)

指纹产物 (Fingerprint artifacts) 捕获了某一向量数据库在查询级别的检索行为特征。Python 引擎通过比对源端产物与目标端产物，来计算出迁移过程的一致性指标。

## 文件结构 (File shape)

```json
{
  "fingerprints": [
    {
      "query_id": "q-1",
      "stable_neighbors": ["a", "b", "c"],
      "boundary_candidates": ["d", "e"],
      "top_k_ids": ["a", "b", "c", "d"]
    }
  ]
}
```

## 字段说明 (Fields)

### `fingerprints`

必须提供的查询级指纹列表。该列表不能为空。

### `query_id`

稳定的查询标识符。源端产物与目标端产物正是通过该字段进行对齐 (aligned) 比对的。

在同一个产物文件中，绝对不允许出现重复的 `query_id`。

### `stable_neighbors`

代表该查询的稳定近邻集合 (stable near-neighbor set) 的标识符。目前的引擎使用杰卡德距离 (Jaccard distance) 来比对源端与目标端的这一集合。

### `boundary_candidates`

位于 TopK 决策边界 (decision boundary) 附近的标识符。在异构数据库迁移过程中，这些候选者对向量索引、距离测算、过滤条件以及排序规则的差异表现得极其敏感。

### `top_k_ids`

在 TopK 结果中实际可见的标识符。引擎利用该集合来探测：在迁移之后，是否有边界候选者跌出或跻身于可见的 TopK 结果之中。

## 当前的比对指标 (Current comparison metrics)

对于每一个成功匹配的 `query_id`，引擎会计算：

- `stable_neighbor_distance`: 稳定邻居集合之间的杰卡德距离。
- `boundary_candidate_distance`: 边界候选者集合之间的杰卡德距离。
- `boundary_flip_rate`: 发生了 TopK 可见性反转 (可见性改变) 的边界候选者所占的比例。

如果在某一侧产物中找不到对应的查询 ID (Missing query IDs)，引擎会将其视为完全不一致，并施加满额距离惩罚 (full-distance penalties)。

当前加权计算后的综合指纹距离 (weighted fingerprint distance) 公式为：

```text
fingerprint_distance =
  0.4 * stable_neighbor_distance
+ 0.4 * boundary_flip_rate
+ 0.2 * boundary_candidate_distance
```

一致性得分 (consistency score) 公式为：

```text
consistency_score = 1.0 - fingerprint_distance
```

这两个计算结果都会被严格限制在 `[0.0, 1.0]` 的区间内。

## 当前局限性 (Current limitations)

初版的产物格式刻意排除了得分曲线 (score curves)、过滤画像 (filter profiles)、距离测算元数据以及集合元数据。这些字段可以在后续迭代中平滑地引入，且无需更改核心的查询对齐模型。

## 生产安全底线 (Security notes)

在测试环境中，产物文件内必须使用合成的或非敏感的标识符。严禁将真实的客户数据、访问凭证、数据库连接字符串或私密元数据混入指纹产物文件中。