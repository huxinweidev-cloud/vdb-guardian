# 离线固件验证命令 (Offline Verify Fixture Command)

`vdbg offline-verify` 命令依托于静态的 JSON 验证固件，执行一条完全脱离数据库的本地验证工作流。它的主要用途是在 Milvus 和 pgvector 真实连接器代码落地之前，提供一种确定性的机制来验证系统内部核心编排和计算逻辑的正确性。

## 命令用法 (Command)

```bash
go run ./cmd/vdbg offline-verify \
  --fixture testdata/offline/basic.json \
  --artifact-dir /tmp/vdb-guardian-offline
```

执行该命令时，系统将：从给定的测试固件中解析出预定义的源端与目标端排序结果，实例化内存连接器，启动内部的离线验证流水线，跨语言拉起 Python 指纹比对引擎，并最终输出包括源端指纹、目标端指纹和比对结果在内的全套产物。

## 固件数据结构 (Fixture shape)

```json
{
  "job_id": "offline-basic",
  "top_k": 3,
  "expand_k": 4,
  "stable_k": 2,
  "boundary_k": 1,
  "queries": [
    {
      "query_id": "q-1",
      "source_hits": [
        {"id": "a", "rank": 1, "score": 0.99}
      ],
      "target_hits": [
        {"id": "a", "rank": 1, "score": 0.99}
      ]
    }
  ]
}
```

针对每一个特定的查询，其声明的源端 (`source_hits`) 和目标端 (`target_hits`) 命中结果数量，**必须至少等于**设置的 `expand_k`。

## 生成的产物 (Generated artifacts)

该命令将在指定的目录生成以下三份文件：

```text
<artifact-dir>/<job-id>-source-fingerprint.json
<artifact-dir>/<job-id>-target-fingerprint.json
<artifact-dir>/<job-id>-result.json
```

同时，该作业的结果文件绝对路径以及计算出的一致性得分 (consistency score)，会被直接打印到标准输出 (stdout)。

## Python 引擎环境探测 (Python engine discovery)

Go 侧运行器会按以下顺序探测并寻找合适的 Python 执行环境：

1. 优先尝试项目内的虚拟环境：`python/.venv/bin/python`
2. 如果未找到，退而求其次尝试系统环境：`python3`
3. 最后尝试：`python`

拉起 Python 子进程时，其工作目录 (working directory) 会被统一设定为代码库根目录下的 `python/` 文件夹。

## 局限性 (Limitations)

该命令**绝对不会**连接任何 Milvus 实例或 pgvector 数据库。它不会执行任何涉及底层数学运算的向量相似度检索。它的存在意义仅仅在于验证本地这套固件驱动型工作流 (fixture-backed workflow) 的稳健性，这涵盖了从内存连接器的数据摄入、指纹产物生成、跨进程调用 Python 引擎进行比对，再到最终序列化输出结果产物这一完整的内部系统链路。