package countries_test

import (
	"context"
	"os"
	"testing"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo2http"
	"github.com/dal-go/dalgo2http/examples/countries"
	"github.com/dal-go/record"
)

// This example runs entirely offline, against the recorded fixture under
// testdata/ (see record_fixtures.go's invocation of dalgo2http.Record — the
// tool item 4 of the stream brief asks for), using ModeSnapshot so it never
// touches the network and cannot flake in CI.
func newSnapshotDB(t *testing.T) dal.DB {
	t.Helper()
	db, err := dalgo2http.NewDB(dalgo2http.Config{
		Collections: []dalgo2http.Collection{countries.Collection()},
		Snapshots:   os.DirFS("testdata"),
		Mode:        dalgo2http.ModeSnapshot,
	})
	if err != nil {
		t.Fatalf("dalgo2http.NewDB: %v", err)
	}
	return db
}

func TestCountries_Get(t *testing.T) {
	db := newSnapshotDB(t)
	rec := dalgo2http.NewRecorder()
	target := map[string]any{}
	key := record.NewKeyWithID("countries", "France")
	r := record.NewRecordWithData(key, &target)
	if err := db.Get(rec.WithContext(context.Background()), r); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if target["currency"] != "EUR" || target["iso3"] != "FRA" {
		t.Fatalf("target = %#v, want currency=EUR iso3=FRA", target)
	}
	prov, seen := rec.Last()
	if !seen || prov.Source != dalgo2http.SourceSnapshot {
		t.Fatalf("Provenance = %+v (seen=%v), want Source=snapshot", prov, seen)
	}
}

func TestCountries_Query(t *testing.T) {
	db := newSnapshotDB(t)
	q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		Where(dal.WhereField("name", dal.Equal, "France")).
		SelectIntoRecord(nil)
	reader, err := db.ExecuteQueryToRecordsReader(context.Background(), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("reader.Next: %v", err)
	}
	if rec.Key().ID != "France" {
		t.Fatalf("record key ID = %v, want France", rec.Key().ID)
	}
}
