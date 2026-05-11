# 本地验证运行器 (Local Verification Runner)

本地验证运行器 (Local Verification Runner) 位于指纹比对引擎之上，是 Go 语言控制平面的第一层业务编排。它能够在不连接任何真实向量数据库的前提下，纯粹依靠本地已有的指纹产物，驱动完成整个验证作业。

## 目的 (Purpose)

该运行器的核心使命，是在正式接入 Milvus 与 pgvector 数据库之前，以端到端的方式跑通本地验证的全流程：

```text
源库指纹产物 (source fingerprint artifact)
        |
目标库指纹产物 (target fingerprint artifact)
        |
        v
Go VerificationRunner (验证运行器)
        |
        v
engine.Engine Compare (调用引擎执行比对)
        |
        v
JSON 格式的比对结果产物 (result artifact)
```

这种设计将核心算法的校验与数据库的网络连通性彻底隔离开来，大幅降低了后期系统集成时的调试难度。

## Go API

运行器的核心代码位于 `internal/jobs/runner.go`。

```go
runner := jobs.NewVerificationRunner(engine, "artifacts")
result, err := runner.Run(ctx, jobs.VerificationRequest{
    JobID: "job-1",
    SourceFingerprintPath: "source.json",
    TargetFingerprintPath: "target.json",
})
```

## 请求模型 (Request)

```go
type VerificationRequest struct {
    JobID string
    SourceFingerprintPath string
    TargetFingerprintPath string
}
```

`SourceFingerprintPath` 与 `TargetFingerprintPath` 必须指向符合 `docs/fingerprint-artifact-format.md` 规范的 JSON 产物文件。

## 结果产物 (Result artifact)

运行器会为每个作业生成一份独立的 JSON 结果文件：

```text
<artifact-dir>/<job-id>-result.json
```

示例内容：

```json
{
  "job_id": "job-1",
  "state": "SUCCEEDED",
  "consistency_score": 0.76,
  "metrics": {
    "FingerprintDistance": 0.24,
    "StableNeighborDistance": 0.25,
    "BoundaryCandidateDistance": 0.1,
    "BoundaryFlipRate": 0.2,
    "MatchedQueryCount": 10,
    "MissingSourceQueryCount": 0,
    "MissingTargetQueryCount": 0
  }
}
```

当前的 Go 结果产物中，嵌套的 `metrics` 对象使用的是 Go 语言风格的指标字段名 (大驼峰)。而底层的 Python 引擎协议使用的是 `snake_case` (蛇形命名法)。未来的报告生成层将负责对外部报告的 JSON 格式进行标准化处理，使其独立于内部本地运行器的产物格式。

## 验证与容错行为 (Validation behavior)

运行器会主动拒绝以下请求：

- 传入的引擎实例为空 (`nil engine`)；
- 空的 `job_id`；
- 空的源指纹产物路径；
- 空的目标指纹产物路径。

如果 Python 引擎在执行比对的过程中抛出错误，运行器会将该错误直接向上传递，并且**不会**生成标记为成功的产物文件。

## 当前局限性 (Current limitations)

目前，该运行器**尚不支持**以下功能：

- 直接解析与加载 `configs/*.yaml` 配置文件；
- 直接从真实的 Milvus 或 pgvector 中采集搜索结果；
- 直接根据数据库的实时查询结果来生成指纹产物；
- 提供可供最终用户调用的 CLI 命令；
- 渲染生成 Markdown 格式的可视化报告。

上述的高级功能将在此运行器架构的基础之上，作为更高层的模块被逐步叠加实现。