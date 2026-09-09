# examples/frankfurter

A dalgo2http descriptor for the [Frankfurter](https://frankfurter.dev)
exchange-rate API: `https://api.frankfurter.dev/v1/latest?base=USD&symbols={to}`.

The stream brief this implements named `api.frankfurter.app`; that host now
301-redirects to `api.frankfurter.dev` (verified live while building this
package). Go's `http.Client` follows the redirect transparently, so either
host works — this descriptor targets `api.frankfurter.dev` directly to skip
the redirect hop on every request.

`KeyField` is `"base"` and is deliberately NOT a declared parameter: `base=USD`
is fixed in the URL template, not something the endpoint can be asked for by
equality. So `dal.DB.Get`/`Exists` fail closed with `dal.ErrNotSupported` for
this collection (see `Capabilities.SupportsGet` in the root package); only
`ExecuteQueryToRecordsReader`, pushing an equality condition on `to`, is
supported. `RowsPath` is empty because the whole response body — a JSON
object, not an array — IS the one row.

`testdata/` holds one fixture, recorded from the live endpoint with
`dalgo2http.Record` (query: `to=EUR`). `example_test.go` runs fully offline
against it (`Mode: dalgo2http.ModeSnapshot`).
