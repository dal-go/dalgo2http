package frankfurter_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo2http"
	"github.com/dal-go/dalgo2http/examples/frankfurter"
	"github.com/dal-go/record"
)

// This example runs entirely offline against the recorded fixture under
// testdata/, using ModeSnapshot so it never touches the network.
func newSnapshotDB(t *testing.T) dal.DB {
	t.Helper()
	db, err := dalgo2http.NewDB(dalgo2http.Config{
		Collections: []dalgo2http.Collection{frankfurter.Collection()},
		Snapshots:   os.DirFS("testdata"),
		Mode:        dalgo2http.ModeSnapshot,
	})
	if err != nil {
		t.Fatalf("dalgo2http.NewDB: %v", err)
	}
	return db
}

func TestFrankfurter_Query(t *testing.T) {
	db := newSnapshotDB(t)
	q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("exchange-rates", ""))).
		Where(dal.WhereField("to", dal.Equal, "EUR")).
		SelectIntoRecord(nil)
	reader, err := db.ExecuteQueryToRecordsReader(context.Background(), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("reader.Next: %v", err)
	}
	data, ok := rec.Data().(map[string]any)
	if !ok || data["base"] != "USD" {
		t.Fatalf("record data = %#v, want base=USD", rec.Data())
	}
}

// Get is unsupported for this collection because "base" (KeyField) is never
// a declared Param — it is fixed in the URL template, not something the
// endpoint can be asked for by equality. This is exactly the fail-closed
// behaviour Capabilities.SupportsGet advertises.
func TestFrankfurter_GetIsUnsupported(t *testing.T) {
	db := newSnapshotDB(t)
	target := map[string]any{}
	r := record.NewRecordWithData(record.NewKeyWithID("exchange-rates", "USD"), &target)
	if err := db.Get(context.Background(), r); !errors.Is(err, dal.ErrNotSupported) {
		t.Fatalf("Get() err = %v, want dal.ErrNotSupported", err)
	}
}
