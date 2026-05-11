package migration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/h3xwave/vdb-guardian/internal/fixtures"
)

const maxPGVectorSeedDimension = 2000

var pgvectorSeedIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// PGVectorSeederConfig controls how synthetic fixture records are written into a
// PostgreSQL table backed by pgvector.
//
// The configuration deliberately supports one table, one text identifier column,
// and one vector column for the first Milvus-to-pgvector migration MVP. More
// complex schema mapping belongs in later migration planning steps.
//
// PGVectorSeederConfig 控制着如何将合成的测试固件记录写入由 pgvector 驱动的 PostgreSQL 数据表中。
// 该配置针对首个 Milvus 到 pgvector 的迁移 MVP 做了刻意限制：仅支持单一表、
// 单一的文本标识符列以及单一的向量列。更复杂的 Schema 映射功能已规划在后续的迁移迭代中。
type PGVectorSeederConfig struct {
	Table        string
	IDColumn     string
	VectorColumn string
	Dimension    int
}

// PGVectorSeedResult summarizes a synthetic fixture seeding run.
//
// The result is designed for CLI/job reporting so callers can confirm which
// table and vector dimension were used and how many fixture records were written.
//
// PGVectorSeedResult 总结了一次合成固件数据的灌入执行结果。
// 该结果专为 CLI 或作业报告而设计，以便调用方能够直观地确认
// 数据被写入了哪张表、所使用的向量维度以及成功写入了多少条固件记录。
type PGVectorSeedResult struct {
	Table         string
	Dimension     int
	RecordsTotal  int
	RecordsSeeded int
}

// PGVectorSeeder creates a pgvector table and upserts synthetic fixture records.
//
// It owns write-side database preparation for migration tests. Read/search
// behavior remains in the pgvector connector so seeding and retrieval stay as
// separate enterprise boundaries.
//
// PGVectorSeeder 负责创建 pgvector 数据表，并以更新插入 (upserts) 的方式写入合成记录。
// 它独揽了迁移测试中“目标端数据库写入准备”的职责。与之相对的，数据读取与检索行为
// 依然被保留在 pgvector 连接器中，从而保证了数据灌入 (seeding) 与数据检索 (retrieval)
// 作为两道独立企业边界的清晰性。
type PGVectorSeeder struct {
	config PGVectorSeederConfig
	db     pgvectorSeedDB
}

type pgvectorSeedDB interface {
	Exec(ctx context.Context, sql string, args ...any) error
}

// NewPGVectorSeeder validates configuration and returns a seeder for synthetic
// pgvector fixture data.
//
// A database adapter is required because the seeder performs write-side effects.
// The adapter can be a fake in unit tests or a real pgx-backed implementation in
// a later integration step.
//
// NewPGVectorSeeder 校验配置，并返回一个用于写入合成 pgvector 测试数据的灌入器。
// 由于数据灌入会产生写入层的副作用，因此数据库适配器是必填项。
// 适配器在单元测试中可以是假实现 (fake)；而在稍后的集成测试步骤中，
// 它可以被替换为基于真实的 pgx 驱动的实现。
func NewPGVectorSeeder(config PGVectorSeederConfig, db pgvectorSeedDB) (PGVectorSeeder, error) {
	config = applyPGVectorSeederDefaults(config)
	if err := validatePGVectorSeederConfig(config, db); err != nil {
		return PGVectorSeeder{}, err
	}
	return PGVectorSeeder{config: config, db: db}, nil
}

// Seed creates the pgvector extension/table and upserts all synthetic records.
//
// It validates that the fixture dimension and every record vector match the
// configured pgvector column dimension before executing writes. It returns a
// summary that can be surfaced in migration CLI output.
//
// Seed 创建 pgvector 扩展及对应的数据表，并以更新插入 (upserts) 的方式写入所有合成记录。
// 在执行任何写入操作之前，它会严格校验固件的整体维度以及每一条记录的向量长度，
// 确保它们均与配置的 pgvector 列维度绝对一致。执行完毕后，它将返回一份可供迁移 CLI
// 终端展示的摘要结果。
func (s PGVectorSeeder) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (PGVectorSeedResult, error) {
	if err := validatePGVectorSeedDataset(s.config, dataset); err != nil {
		return PGVectorSeedResult{}, err
	}
	if err := s.exec(ctx, "create pgvector extension", `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return PGVectorSeedResult{}, err
	}
	if err := s.exec(ctx, "create pgvector table", s.createTableSQL()); err != nil {
		return PGVectorSeedResult{}, err
	}
	upsertSQL := s.upsertSQL()
	for _, record := range dataset.Records {
		literal, err := formatPGVectorSeedLiteral(record.Vector)
		if err != nil {
			return PGVectorSeedResult{}, fmt.Errorf("format pgvector seed record %q: %w", record.ID, err)
		}
		if err := s.exec(ctx, "upsert pgvector record", upsertSQL, record.ID, literal); err != nil {
			return PGVectorSeedResult{}, err
		}
	}
	return PGVectorSeedResult{
		Table:         s.config.Table,
		Dimension:     s.config.Dimension,
		RecordsTotal:  len(dataset.Records),
		RecordsSeeded: len(dataset.Records),
	}, nil
}

func applyPGVectorSeederDefaults(config PGVectorSeederConfig) PGVectorSeederConfig {
	if config.Table == "" {
		config.Table = "items"
	}
	if config.IDColumn == "" {
		config.IDColumn = "id"
	}
	if config.VectorColumn == "" {
		config.VectorColumn = "embedding"
	}
	return config
}

func validatePGVectorSeederConfig(config PGVectorSeederConfig, db pgvectorSeedDB) error {
	if db == nil {
		return errors.New("pgvector seed database adapter is required")
	}
	if config.Dimension <= 0 || config.Dimension > maxPGVectorSeedDimension {
		return fmt.Errorf("pgvector seed dimension must be in range 1..%d", maxPGVectorSeedDimension)
	}
	if err := validatePGVectorSeedIdentifier("table", config.Table); err != nil {
		return err
	}
	if err := validatePGVectorSeedIdentifier("id column", config.IDColumn); err != nil {
		return err
	}
	if err := validatePGVectorSeedIdentifier("vector column", config.VectorColumn); err != nil {
		return err
	}
	return nil
}

func validatePGVectorSeedDataset(config PGVectorSeederConfig, dataset fixtures.SyntheticDataset) error {
	if dataset.Dimension != config.Dimension {
		return fmt.Errorf("synthetic dataset dimension %d does not match pgvector seed dimension %d", dataset.Dimension, config.Dimension)
	}
	for index, record := range dataset.Records {
		if record.ID == "" {
			return fmt.Errorf("synthetic record at index %d has empty id", index)
		}
		if len(record.Vector) != config.Dimension {
			return fmt.Errorf("synthetic record %q vector dimension %d does not match pgvector seed dimension %d", record.ID, len(record.Vector), config.Dimension)
		}
	}
	return nil
}

func (s PGVectorSeeder) exec(ctx context.Context, operation string, sql string, args ...any) error {
	if err := s.db.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func (s PGVectorSeeder) createTableSQL() string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (%s TEXT PRIMARY KEY, %s vector(%d) NOT NULL)`,
		quotePGVectorSeedIdentifier(s.config.Table),
		quotePGVectorSeedIdentifier(s.config.IDColumn),
		quotePGVectorSeedIdentifier(s.config.VectorColumn),
		s.config.Dimension,
	)
}

func (s PGVectorSeeder) upsertSQL() string {
	return fmt.Sprintf(
		`INSERT INTO %s (%s, %s) VALUES ($1, $2::vector) ON CONFLICT (%s) DO UPDATE SET %s = EXCLUDED.%s`,
		quotePGVectorSeedIdentifier(s.config.Table),
		quotePGVectorSeedIdentifier(s.config.IDColumn),
		quotePGVectorSeedIdentifier(s.config.VectorColumn),
		quotePGVectorSeedIdentifier(s.config.IDColumn),
		quotePGVectorSeedIdentifier(s.config.VectorColumn),
		quotePGVectorSeedIdentifier(s.config.VectorColumn),
	)
}

func validatePGVectorSeedIdentifier(label string, value string) error {
	if !pgvectorSeedIdentifierPattern.MatchString(value) {
		return fmt.Errorf("invalid pgvector seed %s identifier %q", label, value)
	}
	return nil
}

func quotePGVectorSeedIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func formatPGVectorSeedLiteral(vector []float64) (string, error) {
	if len(vector) == 0 {
		return "", errors.New("pgvector seed vector must not be empty")
	}
	parts := make([]string, len(vector))
	for index, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "", fmt.Errorf("pgvector seed vector contains non-finite value at index %d", index)
		}
		parts[index] = strconv.FormatFloat(value, 'g', -1, 64)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}
