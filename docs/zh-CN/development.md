# 开发工作流 (Development Workflow)

## 规范 (Rules)

在修改代码之前，请务必阅读 `CLAUDE.md`。开发过程必须遵循测试驱动开发 (TDD) 的原则，并且每个公开方法 (public method) 都必须包含文档注释与对应的测试用例。

## 质量门禁 (Quality gates)

在提交 (commit) 代码之前，请运行以下命令：

```bash
make fmt
make lint
make test
git diff --check
```

## 依赖管理 (Dependency management)

Python 的依赖项由 `uv` 进行管理。除非您明确是为了修复本地环境并已将原因记录在案，否则请勿使用全局的 `pip install` 来安装项目依赖。

Go 语言的依赖项必须经过审慎添加，并保持在最低限度。目前 YAML 配置文件的加载使用了 `gopkg.in/yaml.v3`，因为 Go 标准库中并未原生提供 YAML 解析器。pgvector 连接器的 PostgreSQL 数据库连接依赖于 `github.com/jackc/pgx/v5`。最小化 Milvus 连接器目前仅采用适配器边界 (adapter boundary) 的形式，并未将 Milvus SDK 引入构建过程；仅在实现真实的网路调用和集成测试时，才可引入该 SDK。

## 配置模块开发 (Configuration development)

当修改配置相关代码时，必须同步更新以下内容：

- `internal/config` 包中的结构体及验证测试。
- `configs/*.yaml` 示例文件。
- `docs/config-spec.md` 规范文档。
- 当面向用户的行为发生改变时，需更新 README 文件。

在 TDD 过程中请运行 `go test ./internal/config`，并在提交前运行 `make test`。

## 引擎协议开发 (Engine protocol development)

当修改 Go/Python 引擎协议时，必须同步更新以下内容：

- `internal/engine` 中的运行器 (runner) 测试。
- `python/vdb_fingerprint_engine` 中的数据结构 (schemas) 及 CLI 测试。
- `docs/engine-protocol.md` 协议文档。
- 当面向用户的命令发生改变时，需更新 README 文件。

提交前，请运行 `go test ./internal/engine`、`cd python && uv run pytest tests/test_cli.py` 以及 `make test`。

## 作业运行器开发 (Job runner development)

当修改本地作业运行器时，必须同步更新以下内容：

- `internal/jobs` 中的运行器测试。
- 当输出字段改变时，更新结果产物 (result artifact) 的文档。
- `docs/local-verification-runner.md` 文档。
- 当面向用户的工作流发生改变时，需更新 README 文件。

提交前，请运行 `go test ./internal/jobs`、`make test` 以及 `git diff --check`。

## 连接器开发 (Connector development)

当修改连接器时，必须同步更新以下内容：

- `internal/connectors` 中的接口或实现测试。
- `docs/` 目录下特定连接器的说明文档。
- 当本地验证行为发生改变时，需更新 README 文件。

提交前，请运行 `go test ./internal/connectors`、`make test` 以及 `git diff --check`。

## 迁移模块开发 (Migration development)

当修改数据迁移与测试固件 (fixture) 播种代码时，必须同步更新以下内容：

- `internal/migration` 中的测试。
- `docs/` 目录下特定于迁移的说明文档。
- 当迁移工作流发生改变时，需更新 README 文件。

提交前，请运行 `go test ./internal/migration`、`make test` 以及 `git diff --check`。

## 指纹产物构建器开发 (Fingerprint artifact builder development)

当修改指纹构建器时，必须同步更新以下内容：

- `internal/fingerprints` 中的构建器测试。
- `docs/fingerprint-artifact-builder.md` 文档。
- 当产物契约发生改变时，需更新 `docs/fingerprint-artifact-format.md`。
- 当本地工作流发生改变时，需更新 README 文件。

提交前，请运行 `go test ./internal/fingerprints`、`make test` 以及 `git diff --check`。

## 流水线开发 (Pipeline development)

当修改流水线时，必须同步更新以下内容：

- `internal/pipeline` 中的编排测试。
- 当其工作流契约发生改变时，更新对应的连接器、指纹或运行器文档。
- `docs/local-offline-pipeline.md` 文档。
- 当本地验证行为发生改变时，需更新 README 文件。

提交前，请运行 `go test ./internal/pipeline`、`make test` 以及 `git diff --check`。

## CLI 开发 (CLI development)

当修改 CLI 工具时，必须同步更新以下内容：

- `cmd/vdbg` 中的命令测试。
- `docs/` 目录下针对用户的命令说明文档。
- 当命令名称、参数或冒烟测试 (smoke checks) 改变时，需更新 README 文件。

提交前，请运行 `go test ./cmd/vdbg`、在条件允许时运行文档中注明的冒烟测试命令、`make test` 以及 `git diff --check`。执行数据库写入操作的 CLI 命令应首先编写注入了适配器的测试；真实的 Docker/数据库冒烟测试需要操作者在启动服务前予以明确批准。执行数据库读取操作的冒烟测试命令应尽可能复用现有的本地服务，且严禁隐式启动 Docker。

## 进度汇报 (Progress reporting)

请在各阶段交接点汇报以下进度：

- 已完成的工作 (Completed work)。
- 正在进行的工作 (Current work)。
- 下一步行动 (Next action)。
- 潜在风险或阻塞点 (Risks or blockers)。
- 测试状态 (Test status)。