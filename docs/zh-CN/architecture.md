# vdb-guardian 架构设计

## 核心愿景 (Purpose)

在异构向量数据库的迁移过程中，`vdb-guardian` 致力于精确校验检索行为的一致性。它并非通用的基准测试工具，亦非简单的数据核对程序。其核心的领域模型，聚焦于**检索行为指纹** (retrieval behavior fingerprint)。

## 第一阶段架构 (First-stage architecture)

```text
Go 构建的 CLI/API 控制平面
        |
        | 稳定的 JSON 与产物协议 (stable JSON/artifact protocol)
        v
Python 驱动的指纹引擎
```

Go 语言层专注于企业级可靠性保障 (Enterprise reliability)：

- 提供 CLI 工具及未来的服务端入口。
- 管理作业生命周期 (Job lifecycle) 的状态流转。
- 编排本地验证任务。
- 调度本地离线验证流水线 (Local offline verification pipeline)。
- 负责强类型 YAML 配置的解析与校验。
- 定义连接器 (Connector) 的标准接口。
- 提供内存连接器 (Memory connector)，以确保本地验证的确定性。
- 解析标准化搜索结果，构建指纹产物 (Fingerprint artifact)。
- 抽象产物存储层 (Artifact storage)。
- 划定引擎调用的安全边界。
- 通过稳定的 JSON 协议，驱动 Python 子进程运行器。
- 为未来的可观测性与系统部署预留扩展点。

Python 语言层则专注于算法的快速迭代 (Algorithm velocity)：

- 筛选边界候选数据 (Boundary candidate selection)。
- 依托产物数据，执行源库与目标库的指纹比对。
- 计算稳定邻居 (Stable-neighbor) 及各项指纹指标。
- 测算指纹距离 (Fingerprint distance)。
- 评估并输出一致性得分 (Consistency scoring)。
- 支撑未来的统计分析与报告生成。

## 核心模块 (Core packages)

- `internal/jobs`: 维护持久化的作业生命周期状态，并驱动本地验证流程。
- `internal/pipeline`: 统筹本地离线验证流水线的执行。
- `internal/config`: 确保作业配置 (YAML) 的强类型加载与校验。
- `internal/connectors`: 制定向量数据库连接器的标准契约，并内置内存连接器以及轻量级的 Milvus 与 pgvector 连接器。
- `internal/migration`: 编排写入端的数据迁移，以及测试固件 (fixture) 的种子数据灌入。
- `internal/engine`: 确立 Go 与引擎间的比对契约，并管理 Python 子进程。
- `internal/fingerprints`: 负责将标准化的搜索结果转换为指纹产物。
- `internal/artifacts`: 提供指纹、分析报告及中间文件的存储抽象。
- `python/vdb_fingerprint_engine`: 封装核心的检索行为指纹算法。

## 演进路线 (Evolution path)

1. 构建包含 Go CLI 与 Python 引擎的模块化单体仓库 (Modular monorepo)。
2. 接入 Milvus 与 pgvector 数据库连接器。
3. 引入基于本地 Docker 的集成测试体系。
4. 拓展 API 路由机制，并实现作业的持久化存储。
5. 随着规模扩展的需求，适时将 Python 子进程引擎升级为独立的远程服务。