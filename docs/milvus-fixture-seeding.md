# Milvus Fixture Seeding

Milvus fixture seeding prepares deterministic synthetic vector records for a Milvus collection. It is the source-side write preparation step for the Milvus to pgvector migration-and-verification MVP.

## Current scope

Implemented capabilities:

- Validate Milvus seeder configuration.
- Validate synthetic fixture dimensions and record vectors.
- Create a minimal collection boundary through an injected adapter.
- Insert synthetic records through an injected adapter.
- Return a structured summary with collection, dimension, and record counts.

Not yet implemented:

- Real Milvus Go SDK adapter.
- CLI command for real database seeding.
- Docker integration tests against the migration stack.
- Collection index creation.
- Collection loading orchestration.
- Partitions or metadata fields.
- Query vector insertion.

The first version is intentionally adapter-driven so seeding behavior can be unit-tested without starting Docker or connecting to Milvus.

## API

The seeder lives in `internal/migration`:

```go
type MilvusSeederConfig struct {
    Collection  string
    IDField     string
    VectorField string
    Dimension   int
    Metric      string
}

type MilvusSeeder struct {
    // constructed through NewMilvusSeeder
}

func NewMilvusSeeder(config MilvusSeederConfig, db milvusSeedDB) (MilvusSeeder, error)
func (s MilvusSeeder) Seed(ctx context.Context, dataset fixtures.SyntheticDataset) (MilvusSeedResult, error)
```

Defaults:

```text
Collection:  items
IDField:     id
VectorField: embedding
Metric:      cosine
```

`Dimension` is required and must match the synthetic fixture dimension.

## Adapter behavior

The seeder calls the injected adapter in this order:

```text
CreateCollection(collection, id_field, vector_field, dimension, metric)
InsertRecords(collection, id_field, vector_field, records)
```

Records are copied before they are passed to the adapter so later caller-side mutation of the fixture does not change the inserted batch representation.

## Validation

The seeder rejects:

- Missing database adapter.
- Dimensions outside `1..2000`.
- Unsupported metrics.
- Dataset dimension mismatches.
- Empty record IDs.
- Record vectors whose length does not match the configured dimension.
- `NaN` or infinite vector values.
- Unsafe collection or field identifiers.

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
MilvusSeeder
        ↓
Milvus collection
        ↓
MilvusConnector.Search(query vectors)
        ↓
source fingerprint artifact
```

Together with pgvector fixture seeding, this gives both source and target sides a consistent write-side preparation boundary before the project adds real SDK adapters, Docker integration tests, and the final migrate-and-verify CLI.
