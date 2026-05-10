# pgvector Fixture Seeding

pgvector fixture seeding writes deterministic synthetic vector records into a PostgreSQL table backed by the pgvector extension. It is the write-side preparation step for the Milvus to pgvector migration-and-verification MVP.

## Current scope

Implemented capabilities:

- Validate pgvector seeder configuration.
- Validate synthetic fixture dimensions and record vectors.
- Create the `vector` extension if needed.
- Create a simple pgvector table if needed.
- Upsert fixture records by deterministic ID.
- Return a structured summary with table, dimension, and record counts.

Not yet implemented:

- CLI command for real database seeding.
- pgx-backed production adapter for this seeder.
- Docker integration tests against the migration stack.
- Index creation such as HNSW or IVFFlat.
- Metadata columns or complex schema mapping.
- Milvus fixture seeding.

The first version is intentionally database-adapter driven so the SQL behavior can be unit-tested without starting Docker or connecting to PostgreSQL.

## API

The seeder lives in `internal/migration`:

```go
type PGVectorSeederConfig struct {
    Table        string
    IDColumn     string
    VectorColumn string
    Dimension    int
}

type PGVectorSeeder struct {
    // constructed through NewPGVectorSeeder
}

func NewPGVectorSeeder(config PGVectorSeederConfig, db pgvectorSeedDB) (PGVectorSeeder, error)
func (s PGVectorSeeder) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (PGVectorSeedResult, error)
```

Defaults:

```text
Table:        items
IDColumn:     id
VectorColumn: embedding
```

`Dimension` is required and must match the synthetic fixture dimension.

## SQL behavior

The seeder performs these operations in order:

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

Vector values are passed as pgvector literals such as:

```text
[0.1,0.2,0.3]
```

The upsert behavior makes the same fixture safe to seed repeatedly during local migration experiments.

## Validation

The seeder rejects:

- Missing database adapter.
- Dimensions outside `1..2000`.
- Dataset dimension mismatches.
- Empty record IDs.
- Record vectors whose length does not match the configured dimension.
- Empty vectors, `NaN`, or infinite vector values.
- Unsafe table or column identifiers.

Identifiers must match:

```text
^[A-Za-z_][A-Za-z0-9_]*$
```

Accepted examples:

```text
items
id
embedding
items_2026
```

Rejected examples:

```text
items;drop
public.items
"items"
items-name
```

## MVP role

The intended migration loop is:

```text
synthetic fixture records
        ↓
PGVectorSeeder
        ↓
pgvector table
        ↓
PGVectorConnector.Search(query vectors)
        ↓
target fingerprint artifact
```

After Milvus fixture seeding and real database adapters are added, pgvector seeded data will be used as the target-side dataset for migration verification and retrieval behavior comparison.
