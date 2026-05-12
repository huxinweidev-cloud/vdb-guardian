# 最小化向量迁移运行器

内部向量迁移运行器 (vector migration runner) 构建了首个不依赖具体数据库特性的抽象边界，用于将数据记录从 Milvus 源端读取器平滑迁移至 pgvector 目标端写入器。

该运行器现已被公有 `vdbg migrate` CLI 封装，从而打通了首条真实的 Milvus 到 pgvector 数据传输路径。

## 支持范围 (Scope)

目前已实现：

- 提供纯迁移用途的 `vdbg migrate` CLI。
- 提供一次性自动执行迁移加验证的 `vdbg migrate-and-verify` CLI。
- 强制固定的源集合名称与目标表名称。
- 强制固定的向量维度校验逻辑。
- 能够返回规范化抽象记录模型的源端读取器边界。
- 能够接受规范化抽象记录模型的目标端写入器边界。
- 经过单元测试覆盖的 Milvus 迁移源端适配器边界。
- 经过单元测试覆盖的 pgvector 迁移目标端适配器边界。
- 真实基于 Milvus SDK 检索驱动的迁移读取器实现。
- 真实基于 pgx 驱动的 pgvector 迁移写入器实现。
- 写入数据库之前的防御性向量内存拷贝拷贝操作。
- 面向上下文 (Context) 取消信号的响应与中断检查。
- 为优化诊断而封装的读/写详细错误信息。
- 全面覆盖：成功路径、默认值回退、非法配置拦截、无效数据阻断、读取报错、写入报错以及上下文取消场景的单元测试。

尚未实现：

- 元数据字段列同步。
- Milvus 分区过滤与映射。
- 增量检查点与断点续传。
- 生产环境级别的高速批量导入优化。

## Go 语言架构边界

运行器的核心逻辑位于以下路径：

```text
internal/migration/vector_migration.go
```

核心结构体模型：

```go
type VectorMigrationConfig struct {
    SourceCollection string
    TargetTable      string
    Dimension        int
    BatchSize        int
}

type VectorMigrationRecord struct {
    ID     string
    Vector []float64
}

type VectorMigrationResult struct {
    SourceCollection string
    TargetTable      string
    Dimension        int
    RecordsRead      int
    RecordsWritten   int
}
```

系统内部的源端与目标端读写边界接口定义：

```go
type vectorMigrationSource interface {
    ReadRecords(ctx context.Context, collection string) ([]VectorMigrationRecord, error)
}

type vectorMigrationTarget interface {
    WriteRecords(ctx context.Context, table string, records []VectorMigrationRecord) error
}
```

基于设计考量，这些接口均被限制为包内私有（package-private）。所有对接真实 SDK 与 SQL 的具体适配器实现代码均在同一个包内共存，从而在不对外暴露底层不稳定适配细节的前提下，完成系统的解耦。

## 默认值 (Defaults)

当对应参数被省略时：

- `SourceCollection`: 默认为 `items`
- `TargetTable`: 默认为 `items`
- `BatchSize`: 默认为 `100`

`Dimension` 是必须显式指定的参数，且取值范围必须被限定在 `1..2000` 之间，以匹配目前 pgvector 兼容合成验证数据源的工程极限。

## 数据校验逻辑

在开始处理数据之前，运行器将直接拒绝（阻断）下列非法输入：

- 未传入有效的源端读取器。
- 未传入有效的目标端写入器。
- 源集合或目标表使用了非法的命名标识符。
- 负数维度或超出了框架允许的最大维度限制。
- 负数或零等非法的批处理大小设定。
- 源端数据出现了空字符串形式的 `ID` 字段。
- 发生运行时维度错位（某条数据的维度与设定值不同）。
- 向量数值内包含了 `NaN` (非数字) 或是无限大(`infinite`) 的浮点脏数据。

## 测试命令

```bash
go test ./internal/migration -run 'TestVectorMigration|TestNewVectorMigration' -v
```

提交前完整检查流程：

```bash
make fmt
make lint
make test
git diff --check
```

## 下一步演进计划

未来将为系统引入更多面向生产级任务所需的迁移语义，例如加入对元数据字段和 Milvus 分区的支持，以及实现增量检查点记录和目标端的自动化数据清理策略。
