"""Distance metrics for retrieval behavior fingerprint comparison."""


def jaccard_distance(left: set[str], right: set[str]) -> float:
    """Compute normalized Jaccard distance between two identifier sets.

    This metric is used to quantify differences between stable-neighbor sets or
    boundary-candidate sets. A value of 0.0 means the sets are equivalent, while
    a value of 1.0 means they have no overlap.

    计算两个标识符集合之间的归一化杰卡德距离 (Jaccard distance)。

    该指标用于量化稳定邻居集合 (stable-neighbor sets) 或边界候选者集合
    (boundary-candidate sets) 之间的差异。值为 0.0 表示集合完全等价，
    而值为 1.0 表示它们毫无交集。

    Args:
        left: First set of vector identifiers.
        right: Second set of vector identifiers.

    Returns:
        A normalized distance in the inclusive range [0.0, 1.0].
    """
    if not left and not right:
        return 0.0

    union = left | right
    intersection = left & right
    return 1.0 - (len(intersection) / len(union))


def boundary_flip_rate(
    source_top_k: set[str], target_top_k: set[str], boundary_candidates: set[str]
) -> float:
    """Measure how often boundary candidates cross the topK visibility boundary.

    A boundary flip occurs when a candidate is visible in the source topK but not
    the target topK, or visible in the target topK but not the source topK. This
    metric is central to vdb-guardian because it detects migration drift that a
    coarse data-count check or average benchmark may miss.

    测算边界候选者跨越 TopK 可见性边界的频率 (边界反转率)。

    当一个候选者在源端 TopK 中可见但在目标端 TopK 中不可见时，或者反之在目标端可见但在
    源端不可见时，就会发生边界反转 (boundary flip)。该指标是 vdb-guardian 的核心，
    因为它能够极其敏锐地探测到那些被粗粒度的数据量核对或宏观基准测试所遗漏的迁移行为漂移。

    Args:
        source_top_k: Identifiers visible in the source database topK result.
        target_top_k: Identifiers visible in the target database topK result.
        boundary_candidates: Candidate identifiers near the source or target topK cutoff.

    Returns:
        The fraction of boundary candidates whose topK visibility changed.
    """
    if not boundary_candidates:
        return 0.0

    flipped = [
        candidate_id
        for candidate_id in boundary_candidates
        if (candidate_id in source_top_k) != (candidate_id in target_top_k)
    ]
    return len(flipped) / len(boundary_candidates)


def weighted_fingerprint_distance(
    *, components: dict[str, float], weights: dict[str, float]
) -> float:
    """Combine named fingerprint-distance components with normalized weights.

    结合已命名的指纹距离分量与归一化的权重，计算出综合指纹距离。

    Args:
        components: Mapping from component names to normalized distances.
        weights: Mapping from component names to non-negative weights.

    Returns:
        Weighted normalized fingerprint distance.

    Raises:
        ValueError: If components are missing weights, weights are negative, or
            the total weight is not positive.
    """
    if not components:
        return 0.0

    missing_weights = set(components) - set(weights)
    if missing_weights:
        raise ValueError(f"weights missing components: {sorted(missing_weights)}")

    total_weight = 0.0
    total_distance = 0.0
    for name, component_distance in components.items():
        weight = weights[name]
        if weight < 0:
            raise ValueError(f"weights must not be negative: {name}")
        total_weight += weight
        total_distance += component_distance * weight

    if total_weight <= 0:
        raise ValueError("weights must have a positive total")

    return total_distance / total_weight
