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
`ClientSideFilter` (public data only); a non-empty `SelectColumns()`
projection is always refused, since this adapter has no schema to enforce it
against; a `Capabilities` lookup lets a caller ask what is pushable before
executing; an optional recorded-snapshot store answers when a live request
fails for a transient reason, and every result reports whether it came from
`live` or `snapshot` (`Provenance`); writes and mutating transactions return
`dal.ErrNotSupported` unconditionally.

The Phase 1 HTTP bounds (`datatug/datatug`'s
`spec/features/core-investigation-loop/api-contract.md`, "Bounded lookups and
HTTP") additionally require: `https://`-only descriptors; a guarded dialer
denying private/loopback/link-local/metadata-service addresses including DNS
rebinding; no redirects; a 2 MiB response bound; provider capability checks
before dispatch; and no secrets in query strings. See the ACs below.

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

### AC: https-only-descriptors

**Given** a `Collection` whose `URLTemplate` uses `http://` (or any scheme
other than `https://`)
**When** it is validated (`LoadConfigYAML`/`LoadConfigJSON`/`NewDB`)
**Then** it is refused with `ErrInvalidConfig`, UNLESS `InsecureAllowLoopback`
is set AND the host is literally loopback (127.0.0.1, ::1, localhost) — a
test-only escape hatch not reachable from YAML/JSON config at all (see
`config_test.go`'s `TestValidateConfig` cases `"plain http rejected by
default"`, `"unsupported scheme rejected"`, `"InsecureAllowLoopback with a
non-loopback host is still rejected"` and `"InsecureAllowLoopback with a
loopback host is accepted"`).

### AC: address-guard-denies-private-and-metadata

**Given** the default client (`Config.Client` left nil)
**When** it dials any request, resolving the target host at most once
**Then** it refuses every private (RFC1918 + IPv6 ULA), loopback, link-local
(including the `169.254.169.254` cloud metadata address), multicast or
unspecified candidate address, dialing only a validated IP literal — never
re-resolving the hostname at dial time, so a later DNS rebind cannot redirect
the connection (see `security_test.go`'s `TestIsBlockedIP` for the exhaustive
address-class table, and
`TestGuardedDialContext_ResolvesOnceAndDialsOnlyTheValidatedIP` for the
single-resolution proof). `Collection.InsecureAllowLoopback` relaxes ONLY the
loopback check, for httptest use (see `TestNewDefaultClient_RealLoopbackRequestSucceeds`);
every other class, including the metadata address, stays blocked even then
(see `TestNewDefaultClient_MetadataAddressRejected`).

### AC: no-redirects

**Given** a live endpoint that responds with a redirect
**When** the default client (or any client with `CheckRedirect: denyRedirect`)
requests it
**Then** the redirect is not followed; the result wraps
`ErrRedirectNotAllowed` and `ErrUpstreamClient` (never `ErrUpstream`, so
`ModeLiveThenSnapshot` must not treat it as fallback-eligible even when a
matching snapshot exists) — see `redirect_test.go`'s
`TestDoLiveFetch_RedirectRefused`, `TestNewDefaultClient_RedirectRefused` and
`TestFetchRows_RedirectDoesNotTriggerSnapshotFallback`.

### AC: bounded-response-size

**Given** a live response body over 2 MiB
**When** it is read
**Then** the request fails explicitly with `ErrResponseTooLarge` rather than
being silently truncated by a `LimitReader` and then failing JSON decoding
downstream with a misleading error; a body of exactly 2 MiB still succeeds
(see `request_test.go`'s `TestDoLiveFetch_ResponseTooLarge` and
`TestDoLiveFetch_ResponseAtLimitSucceeds`). Row-shape validation errors
(`extractRows`) name the `rowsPath` and the specific failing segment/row
index (see `jsonpath_test.go`'s `TestExtractRows`, `wantErrContains`).

### AC: projection-refused-before-dispatch

**Given** a `dal.StructuredQuery` with a non-empty `SelectColumns()`
**When** `ExecuteQueryToRecordsReader` runs
**Then** it is refused with `dal.ErrNotSupported` before any HTTP request is
made — this adapter has no schema and cannot guarantee an un-requested field
is actually absent from the response, so it refuses rather than silently
returning full rows that would look like the projection was honoured (see
`query_test.go`'s `TestExecuteQueryToRecordsReader_ColumnProjectionRefused`,
which uses `noCallClient` to fail the test outright if a request is ever
sent).

### AC: no-secrets-in-query-string

**Given** a `Collection` whose declared query-location `Param` name, or whose
`URLTemplate`'s own literal query-string key, looks like a credential
(`token`, `apikey`, `api_key`, `secret`, `password`, `authorization`,
case-insensitive substring match)
**When** it is validated
**Then** it is refused with `ErrInvalidConfig`; a header with the same name
is NOT refused, since `Headers` (an environment variable, never a literal) is
the documented, correct place for a credential (see `config_test.go`'s
`TestValidateConfig` cases `"declared query param named apikey is
rejected"`, `"literal query string key named token is rejected"`, `"literal
query string key containing api_key is rejected"` and `"header named
Authorization is NOT rejected"`).

## Open Questions

None at this time — task-11 in `datatug/datatug`'s
`2026-09-09-phase-1-core-investigation-loop` plan is `datatug-cli`'s own
executor consuming this package; wiring that consumer is out of this
package's scope (see PR body for what stream S11 did and did not cover).

---
*This document follows the https://specscore.md/feature-specification*
