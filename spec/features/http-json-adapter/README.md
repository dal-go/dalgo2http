---
format: https://specscore.md/feature-specification
status: Implementing
---

# Feature: HTTP/JSON adapter

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/dal-go/dalgo2http/spec/features/http-json-adapter?op=explore) | [Edit](https://specscore.studio/app/github.com/dal-go/dalgo2http/spec/features/http-json-adapter?op=edit) | [Ask question](https://specscore.studio/app/github.com/dal-go/dalgo2http/spec/features/http-json-adapter?op=ask) | [Request change](https://specscore.studio/app/github.com/dal-go/dalgo2http/spec/features/http-json-adapter?op=request-change) |
**Status:** Implementing
**Source Ideas:** —

## Summary

Read-only HTTP/JSON adapter for DALgo: declarative collections, fail-closed pushdown, capabilities, snapshot fallback with provenance

## Problem

DataTug's core-investigation-loop demo project needs a public, keyless,
read-only reference-data source (founder ruling 2026-09-09: *"I want to have
in MVP: inGitDB, SQLite, HTTP endpoint. Regards HTTP - some public sources
like countries or currency rates."*) that answers through the same
`dal.Query` model — and, crucially, the same access-policy pushdown
guarantees — as `dalgo2sqlite` and `dalgo2ingitdb` already do. A hand-rolled,
consumer-private HTTP fetcher (founder decision, recorded in README.md's
Status line: *"a generic DALgo adapter for HTTP rather than a consumer-private
fetcher"*) would duplicate that pushdown logic per consumer and could not be
reasoned about by a shared policy layer the way a `dal.DB` can.

## Behavior

See README.md's "Design constraints" section (binding, not relaxable without
a recorded decision) for the full contract. In short: each HTTP source is a
declarative `Collection` descriptor (URL template, path/query parameter
mapping, JSON row path, key field, environment-sourced headers, timeout); a
`dal.Query` executes only when every `Where()` condition reduces to equality
on a declared parameter field, combined with `AND` — anything else fails
closed with `dal.ErrNotSupported` unless the collection opts into
`ClientSideFilter` (public data only); a `Capabilities` lookup lets a caller
ask what is pushable before executing; an optional recorded-snapshot store
answers when a live request fails for a transient reason, and every result
reports whether it came from `live` or `snapshot` (`Provenance`); writes and
mutating transactions return `dal.ErrNotSupported` unconditionally.

## Acceptance Criteria

### AC: fail-closed-pushdown

**Given** a `dal.Query` whose `Where()` condition cannot be fully expressed
as declared-parameter equalities
**When** `ExecuteQueryToRecordsReader` runs against a collection that does
NOT set `ClientSideFilter`
**Then** it returns an error wrapping `dal.ErrNotSupported` and the adapter
never makes an HTTP request (see `query_test.go`'s `TestExecuteQueryToRecordsReader_FailsClosed`, which uses an `http.RoundTripper` that fails the test if invoked at all).

### AC: snapshot-fallback-with-provenance

**Given** `Mode: ModeLiveThenSnapshot` and a recorded snapshot for the exact
request (collection + sorted parameters)
**When** the live endpoint fails for a transient reason (network error,
timeout, or a 5xx/429 response — never a 4xx, which is a caller/config error)
**Then** the read succeeds from the snapshot and the observed `Provenance`
reports `Source: SourceSnapshot` (see `snapshot_test.go`'s
`TestRecord_And_SnapshotFallback`).

### AC: get-requires-declared-key-field

**Given** a collection whose `KeyField` is NOT itself a declared `Param`
**When** `Get` or `Exists` is called
**Then** it fails closed with `dal.ErrNotSupported` rather than fetching a
broader listing and scanning it client-side (see `get_test.go`'s
`TestGet_KeyFieldNotDeclaredFailsClosed`, and `examples/frankfurter`, a real
endpoint demonstrating this).

### AC: http-source-in-demo (verifies core-investigation-loop#REQ:http-reference-source)

**Given** the two example descriptors under `examples/` (CountriesNow
currency-by-country, Frankfurter exchange rates — substituted for REST
Countries, whose public v3.1 API is fully deprecated; see
`examples/countries/README.md`)
**When** their offline tests run against the recorded `testdata/` fixtures
(`Mode: ModeSnapshot`, no network)
**Then** `Get`/`ExecuteQueryToRecordsReader` return rows shaped like any
other DALgo source, keyed and filtered exactly as the descriptor declares.

## Open Questions

None at this time — task-11 in `datatug/datatug`'s
`2026-09-09-phase-1-core-investigation-loop` plan is `datatug-cli`'s own
executor consuming this package; wiring that consumer is out of this
package's scope (see PR body for what stream S11 did and did not cover).

---
*This document follows the https://specscore.md/feature-specification*
