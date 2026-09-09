// Package frankfurter is a dalgo2http descriptor for the Frankfurter
// exchange-rate API, the second of the two example sources DataTug's demo
// knowledge project uses to satisfy REQ:http-reference-source ("currency
// rates", founder ruling 2026-09-09).
//
// The brief this stream implements named api.frankfurter.app; that host now
// 301-redirects to api.frankfurter.dev (verified live while building this
// package), and Go's http.Client follows the redirect transparently, so
// either host works — this descriptor targets api.frankfurter.dev directly
// to avoid a needless redirect hop on every request.
package frankfurter

import (
	"time"

	"github.com/dal-go/dalgo2http"
)

// Collection is the dalgo2http descriptor for
// https://api.frankfurter.dev/v1/latest?base=USD&symbols={to}.
//
// KeyField is "base" and is DELIBERATELY NOT a declared Param — "base=USD"
// is fixed in the URL template, not something a query can ask for by
// equality — so dal.DB.Get/Exists fail closed with dal.ErrNotSupported for
// this collection (see Capabilities.SupportsGet in the root package); only
// ExecuteQueryToRecordsReader, pushing an equality condition on "to", is
// supported. RowsPath is empty because the whole response body — a JSON
// object, not an array — IS the one row.
func Collection() dalgo2http.Collection {
	return dalgo2http.Collection{
		Name:        "exchange-rates",
		URLTemplate: "https://api.frankfurter.dev/v1/latest?base=USD&symbols={to}",
		KeyField:    "base",
		Params: map[string]dalgo2http.Param{
			"to": {Location: dalgo2http.ParamQuery},
		},
		Timeout: 10 * time.Second,
	}
}
