// Package frankfurter is a dalgo2http descriptor for the Frankfurter
// exchange-rate API, the second of the two example sources DataTug's demo
// knowledge project uses to satisfy REQ:http-reference-source ("currency
// rates", founder ruling 2026-09-09).
//
// The brief this stream implements named api.frankfurter.app; that host now
// 301-redirects to api.frankfurter.dev (verified live while building this
// package). This descriptor targets api.frankfurter.dev directly, which now
// matters for more than avoiding a needless hop: the Phase 1 HTTP bounds
// disable redirects by default (see the root package's README.md "Design
// constraints" and security.go's denyRedirect), so a descriptor pointed at
// api.frankfurter.app would now fail outright with ErrRedirectNotAllowed
// instead of following the redirect. See example_test.go's
// TestFrankfurter_NoRedirectNeeded for a fake-server proof this descriptor
// resolves in one hop.
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
