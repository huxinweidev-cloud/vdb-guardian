# 本地离线验证流水线 (Local Offline Verification Pipeline)

本地离线验证流水线是 `vdb-guardian` 中第一个实现了端到端 (end-to-end) 串联、却又完全脱离真实数据库的验证路径。它将确定性的连接器 (deterministic connectors)、指纹产物构建机制以及本地验证运行器无缝衔接，在这个过程中，既不需要启动 Docker 容器，也无需与真实的向量数据库进行任何网络通信。

## 工作流 (Workflow)

```text
源端连接器 (source connector)
        |
        v
目标端连接器 (target connector)
        |
        v
标准化的 SearchResponse (搜索结果) 数据结构
        |
        v
调用 fingerprints.BuildArtifact (构建产物)
        |
        v
<job-id>-source-fingerprint.json
<job-id>-target-fingerprint.json
        |
        v
移交 jobs.VerificationRunner (验证运行器)
        |
        v
输出 <job-id>-result.json (最终比对报告)
```

## 所在模块 (Package)

具体的实现代码位于：

```text
internal/pipeline
```

该流水线被刻意设计为内部 (internal) 模块，因为它本质上是 Go 控制平面的编排胶水层 (orchestration glue)，而非对外开放的公共 SDK API。

## 核心 API (Core API)

```go
pipeline := pipeline.NewOfflinePipeline(
    sourceConnector,
    targetConnector,
    verificationRunner,
    artifactDir,
    fingerprints.BuildOptions{TopK: 3, StableK: 2, BoundaryK: 1},
)

result, err := pipeline.Run(ctx, pipeline.OfflineRequest{
    JobID:    "job-1",
    QueryIDs: []string{"q-1", "q-2"},
    TopK:     3,
    ExpandK:  4,
})
```

## 生成的产物 (Generated artifacts)

运行该流水线将在文件系统中输出以下文件：

```text
<artifact-dir>/<job-id>-source-fingerprint.json
<artifact-dir>/<job-id>-target-fingerprint.json
<artifact-dir>/<job-id>-result.json
```

源端与目标端的指纹产物将严格遵循以下文档中定义的 Schema 规范：

```text
docs/fingerprint-artifact-format.md
```

而最终的比对结果产物，则由本地验证运行器生成，其格式定义见：

```text
docs/local-verification-runner.md
```

## 验证与容错行为 (Validation)

`Run` 方法会主动拦截并拒绝以下情况：

- 源端或目标端连接器为空 (`nil`)；
- 验证运行器引擎为空 (`nil`)；
- 产物输出目录为空；
- 作业 ID (Job ID) 为空；
- 查询 ID (Query ID) 列表为空；
- `TopK` 不是正整数；
- `ExpandK` 的值小于 `TopK`；
- 连接器执行搜索时发生错误；
- 指纹产物构建过程中发生错误；
- 写入产物文件时发生 I/O 错误；
- 验证运行器执行时发生错误。

## 当前连接器的约定 (Current connector convention)

当该流水线配合内存连接器 (`MemoryConnector`) 使用时，每一个单独的查询 ID 都会被注入到 `SearchRequest.Collection` 字段中。这使得流水线能够在没有任何真实向量检索基础设施的情况下，依然完整地行使与验证相同的连接器接口契约。在稍后的迭代中，真实的 Milvus 与 pgvector 连接器会将真实的查询向量 (embeddings) 映射到数据库中对应的集合或表上进行检索。

## 局限性 (Limitations)

目前，该流水线尚未开放 CLI 命令或 HTTP API 接口调用。它现阶段的定位仅仅是一个具备完善测试覆盖的、专用于离线功能验证的**内部编排层**。接下来的演进计划是：将它与强类型的作业配置模块对接，并在端到端行为彻底稳定之后，最终向用户暴露可执行的 CLI 命令。