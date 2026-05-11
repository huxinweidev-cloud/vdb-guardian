# pgvector 测试数据灌入 (pgvector Fixture Seeding)

pgvector 测试数据灌入机制 (Fixture Seeding) 负责将确定性的合成向量记录写入底层启用了 pgvector 扩展的 PostgreSQL 数据表中。在 Milvus 到 pgvector 的迁移与验证 MVP 中，这是目标库极其关键的数据准备步骤。

## 当前范围 (Current scope)

已实现的能力：

- 校验 pgvector 灌入器 (seeder) 配置的合法性。
- 校验合成测试固件 (synthetic fixture) 的维度及记录向量是否合规。
- 按需自动在数据库中创建 (Create) `vector` 扩展。
- 按需自动创建用于存储向量数据的极简 PostgreSQL 数据表。
- 通过稳定的确定性 ID，执行记录的无冲突写入或更新 (Upsert)。
- 返回结构化的灌入摘要，包含表名、维度及成功写入的行数。

尚未实现：

- 针对真实数据库进行一键灌入的独立 CLI 命令。
- 基于 `pgx` 驱动、对接该灌入器的生产级底层适配器。
- 针对 Docker 迁移技术栈的完整自动化集成测试。
- 诸如 HNSW 或 IVFFlat 索引的自动化构建管理。
- 元数据列 (Metadata columns) 及更复杂的 Schema 映射支持。
- Milvus 端的测试数据灌入打通。

首个版本刻意采用了数据库适配器驱动 (database-adapter driven) 的设计架构，这使得核心的 SQL 生成及流转行为在不启动 Docker、不连接 PostgreSQL 的情况下也能进行充分的单元测试。

## API 设计 (API)

灌入器相关的代码位于 `internal/migration` 包内：

```go
type PGVectorSeederConfig struct {
    Table        string
    IDColumn     string
    VectorColumn string
    Dimension    int
}

type PGVectorSeeder struct {
    // 需通过 NewPGVectorSeeder 函数进行构造
}

func NewPGVectorSeeder(config PGVectorSeederConfig, db pgvectorSeedDB) (PGVectorSeeder, error)
func (s PGVectorSeeder) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (PGVectorSeedResult, error)
```

默认配置值：

```text
Table:        items
IDColumn:     id
VectorColumn: embedding
```

其中 `Dimension` (维度) 是必填项，且必须与正在处理的合成测试固件中声明的维度严格匹配。

## SQL 行为 (SQL behavior)

灌入器会依次执行以下 SQL 指令流：

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

```sql
CREATE TABLE IF NOT EXISTS "items" (
  "id" TEXT PRIMARY KEY,
  "embedding" vector(8) NOT NULL
);
```

```sql
INSERT INTO "items" ("id", "embedding")
VALUES ($1, $2::vector)
ON CONFLICT ("id")
DO UPDATE SET "embedding" = EXCLUDED."embedding";
```

在这个过程中，向量数据会被格式化为符合 pgvector 要求的文本字面量 (literals) 进行传递，例如：

```text
[0.1,0.2,0.3]
```

得益于 `ON CONFLICT ... DO UPDATE` (Upsert) 的幂等设计，在本地进行各种迁移调试实验时，无论重复灌入同一份固件多少次都是绝对安全的，绝不会引发主键冲突异常。

## 验证与容错 (Validation)

灌入器会主动拒绝并拦截以下情况：

- 缺失必要的数据库适配器。
- `Dimension` 的值超出 `1..2000` 的合法范围。
- `Dimension` 配置与实际数据集中声明的维度不匹配。
- 存在空的记录 ID。
- 记录中的向量长度与配置的 `Dimension` 不一致。
- 存在空向量、包含非数字 (`NaN`) 或无穷大 (infinite) 值的异常向量。
- 表名或列名使用了不安全的标识符。

标识符必须符合以下正则表达式规范以防范注入风险：

```text
^[A-Za-z_][A-Za-z0-9_]*$
```

支持的标识符示例：

```text
items
id
embedding
items_2026
```

被拦截拒绝的非法示例：

```text
items;drop
public.items
"items"
items-name
```

## MVP 中的角色 (MVP role)

预期的完整迁移闭环如下：

```text
合成的测试固件记录 (synthetic fixture records)
        ↓
PGVectorSeeder (执行目标端灌入)
        ↓
PostgreSQL 内部的 pgvector 表
        ↓
PGVectorConnector.Search(使用固件中的查询向量执行检索)
        ↓
生成目标端指纹产物 (target fingerprint artifact)
```

待到 Milvus 端的固件灌入及真实的数据库适配器被完整拼装后，灌入 pgvector 的这批数据将被正式确立为迁移验证的比对基准，用于深度测算并评估异构数据库间的检索行为一致性。