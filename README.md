# dalgo2http

HTTP/JSON adapter for [DALgo](https://github.com/dal-go/dalgo): expose read-only REST endpoints (public reference data, internal JSON APIs) as DALgo collections so that the same `dal.Query` model, and the same access policies, apply to them as to SQL, SQLite, Firestore and inGitDB sources.

Status: v0.x implemented 2026-09-09 (founder decision: a generic DALgo adapter for HTTP rather than a consumer-private fetcher). First consumer: DataTug's demo knowledge project. The two example descriptors under `examples/` (CountriesNow currency-by-country, Frankfurter exchange rates) replace REST Countries — restcountries.com's public v3.1 API is now fully deprecated; see `examples/countries/README.md`.

## Design constraints (do not relax without a recorded decision)

- **Declarative collections.** Each collection is a descriptor: URL template, HTTP method (GET only in v0.x of this adapter), which query fields map to path/query parameters, the JSON path to the rows, the key field, timeout. Descriptors carry no secrets; header values come from the environment.
- **Fail closed on pushdown.** A `dal.Query` is executed only when every condition can be expressed by the endpoint (equality on declared parameter fields). Anything else returns a typed "not supported" error. The adapter never fetches a superset and filters client-side unless the descriptor explicitly opts in, because an access-policy predicate that cannot be pushed down must refuse, not leak.
- **Declared capabilities.** Callers can ask the adapter what it supports per collection so a policy layer can decide before executing.
- **Snapshots.** An optional recorded snapshot store answers when the endpoint is unreachable; every result says whether it came from `live` or `snapshot`.
- **Read-only.** Writes and transactions that mutate return "not supported".

## Usage

```go
db, err := dalgo2http.NewDB(dalgo2http.Config{
	Collections: []dalgo2http.Collection{
		{
			Name:        "countries",
			URLTemplate: "https://countriesnow.space/api/v0.1/countries/currency/q?country={name}",
			KeyField:    "name",
			RowsPath:    "data",
			Params:      map[string]dalgo2http.Param{"name": {Location: dalgo2http.ParamQuery}},
			Timeout:     10 * time.Second,
		},
	},
	// Snapshots: os.DirFS("testdata"), // optional recorded-fixture fallback
	// Mode:      dalgo2http.ModeLiveThenSnapshot, // the default
})

// Get by key (only works when KeyField is itself a declared Param — see
// Capabilities.SupportsGet below):
target := map[string]any{}
rec := record.NewRecordWithData(record.NewKeyWithID("countries", "France"), &target)
err = db.Get(ctx, rec)

// Query: only an equality condition on a declared Param field is pushed
// into the URL; anything else fails closed with dal.ErrNotSupported unless
// the collection sets ClientSideFilter.
q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
	Where(dal.WhereField("name", dal.Equal, "France")).
	SelectIntoRecord(nil)
reader, err := db.ExecuteQueryToRecordsReader(ctx, q)

// Capabilities is an optional adapter capability (see dal.As's doc comment
// on why a plain type assertion on a dal.DB does not see it):
caps, err := dal.As[dalgo2http.CapabilitiesProvider](db)
```

Building a recorded-snapshot fixture (used both for offline tests and as the
live-request fallback the design constraints describe):

```go
path, err := dalgo2http.Record(ctx, http.DefaultClient, coll, map[string]string{"name": "France"}, "testdata")
```

See `examples/countries` and `examples/frankfurter` for complete, runnable
descriptors with recorded fixtures and offline tests.

## Descriptor reference

| Field              | Meaning |
|--------------------|---------|
| `Name`             | Collection name a `record.Key` or `dal.Query.From()` names. |
| `URLTemplate`      | Request URL with `{name}` placeholders for declared `Params`. A placeholder can sit in the path or be embedded in a literal query string (e.g. `...?symbols={to}`); its declared `Param.Location` decides the escaping used, not its position in the string. |
| `Method`           | Empty or `dalgo2http.MethodGET` — this adapter is read-only GET-only in v0.x. |
| `Params`           | `map[string]Param{name: {Location: ParamPath \| ParamQuery}}`. A `ParamQuery` entry whose name never appears in `URLTemplate` is instead appended as an extra `?name=value` when a query supplies a value for it. |
| `RowsPath`         | Dot-separated JSON object-field path to the rows. Empty means the response root: a JSON array of row objects, or a single JSON object treated as one row. |
| `KeyField`         | The row field that becomes a `record.Key`'s ID. `Get`/`Exists` only work when `KeyField` is ALSO a declared `Param` — see `Capabilities.SupportsGet` — otherwise they fail closed with `dal.ErrNotSupported`. |
| `Headers`          | `map[headerName]envVarName`. A header value is never a literal in a descriptor; an unset/empty variable means the header is simply not sent. |
| `Timeout`          | Bounds one request to this collection's endpoint. Zero means no adapter-imposed timeout beyond the context's own deadline. |
| `ClientSideFilter` | Opt-in escape hatch for a `dal.Query` condition that cannot be fully pushed into the URL: the equality parts that CAN be pushed still narrow the request, and the remainder is evaluated in memory. Set this ONLY on a collection where over-fetching cannot leak anything a caller was not already allowed to see (public reference data) — see the "Fail closed on pushdown" design constraint above. |

`Config` also loads from YAML or JSON via `LoadConfigYAML`/`LoadConfigJSON`
(a `collections:` list of the fields above, plus a repo-level `mode:`; a
collection's `timeout` is a duration string like `"10s"`).

## Query support

`ExecuteQueryToRecordsReader` (via `dal.StructuredQuery`) supports:

- An equality condition (`dal.Equal`) on a declared `Param` field, combined with `AND` (a `GroupCondition` with any other operator, or a bare `OR`, is not pushable and fails closed unless `ClientSideFilter` is set).
- `Limit`, applied AFTER fetching (never pushed into the request).

It does not support (fails closed with `dal.ErrNotSupported`): joins, `GROUP BY`/`HAVING`, `ORDER BY`, `Offset`, or start cursors.

`ExecuteQueryToRecordsetReader` is not implemented (returns `dal.ErrNotSupported`): this adapter's rows are schemaless HTTP/JSON objects, and `recordset.Recordset`'s typed columnar shape is not something a declarative descriptor can derive without a schema. `dalgo2fs`, the reference minimal read-only adapter, makes the same choice for the same reason.

## Spec

See `spec/features/http-json-adapter/README.md`.
