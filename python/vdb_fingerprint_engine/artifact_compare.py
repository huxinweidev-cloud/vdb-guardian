"""Artifact-backed retrieval behavior fingerprint comparison.

基于产物的检索行为指纹比对机制。
"""

import json
from pathlib import Path

from pydantic import BaseModel, Field

from vdb_fingerprint_engine.distance import boundary_flip_rate, jaccard_distance
from vdb_fingerprint_engine.schemas import CompareOutput, MetricSummary


class QueryFingerprint(BaseModel):
    """Represent one query-level retrieval behavior fingerprint.

    代表单次查询级别的检索行为指纹画像。

    Args:
        query_id: Stable query identifier used to align source and target fingerprints.
        stable_neighbors: Identifiers representing the stable near-neighbor set.
        boundary_candidates: Identifiers near the topK decision boundary.
        top_k_ids: Identifiers visible in the query's topK results.
    """

    query_id: str
    stable_neighbors: list[str] = Field(default_factory=list)
    boundary_candidates: list[str] = Field(default_factory=list)
    top_k_ids: list[str] = Field(default_factory=list)


class FingerprintArtifact(BaseModel):
    """Represent a file containing query-level retrieval behavior fingerprints.

    代表包含大量查询级检索行为指纹的产物文件结构。

    Args:
        fingerprints: Query-level fingerprints captured from one vector database.
    """

    fingerprints: list[QueryFingerprint]


class AggregateMetrics(BaseModel):
    """Represent averaged artifact comparison metrics before protocol conversion.

    代表在被转换为传输协议之前的、经过平均化处理的产物比对指标聚合结构。

    Args:
        stable_neighbor_distance: Average stable-neighbor Jaccard distance.
        boundary_candidate_distance: Average boundary-candidate Jaccard distance.
        boundary_flip_rate: Average rate of boundary candidates entering or leaving topK.
        fingerprint_distance: Weighted normalized retrieval behavior fingerprint distance.
        matched_query_count: Number of query IDs present in both artifacts.
        missing_source_query_count: Number of target query IDs missing from source artifact.
        missing_target_query_count: Number of source query IDs missing from target artifact.
    """

    stable_neighbor_distance: float
    boundary_candidate_distance: float
    boundary_flip_rate: float
    fingerprint_distance: float
    matched_query_count: int
    missing_source_query_count: int
    missing_target_query_count: int


def compare_fingerprint_artifacts(
    job_id: str, source_path: Path, target_path: Path
) -> CompareOutput:
    """Compare two fingerprint artifact files and return normalized metrics.

    The comparison aligns query fingerprints by `query_id`, averages per-query
    stable-neighbor distance, boundary-candidate distance, and boundary flip
    rate, then combines them into a weighted fingerprint distance. Missing query
    IDs contribute a full-distance penalty so incomplete artifacts lower the
    final consistency score.

    比对两份指纹产物文件，并返回规范化的指标数据。

    该比对流程会根据 `query_id` 将查询指纹进行严格对齐，计算并平均化每次查询的
    稳定邻居距离、边界候选者距离以及边界反转率，然后将它们揉合为一个加权的综合
    指纹距离。对于缺失的查询 ID，系统将毫不留情地施加满额距离惩罚 (full-distance penalty)，
    以此确保不完整的残缺产物会显著拉低最终的整体一致性得分。

    Args:
        job_id: Verification job identifier copied into the compare output.
        source_path: Path to source database fingerprint artifact JSON.
        target_path: Path to target database fingerprint artifact JSON.

    Returns:
        CompareOutput containing consistency score and aggregate metrics.

    Raises:
        ValueError: If either artifact has no fingerprints or contains duplicate query IDs.
        OSError: If artifact files cannot be read.
        ValidationError: If artifact JSON does not match the expected schema.
    """
    source_artifact = load_fingerprint_artifact(source_path)
    target_artifact = load_fingerprint_artifact(target_path)
    metrics = aggregate_artifact_metrics(source_artifact, target_artifact)
    consistency_score = clamp01(1.0 - metrics.fingerprint_distance)
    return CompareOutput(
        job_id=job_id,
        consistency_score=consistency_score,
        metrics=MetricSummary(
            fingerprint_distance=metrics.fingerprint_distance,
            stable_neighbor_distance=metrics.stable_neighbor_distance,
            boundary_candidate_distance=metrics.boundary_candidate_distance,
            boundary_flip_rate=metrics.boundary_flip_rate,
            matched_query_count=metrics.matched_query_count,
            missing_source_query_count=metrics.missing_source_query_count,
            missing_target_query_count=metrics.missing_target_query_count,
        ),
    )


def load_fingerprint_artifact(path: Path) -> FingerprintArtifact:
    """Load and validate one fingerprint artifact JSON file.

    加载并校验单份指纹产物 JSON 文件。

    Args:
        path: JSON artifact path to load.

    Returns:
        FingerprintArtifact parsed from the JSON file.

    Raises:
        ValueError: If the artifact has no fingerprints.
        OSError: If the file cannot be read.
        ValidationError: If the JSON schema is invalid.
    """
    payload = json.loads(path.read_text(encoding="utf-8"))
    artifact = FingerprintArtifact.model_validate(payload)
    if not artifact.fingerprints:
        raise ValueError("fingerprints must not be empty")
    return artifact


def aggregate_artifact_metrics(
    source_artifact: FingerprintArtifact,
    target_artifact: FingerprintArtifact,
) -> AggregateMetrics:
    """Aggregate query-level fingerprint distances across two artifacts.

    跨两份产物文件对查询级别的指纹距离执行聚合与统计。

    Args:
        source_artifact: Fingerprints collected from the source database.
        target_artifact: Fingerprints collected from the target database.

    Returns:
        AggregateMetrics with averaged distances and missing-query counts.

    Raises:
        ValueError: If duplicate query IDs appear in either artifact.
    """
    source_by_query = index_by_query_id(source_artifact)
    target_by_query = index_by_query_id(target_artifact)
    source_query_ids = set(source_by_query)
    target_query_ids = set(target_by_query)
    matched_query_ids = sorted(source_query_ids & target_query_ids)
    missing_target_count = len(source_query_ids - target_query_ids)
    missing_source_count = len(target_query_ids - source_query_ids)

    stable_distances: list[float] = []
    boundary_distances: list[float] = []
    flip_rates: list[float] = []
    for query_id in matched_query_ids:
        source = source_by_query[query_id]
        target = target_by_query[query_id]
        stable_distances.append(
            jaccard_distance(set(source.stable_neighbors), set(target.stable_neighbors))
        )
        boundary_distances.append(
            jaccard_distance(set(source.boundary_candidates), set(target.boundary_candidates))
        )
        boundary_union = set(source.boundary_candidates) | set(target.boundary_candidates)
        flip_rates.append(
            boundary_flip_rate(set(source.top_k_ids), set(target.top_k_ids), boundary_union)
        )

    penalty_count = missing_source_count + missing_target_count
    denominator = len(matched_query_ids) + penalty_count
    if denominator == 0:
        fingerprint_distance = 1.0
        stable_distance = 1.0
        boundary_distance = 1.0
        flip_rate = 1.0
    else:
        stable_distance = average_with_full_penalty(stable_distances, penalty_count, denominator)
        boundary_distance = average_with_full_penalty(
            boundary_distances, penalty_count, denominator
        )
        flip_rate = average_with_full_penalty(flip_rates, penalty_count, denominator)
        fingerprint_distance = clamp01(
            (0.4 * stable_distance) + (0.4 * flip_rate) + (0.2 * boundary_distance)
        )

    return AggregateMetrics(
        stable_neighbor_distance=stable_distance,
        boundary_candidate_distance=boundary_distance,
        boundary_flip_rate=flip_rate,
        fingerprint_distance=fingerprint_distance,
        matched_query_count=len(matched_query_ids),
        missing_source_query_count=missing_source_count,
        missing_target_query_count=missing_target_count,
    )


def index_by_query_id(artifact: FingerprintArtifact) -> dict[str, QueryFingerprint]:
    """Index query fingerprints by query ID while rejecting duplicates.

    按查询 ID 对查询指纹进行索引化，并主动拒绝任何重复数据。

    Args:
        artifact: Fingerprint artifact to index.

    Returns:
        Mapping from query ID to query fingerprint.

    Raises:
        ValueError: If a query ID appears more than once.
    """
    indexed: dict[str, QueryFingerprint] = {}
    for fingerprint in artifact.fingerprints:
        if fingerprint.query_id in indexed:
            raise ValueError(f"duplicate query_id {fingerprint.query_id!r}")
        indexed[fingerprint.query_id] = fingerprint
    return indexed


def average_with_full_penalty(values: list[float], penalty_count: int, denominator: int) -> float:
    """Average matched distances while treating missing queries as full distance.

    在求取平均匹配距离的过程中，将所有缺失的查询视作满额的距离惩罚 (full distance)。

    Args:
        values: Distances for matched query IDs.
        penalty_count: Number of missing query penalties to add as `1.0` values.
        denominator: Total matched-plus-penalty count.

    Returns:
        A normalized average in `[0, 1]`.
    """
    return clamp01((sum(values) + penalty_count) / denominator)


def clamp01(value: float) -> float:
    """Clamp a numeric metric to the inclusive `[0, 1]` range.

    将一项数值指标钳制 (clamp) 到受包含的 `[0, 1]` 范围之内。

    Args:
        value: Metric value to normalize.

    Returns:
        The value constrained to `[0.0, 1.0]`.
    """
    return max(0.0, min(1.0, value))
