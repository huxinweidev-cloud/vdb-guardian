# Milvus Connector

The minimal Milvus connector implements the shared `connectors.Connector` interface for source-side retrieval behavior collection in the Milvus to pgvector migration MVP.

## Current scope

Implemented capabilities:

- Validate Milvus connector configuration.
- Create a placeholder adapter from a Milvus address.
- Connect, count, search, and close through a small adapter boundary.
- Convert Milvus search hits into normalized `connectors.SearchResponse` values.
- Normalize metric score direction so larger `SearchHit.Score` values are better.

Not yet implemented:

- Real Milvus SDK network calls.
- Collection creation.
- Index creation or load orchestration.
- Fixture seeding into Milvus.
- Metadata filters or Milvus boolean expressions.
- Integration tests against the Docker migration stack.

The first version keeps SDK details behind an adapter boundary so connector normalization is tested without Docker or network state. The real SDK adapter will be filled in during the migration/integration steps.

## Configuration

The connector is configured with `MilvusConfig`:

```go
type MilvusConfig struct {
    Name              string
    Address           string
    DefaultCollection string
    IDField           string
    VectorField       string
    Metric            string
}
```

Defaults:

```text
Name:              milvus
DefaultCollection: items
IDField:           id
VectorField:       embedding
Metric:            cosine
```

The local Docker stack exposes Milvus at `localhost:19530` by default, but production or shared environment addresses must be supplied through runtime configuration. Do not commit real credentials or private endpoints.

## Search behavior

The connector maps the shared `SearchRequest` into a Milvus adapter request:

```text
SearchRequest.Collection  -> collection name, or DefaultCollection when empty
SearchRequest.QueryVector -> query vector
SearchRequest.ExpandK     -> Milvus search limit
SearchRequest.Params      -> connector-specific search params
```

The connector returns ranked hits in Milvus result order:

```go
SearchResponse{
    Hits: []SearchHit{
        {ID: "vec-000001", Rank: 1, Score: 0.98},
    },
}
```

`ExpandK` is used as the search limit so the fingerprint builder can observe boundary candidates around the business `TopK` cutoff.

## Score normalization

The project-wide normalized score rule is:

```text
larger SearchHit.Score means better match
```

Milvus metric handling:

```text
cosine: pass score through
ip:     pass score through
l2:     convert distance to negative score
```

This keeps Milvus output comparable with pgvector and future connectors.

## Safe identifiers

Milvus collection and field names are restricted to simple identifiers:

```text
^[A-Za-z_][A-Za-z0-9_]*$
```

Supported examples:

```text
items
embedding
id
```

Rejected examples:

```text
items;drop
public.items
"items"
```

Although Milvus is not SQL, rejecting unsafe dynamic names prevents accidental SDK misuse and keeps configuration behavior predictable.

## MVP role

This connector is the source-side search connector for the migration MVP. The intended later flow is:

```text
synthetic fixture records
        ↓
seed Milvus collection
        ↓
Milvus Search(query vectors)
        ↓
connectors.SearchResponse
        ↓
fingerprint artifact builder
        ↓
verification runner
```

After the real SDK adapter and fixture seeding are added, this connector will feed source-side retrieval behavior into the same artifact comparison path already used by local offline verification.
