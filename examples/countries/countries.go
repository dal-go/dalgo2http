// Package countries is a dalgo2http descriptor for CountriesNow's keyless
// currency-by-country lookup, one of the two example sources DataTug's demo
// knowledge project uses to satisfy REQ:http-reference-source (public,
// keyless, read-only reference data — "some public sources like countries or
// currency rates", founder ruling 2026-09-09).
//
// The upstream API this originally targeted, restcountries.com's v3.1, is
// fully deprecated (every route now 301s to a static "deprecated, see v5"
// error payload — verified live while building this package); CountriesNow
// is the substitute, which the plan this stream implements explicitly allows
// ("the concrete public endpoints... are the lead's choice and swap
// freely").
package countries

import (
	"time"

	"github.com/dal-go/dalgo2http"
)

// Collection is the dalgo2http descriptor for
// https://countriesnow.space/api/v0.1/countries/currency/q?country={name}.
//
// KeyField is "name" (not e.g. an ISO code) DELIBERATELY: it is the same
// field the "name" Param equality condition targets, so both
// dal.DB.Get/Exists AND ExecuteQueryToRecordsReader work for this
// collection — see Capabilities.SupportsGet in the root package, which is
// false whenever KeyField is not also a declared Param.
func Collection() dalgo2http.Collection {
	return dalgo2http.Collection{
		Name:        "countries",
		URLTemplate: "https://countriesnow.space/api/v0.1/countries/currency/q?country={name}",
		KeyField:    "name",
		RowsPath:    "data",
		Params: map[string]dalgo2http.Param{
			"name": {Location: dalgo2http.ParamQuery},
		},
		Timeout: 10 * time.Second,
	}
}
