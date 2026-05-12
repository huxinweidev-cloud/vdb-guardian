package migration

// Default collection, table, and field names shared across seeders, migration
// adapters, and their unit tests so repeated string literals don't trigger
// golangci-lint goconst warnings.
//
// 默认的集合、表和字段名称。这些常量在种子数据生成器 (seeders)、迁移适配器及其
// 单元测试之间共享，以避免重复的字符串字面量触发 golangci-lint 的 goconst 警告。
const (
	DefaultSeedCollection  = "items"
	DefaultSeedIDField     = "id"
	DefaultSeedVectorField = "embedding"
)
