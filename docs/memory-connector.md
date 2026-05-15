# Memory Connector

The memory connector is a deterministic connector implementation for local verification and tests. It implements the `internal/connectors.Connector` interface without contacting a real vector database.

## Purpose

The connector lets the project exercise connector normalization before Milvus and pgvector implementations exist:

```text
precomputed ranked hits
        |
        v
MemoryConnector.Search
        |
        v
connectors.SearchResponse
        |
        v
fingerprint artifact builder
        |
        v
verification runner
```

This keeps local end-to-end verification independent from Docker, SDKs, SQL, and network failures.

## Construction

```go
connector := connectors.NewMemoryConnector("memory-source", map[string][]connectors.SearchHit{
    "q-1": {
        {ID: "a", Rank: 1, Score: 0.99},
        {ID: "b", Rank: 2, Score: 0.95},
        {ID: "c", Rank: 3, Score: 0.90},
    },
})
```

The constructor deep-copies and rank-sorts the configured hits so caller mutations do not affect connector behavior.

## Search behavior

The current memory connector uses `SearchRequest.Collection` as the query key. This is a temporary local-verification convention until real connectors search by `QueryVector` against a database collection.

```go
response, err := connector.Search(ctx, connectors.SearchRequest{
    Collection: "q-1",
    TopK:       2,
    ExpandK:    3,
})
```

The response contains the first `ExpandK` hits ordered by ascending rank.

## Validation

`Search` rejects:

- canceled contexts;
- `TopK <= 0`;
- `ExpandK <= 0`;
- `ExpandK < TopK`;
- empty collection/query key;
- missing query keys;
- query keys with fewer than `ExpandK` hits.

## Lifecycle methods

`Connect`, `Count`, `Search`, and `Close` are implemented so the connector satisfies the full enterprise connector contract. `Connect` and `Close` perform no network I/O.

## Limitations

The memory connector is not a migration source or target. It is only for deterministic local verification, unit tests, and future offline end-to-end tests before Milvus and pgvector are connected.
