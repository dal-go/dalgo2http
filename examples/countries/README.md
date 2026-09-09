# examples/countries

A dalgo2http descriptor for [CountriesNow](https://countriesnow.space)'s
keyless currency-by-country lookup:
`https://countriesnow.space/api/v0.1/countries/currency/q?country={name}`.

This targets CountriesNow rather than restcountries.com, which the stream
brief originally named: restcountries.com's public v3.1 API is now fully
deprecated (every route, including `/v3.1/name/{name}` and `/v3.1/alpha/...`,
301s to a static payload reporting the deprecation — verified live while
building this package). Swapping the concrete public endpoint is within
scope: the plan this stream implements says so explicitly ("the concrete
public endpoints... are the lead's choice and swap freely").

`KeyField` is `"name"` — the same field the `name` equality parameter
targets — so both `dal.DB.Get`/`Exists` and `ExecuteQueryToRecordsReader`
work for this collection. Contrast `examples/frankfurter`, where `KeyField`
is deliberately NOT a declared parameter.

`testdata/` holds one fixture, recorded from the live endpoint with
`dalgo2http.Record` (query: `country=France`). `example_test.go` runs fully
offline against it (`Mode: dalgo2http.ModeSnapshot`), so it never depends on
CountriesNow being reachable from CI.
