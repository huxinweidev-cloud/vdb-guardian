"""Shared schemas for retrieval behavior fingerprint calculations.

The Python engine keeps these schemas small and explicit so the Go control plane
can exchange JSON payloads with the algorithm layer without depending on Python
implementation details.

指纹计算模块共享的数据结构 Schema。

Python 引擎刻意保持这些 Schema 的精简与明确，以便 Go 控制平面能够通过 JSON 负载
与算法层进行数据交换，而完全无需依赖任何特定的 Python 实现细节。
"""

from pydantic import BaseModel, Field


class SearchHit(BaseModel):
    """Represent one normalized vector search result.

    代表单条规范化的向量检索结果。

    Args:
        id: Stable vector or document identifier used to compare source and target results.
        rank: One-based rank returned by a vector database search.
        score: Similarity score used to identify boundary candidates and score curves.
    """

    id: str
    rank: int = Field(ge=1)
    score: float


class CompareInput(BaseModel):
    """Represent the JSON request sent from the Go control plane to Python.

    代表从 Go 控制平面发送至 Python 端的 JSON 请求格式。

    Args:
        job_id: Stable verification job identifier used to correlate input and output.
        source_fingerprint_path: Artifact path for the source retrieval behavior fingerprint.
        target_fingerprint_path: Artifact path for the target retrieval behavior fingerprint.
    """

    job_id: str
    source_fingerprint_path: str
    target_fingerprint_path: str


class MetricSummary(BaseModel):
    """Represent normalized comparison metrics returned to the Go control plane.

    代表返回给 Go 控制平面的规范化比对指标集合。

    Args:
        fingerprint_distance: Overall normalized distance between source and target fingerprints.
        stable_neighbor_distance: Average distance between stable-neighbor sets.
        boundary_candidate_distance: Average distance between boundary-candidate sets.
        boundary_flip_rate: Fraction of boundary candidates whose topK visibility changed.
        matched_query_count: Number of query IDs present in both artifacts.
        missing_source_query_count: Number of target query IDs missing from the source artifact.
        missing_target_query_count: Number of source query IDs missing from the target artifact.
    """

    fingerprint_distance: float = Field(ge=0.0, le=1.0)
    stable_neighbor_distance: float = Field(default=0.0, ge=0.0, le=1.0)
    boundary_candidate_distance: float = Field(default=0.0, ge=0.0, le=1.0)
    boundary_flip_rate: float = Field(ge=0.0, le=1.0)
    matched_query_count: int = Field(default=0, ge=0)
    missing_source_query_count: int = Field(default=0, ge=0)
    missing_target_query_count: int = Field(default=0, ge=0)


class CompareOutput(BaseModel):
    """Represent the JSON response produced by the Python fingerprint engine.

    代表 Python 指纹比对引擎生成的 JSON 响应格式。

    Args:
        job_id: Verification job identifier copied from the compare input.
        consistency_score: Normalized consistency score where higher means more consistent.
        metrics: Decomposed fingerprint comparison metrics.
    """

    job_id: str
    consistency_score: float = Field(ge=0.0, le=1.0)
    metrics: MetricSummary
