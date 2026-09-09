# dalgo2http

HTTP/JSON adapter for [DALgo](https://github.com/dal-go/dalgo): expose read-only REST endpoints (public reference data, internal JSON APIs) as DALgo collections so that the same `dal.Query` model, and the same access policies, apply to them as to SQL, SQLite, Firestore and inGitDB sources.

Status: bootstrapped 2026-09-09 (founder decision: a generic DALgo adapter for HTTP rather than a consumer-private fetcher). First consumer: DataTug's demo knowledge project (REST Countries, Frankfurter exchange rates).

## Design constraints (do not relax without a recorded decision)

- **Declarative collections.** Each collection is a descriptor: URL template, HTTP method (GET only in v0.x of this adapter), which query fields map to path/query parameters, the JSON path to the rows, the key field, timeout. Descriptors carry no secrets; header values come from the environment.
- **Fail closed on pushdown.** A `dal.Query` is executed only when every condition can be expressed by the endpoint (equality on declared parameter fields). Anything else returns a typed "not supported" error. The adapter never fetches a superset and filters client-side unless the descriptor explicitly opts in, because an access-policy predicate that cannot be pushed down must refuse, not leak.
- **Declared capabilities.** Callers can ask the adapter what it supports per collection so a policy layer can decide before executing.
- **Snapshots.** An optional recorded snapshot store answers when the endpoint is unreachable; every result says whether it came from `live` or `snapshot`.
- **Read-only.** Writes and transactions that mutate return "not supported".

See `spec/` for the Feature and plan once written.
