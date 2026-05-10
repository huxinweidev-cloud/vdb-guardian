package migration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/fixtures"
)

const maxPGVectorSeedDimension = 2000

var pgvectorSeedIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// PGVectorSeederConfig controls how synthetic fixture records are written into a
// PostgreSQL table backed by pgvector.
//
// The configuration deliberately supports one table, one text identifier column,
// and one vector column for the first Milvus-to-pgvector migration MVP. More
// complex schema mapping belongs in later migration planning steps.
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
