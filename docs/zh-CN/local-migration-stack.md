# 本地 Milvus 到 pgvector 迁移技术栈 (Local Migration Stack)

本地迁移技术栈 (Local migration stack) 为首个 Milvus 到 pgvector 的迁移与验证 MVP (最简可行产品) 提供了必要的数据库服务支持。需要着重强调的是，该技术栈**仅限本地使用**，严禁将其作为生产环境的基础设施。

## 包含的服务 (Services)

该技术栈的定义文件位于：

```text
deployments/docker-compose.migration.yml
```

包含以下服务：

- `milvus-etcd`: Milvus 单机版 (standalone) 依赖的 etcd 服务。
- `milvus-minio`: Milvus 单机版依赖的对象存储 (object storage) 服务。
- `milvus-standalone`: 充当数据源 (source) 的 Milvus 向量数据库。
- `postgres-pgvector`: 已初始化 pgvector 扩展、充当目标库 (target) 的 PostgreSQL 数据库。

## 端口映射 (Ports)

宿主机默认暴露的端口如下：

| 服务 (Service) | 端口 (Port) | 用途 (Purpose) |
| --- | ---: | --- |
| Milvus | 19530 | gRPC SDK 访问端点 |
| Milvus | 9091 | HTTP 健康检查与监控指标端点 |
| PostgreSQL | 5432 | PostgreSQL 访问端点 |
| MinIO | 9000 | 兼容 S3 的 API 端点 |
| MinIO | 9001 | MinIO 控制台 (console) |
| etcd | 2379 | etcd 客户端访问端点 |

您可以通过环境变量来覆盖这些默认端口，例如：

```bash
VDB_GUARDIAN_POSTGRES_PORT=15432 \
VDB_GUARDIAN_MILVUS_PORT=19531 \
docker compose -f deployments/docker-compose.migration.yml up -d
```

## 本地专属凭据 (Local-only credentials)

PostgreSQL 服务使用了专供本地使用的默认凭据：

```text
POSTGRES_DB=vdb_guardian
POSTGRES_USER=vdb_guardian
POSTGRES_PASSWORD=vdb_guardian_local_password
```

**这些绝对不是生产级别的凭据。** 切勿在本地开发环境之外的任何地方重复使用它们。

## 仅校验配置 (Validate without starting containers)

您可以使用以下命令检查配置是否正确：

```bash
make migration-stack-config
```

或执行底层脚本：

```bash
scripts/check-migration-stack.sh config
```

该操作只会校验 Compose 文件的语法，**不会**真正创建容器、网络或数据卷。

## 启动技术栈 (Start the stack)

启动 Docker 意味着会产生副作用 (side effects)：它将创建实际的容器、网络与数据卷。

```bash
make migration-stack-up
```

等效的底层命令：

```bash
docker compose -f deployments/docker-compose.migration.yml up -d
```

## 检查状态 (Check status)

```bash
make migration-stack-status
```

## 健康检查 (Health checks)

在技术栈运行后，您应该验证 PostgreSQL 与 pgvector 是否就绪：

```bash
scripts/check-migration-stack.sh postgres
```

验证 Milvus gRPC 端口是否开放：

```bash
scripts/check-migration-stack.sh milvus-port
```

## 停止并清理容器 (Stop and remove containers)

```bash
make migration-stack-down
```

此命令会删除所有相关容器与 Compose 专属网络。除非您手动清理，否则具名数据卷 (Named volumes) 将会被保留。

## pgvector 目标端灌入冒烟测试 (pgvector seed smoke check)

确认 PostgreSQL pgvector 服务健康后，您可以通过以下命令验证目标端的数据灌入流水线：

```bash
go run ./cmd/vdbg seed-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --id-column id \
  --vector-column embedding
```

随后，验证被灌入的数据行数及向量维度：

```bash
docker compose -f deployments/docker-compose.migration.yml exec -T postgres-pgvector psql \
  -U vdb_guardian \
  -d vdb_guardian \
  -c "SELECT COUNT(*) AS seeded_records FROM items; SELECT id, vector_dims(embedding) AS dims FROM items ORDER BY id LIMIT 3;"
```

对于已提交在代码库中的 `synthetic-small.json` 固件，预期的行数为 `100`，向量维度应为 `8`。

## pgvector 目标端检索冒烟测试 (pgvector search smoke check)

数据灌入成功后，您可以使用以下命令验证目标端的检索链路：

```bash
go run ./cmd/vdbg search-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

对于同一份小固件，当使用 `--expand-k 5` 参数时，终端应该打印出 `5` 条命中记录。

## pgvector 目标端指纹产物构建校验 (pgvector target fingerprint artifact check)

检索测试通过后，即可基于固件中的所有查询请求，构建目标端的指纹产物：

```bash
go run ./cmd/vdbg build-pgvector-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --output /tmp/vdb-guardian-target-fingerprint.json \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

验证生成的 JSON 产物结构是否合法：

```bash
python -m json.tool /tmp/vdb-guardian-target-fingerprint.json >/dev/null
```

预期该产物中将包含 `10` 个查询级指纹。

## Milvus 源端灌入冒烟测试 (Milvus seed smoke check)

确认 Milvus 单机服务健康后，通过真实的 Milvus Go SDK 适配器，将固件数据灌入源端集合：

```bash
go run ./cmd/vdbg seed-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --metric cosine
```

预期的行数为 `100`，向量维度应为 `8`。

## Milvus 源端检索冒烟测试 (Milvus search smoke check)

源端灌入成功后，使用以下命令验证源端检索链路：

```bash
go run ./cmd/vdbg search-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

预期终端将打印出 `5` 条命中记录。

## Milvus 源端指纹产物构建校验 (Milvus source fingerprint artifact check)

基于所有查询请求，构建源端的指纹产物：

```bash
go run ./cmd/vdbg build-milvus-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --output /tmp/vdb-guardian-source-fingerprint.json \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

验证生成的 JSON 产物结构是否合法：

```bash
python -m json.tool /tmp/vdb-guardian-source-fingerprint.json >/dev/null
```

预期产物中将包含 `10` 个查询级指纹。

## 源/目标产物比对校验 (Source/target artifact comparison check)

当源端与目标端的产物文件均已就绪后，驱动 Python 引擎执行最终的比对：

```bash
go run ./cmd/vdbg compare-artifacts \
  --source /tmp/vdb-guardian-source-fingerprint.json \
  --target /tmp/vdb-guardian-target-fingerprint.json \
  --artifact-dir /tmp/vdb-guardian-compare \
  --job-id real-artifact-smoke
```

该命令将产出结果文件：

```text
/tmp/vdb-guardian-compare/real-artifact-smoke-result.json
```

由于两个产物均基于同一份测试固件及兼容的参数配置生成，比对报告应显示 `matched_queries: 10`，且不存在源端或目标端缺失查询的情况。具体的距离指标分数将取决于这两个真实数据库在底层检索行为上的细微差异。

## Milvus 连接器极简测试 (Milvus connector smoke check)

执行底层的 Milvus 就绪检查，仅用于验证 gRPC SDK 端口是否可达：

```bash
scripts/check-migration-stack.sh milvus-port
```

## 当前局限性 (Current limitations)

目前，该技术栈已经完整支持了 pgvector (目标端) 与 Milvus (源端) 的固件灌入、检索验证、产物构建，以及最终的双端产物比对流水线验证。然而，它**尚未**支持从 Milvus 向 pgvector 进行全量数据的自动迁移，也未能一键执行“迁移并验证”的完整串联工作流。这些高级能力将在后续的迁移 MVP 迭代中逐步落地。