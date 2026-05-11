# 引擎协议 (Engine Protocol)

引擎协议定义了 Go 控制平面 (control plane) 如何调用并驱动底层的 Python 检索行为指纹引擎。

## 当前执行模式 (Current execution mode)

为了简化初期企业级部署的复杂度，目前的实现方案采用了稳健的 Python 子进程 (subprocess) 模式：

```text
Go PythonRunner (运行器)
  -> 派生子进程：python -m vdb_fingerprint_engine.cli compare --input input.json --output output.json
  -> 解析返回的 JSON CompareOutput 数据
```

这种设计不仅大幅降低了部署门槛，同时也构建了一个清晰的进程隔离边界。随着未来系统规模的扩张，该边界可以平滑地演进为 gRPC、HTTP 接口或独立的远程 Python 微服务。

## Go 运行器实现 (Go runner)

Go 语言端的调用逻辑封装在 `internal/engine/python_runner.go` 中。

```go
runner := engine.NewPythonRunner("/path/to/python", "/path/to/repo/python")
output, err := runner.Compare(ctx, engine.CompareInput{
    JobID: "job-1",
    SourceFingerprintPath: "source.json",
    TargetFingerprintPath: "target.json",
})
```

该运行器完整地接管了以下生命周期流程：

1. 在宿主机上创建一个专用的临时工作目录。
2. 将调用的参数序列化并写入 `input.json` 文件。
3. 执行 Python CLI 的 `compare` 核心比对命令。
4. 读取引擎输出的 `output.json` 结果文件。
5. 将 Python 端生成的 `snake_case` (蛇形命名) JSON 数据反序列化为强类型的 Go 结构体。
6. 如果子进程执行失败，则精准捕获并抛出附带了子进程标准输出/错误流的诊断级错误。

## 输入格式 (Input JSON)

```json
{
  "job_id": "job-1",
  "source_fingerprint_path": "source.json",
  "target_fingerprint_path": "target.json"
}
```

字段说明：

- `job_id`: 验证作业的唯一稳定标识符。
- `source_fingerprint_path`: 源库检索行为指纹产物文件的绝对/相对路径。
- `target_fingerprint_path`: 目标库检索行为指纹产物文件的绝对/相对路径。

源文件与目标文件都必须严格遵循 `docs/fingerprint-artifact-format.md` 中定义的规范。

## 输出格式 (Output JSON)

```json
{
  "job_id": "job-1",
  "consistency_score": 0.76,
  "metrics": {
    "fingerprint_distance": 0.24,
    "stable_neighbor_distance": 0.25,
    "boundary_candidate_distance": 0.1,
    "boundary_flip_rate": 0.2,
    "matched_query_count": 10,
    "missing_source_query_count": 0,
    "missing_target_query_count": 0
  }
}
```

字段说明：

- `job_id`: 直接从输入负载 (payload) 中拷贝回填。
- `consistency_score`: 归一化后的一致性得分，区间为 `[0, 1]`；分数越高，说明源库与目标库的行为越一致。
- `metrics.fingerprint_distance`: 经过权重计算与归一化处理后的综合指纹距离。
- `metrics.stable_neighbor_distance`: 稳定邻居集合 (stable-neighbor sets) 之间的平均杰卡德距离 (Jaccard distance)。
- `metrics.boundary_candidate_distance`: 边界候选者集合 (boundary-candidate sets) 之间的平均杰卡德距离。
- `metrics.boundary_flip_rate`: 归一化后的边界候选者 TopK 反转率。
- `metrics.matched_query_count`: 在源端与目标端均存在的匹配查询 ID 数量。
- `metrics.missing_source_query_count`: 存在于目标端，但源端产物中缺失的查询 ID 数量。
- `metrics.missing_target_query_count`: 存在于源端，但目标端产物中缺失的查询 ID 数量。

## 当前引擎行为 (Current behavior)

目前，Python 的 `compare` 命令行工具会主动读取指定的源库和目标库指纹产物，并计算出严谨的、基于产物的一致性指标。任何在单侧缺失的查询 ID (Missing query IDs)，都将遭受**满额距离的惩罚 (full-distance penalties)**。

## 生产安全底线 (Security notes)

- 绝对禁止将数据库凭据、密钥或任何形式的 Secret 混入引擎输入的 JSON 文件中。
- 严禁在日志中输出真实的生产环境连接字符串 (DSNs) 以及包含机密信息的产物绝对路径。
- Go 运行器生成的临时输入文件将强制应用 `0600` (仅所有者读写) 级别的文件权限。
- 每次引擎调用完毕后，必须无条件清理所有的临时文件以确保不留痕迹。