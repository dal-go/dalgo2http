package dalgo2http_test

import (
	"testing"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo/dalgotest"
	"github.com/dal-go/dalgo2http"
)

// TestConformance runs DALgo's shared write-invariant suite against
// dalgo2http. dalgo2http is read-only (see transaction.go): every write
// method returns dal.ErrNotSupported unconditionally. The suite tolerates
// that (see dalgotest.Checks' doc comment), so it does not test "does a
// write succeed" here — nothing does — but it DOES prove something real: the
// framework's write pipeline runs validation on the record data BEFORE ever
// reaching this adapter's not-supported stubs, so an invalid record is
// rejected with the validation error, not a storage error. That ordering is
// exactly what a read-only adapter risks getting backwards if it skips the
// framework's NewDB wrapping.
func TestConformance(t *testing.T) {
	dalgotest.RunConformance(t, func(t *testing.T) (dal.DB, func()) {
		// KeyField is deliberately NOT a declared Param, so Get/Exists fail
		// closed with dal.ErrNotSupported for every key without ever calling
		// the network (see database.go) — the suite's persisted() helper
		// treats dal.ErrNotSupported as "cannot answer, skip the assertion",
		// which is exactly the read-only conformance dalgo2fs's own
		// TestConformance relies on. Declaring KeyField as a real path
		// parameter here would make Exists() actually dial "example.invalid",
		// making this test's outcome depend on this environment's DNS/network
		// policy — no test in this package should require network access.
		coll := dalgo2http.Collection{
			Name:        dalgotest.DefaultCollection,
			URLTemplate: "https://example.invalid/items",
			KeyField:    "id",
		}
		db, err := dalgo2http.NewDB(dalgo2http.Config{Collections: []dalgo2http.Collection{coll}})
		if err != nil {
			t.Fatalf("dalgo2http.NewDB: %v", err)
		}
		return db, nil
	})
}
