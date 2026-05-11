# 指纹引擎规范 (Fingerprint Engine Specification)

指纹引擎的核心职责是测算源向量数据库与目标向量数据库在检索行为上的差异。

## 初版指标 (Initial metrics)

当前的实现版本包含以下核心机制：

- 边界候选者筛选 (Boundary candidate selection)。
- 杰卡德距离计算 (Jaccard distance)。
- 边界反转率测算 (Boundary flip rate)。
- 加权综合指纹距离 (Weighted fingerprint distance)。
- 基于产物的源/目标指纹比对 (Artifact-backed comparison)。
- 一致性评分 (Consistency scoring)。

## 边界候选者 (Boundary candidates)

边界候选者是指那些得分接近第 K 个结果、徘徊在 TopK 决策边界附近的命中记录。它们至关重要，因为由迁移所引发的索引机制、距离算法或过滤逻辑的差异，往往会导致这些候选者跌出或跻身于可见的 TopK 结果之中。

## 基于产物的比对 (Artifact-backed comparison)

`compare` 命令通过读取源端与目标端的指纹产物文件来执行比对。有关产物的 Schema 规范，请参阅 `docs/fingerprint-artifact-format.md`。

对于每一个成功匹配的 `query_id`，引擎会计算：

- `stable_neighbor_distance`: 稳定邻居集合之间的杰卡德距离。
- `boundary_candidate_distance`: 边界候选者集合之间的杰卡德距离。
- `boundary_flip_rate`: 发生了 TopK 可见性反转的边界候选者所占的比例。

如果在某一侧产物中找不到对应的查询 ID (Missing query IDs)，引擎会将其视为完全不一致，并施加满额距离惩罚 (full-distance penalties)。

## 距离指标 (Distance metrics)

引擎会在可能的情况下返回归一化至 `[0, 1]` 区间内的数值。综合指纹距离越低，说明源库与目标库的检索行为越相似。一致性得分越高，说明迁移的一致性越好。

当前加权计算后的综合指纹距离公式为：

```text
fingerprint_distance =
  0.4 * stable_neighbor_distance
+ 0.4 * boundary_flip_rate
+ 0.2 * boundary_candidate_distance
```

一致性得分公式为：

```text
consistency_score = 1.0 - fingerprint_distance
```

## 协议调用方向 (Protocol direction)

Go 控制平面通过子进程运行器和基于 JSON 文件的协议来调用 Python 引擎：

```text
python -m vdb_fingerprint_engine.cli compare --input input.json --output output.json
```

Python 引擎将返回一份精简的 JSON 摘要文件，并在未来的迭代中，通过产物边界 (artifact boundary) 输出更为详尽的比对报告。

有关当前通信 Schema 的规范，请参阅 `docs/engine-protocol.md`。