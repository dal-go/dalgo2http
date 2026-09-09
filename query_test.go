package dalgo2http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dal-go/dalgo/dal"
)

func countriesCollection(url string) Collection {
	return Collection{
		Name:        "countries",
		URLTemplate: url + "/countries/currency/q?country={name}",
		KeyField:    "name",
		RowsPath:    "data",
		Params:      map[string]Param{"name": {Location: ParamQuery}},
	}
}

func newQuery(collection string, cond dal.Condition, limit int) dal.StructuredQuery {
	var b dal.IQueryBuilder = dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef(collection, "")))
	if cond != nil {
		b = b.Where(cond)
	}
	if limit > 0 {
		b = b.Limit(limit)
	}
	return b.SelectIntoRecord(nil)
}

func TestExecuteQueryToRecordsReader_EqualityPushdown(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"error":false,"data":{"name":"France","currency":"EUR","iso2":"FR","iso3":"FRA"}}`))
	}))
	defer srv.Close()

	db, err := NewDB(Config{Collections: []Collection{countriesCollection(srv.URL)}, Client: srv.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	q := newQuery("countries", dal.WhereField("name", dal.Equal, "France"), 0)
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
	data, ok := rec.Data().(map[string]any)
	if !ok || data["currency"] != "EUR" {
		t.Fatalf("record data = %#v, want currency=EUR", rec.Data())
	}
	if gotQuery != "country=France" {
		t.Fatalf("upstream query = %q, want country=France", gotQuery)
	}
	if _, err := reader.Next(); err != dal.ErrNoMoreRecords {
		t.Fatalf("second Next() err = %v, want ErrNoMoreRecords", err)
	}
}

func TestExecuteQueryToRecordsReader_FailsClosed(t *testing.T) {
	client := noCallClient(t)
	coll := countriesCollection("https://example.invalid")
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: client, Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	ctx := context.Background()

	cases := []struct {
		name string
		q    dal.StructuredQuery
	}{
		{"undeclared field", newQuery("countries", dal.WhereField("region", dal.Equal, "Europe"), 0)},
		{"non-equality operator", newQuery("countries", dal.WhereField("name", dal.GreaterThen, "F"), 0)},
		{"OR group", newQuery("countries", dal.NewGroupCondition(dal.Or, dal.WhereField("name", dal.Equal, "France"), dal.WhereField("name", dal.Equal, "Spain")), 0)},
		{"order by", dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
			Where(dal.WhereField("name", dal.Equal, "France")).
			OrderBy(dal.AscendingField("name")).
			SelectIntoRecord(nil)},
		{"offset", dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
			Where(dal.WhereField("name", dal.Equal, "France")).
			Offset(1).
			SelectIntoRecord(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.ExecuteQueryToRecordsReader(ctx, tc.q); err == nil {
				t.Fatalf("ExecuteQueryToRecordsReader() = nil error, want dal.ErrNotSupported")
			} else if !isNotSupported(err) {
				t.Fatalf("ExecuteQueryToRecordsReader() = %v, want dal.ErrNotSupported", err)
			}
		})
	}
}

func TestExecuteQueryToRecordsReader_UnknownCollection(t *testing.T) {
	db, err := NewDB(Config{Collections: []Collection{countriesCollection("https://example.invalid")}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("no-such-collection", nil, 0)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), q); err == nil {
		t.Fatalf("expected error for unknown collection")
	}
}

func TestExecuteQueryToRecordsReader_ClientSideFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"countryCode":"FR","name":"France"},{"countryCode":"DE","name":"Germany"},{"countryCode":"ES","name":"Spain"}]`))
	}))
	defer srv.Close()

	// countries-all has no URL parameters at all (a "list everything" endpoint);
	// ClientSideFilter is what makes an equality condition on a field the
	// endpoint cannot be asked to filter by still answerable.
	coll := Collection{
		Name:             "countries-all",
		URLTemplate:      srv.URL + "/countries",
		KeyField:         "countryCode",
		ClientSideFilter: true,
	}

	db, err := NewDB(Config{Collections: []Collection{coll}, Client: srv.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("countries-all", dal.WhereField("countryCode", dal.Equal, "DE"), 0)
	reader, err := db.ExecuteQueryToRecordsReader(context.Background(), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("reader.Next: %v", err)
	}
	if rec.Key().ID != "DE" {
		t.Fatalf("record key ID = %v, want DE", rec.Key().ID)
	}
	if _, err := reader.Next(); err != dal.ErrNoMoreRecords {
		t.Fatalf("expected exactly one matching row, got a second: err=%v", err)
	}
}

func TestExecuteQueryToRecordsReader_ClientSideFilterFalseFailsClosed(t *testing.T) {
	coll := countriesCollection("https://example.invalid") // ClientSideFilter defaults to false
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("countries", dal.WhereField("region", dal.Equal, "Europe"), 0)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), q); !isNotSupported(err) {
		t.Fatalf("err = %v, want dal.ErrNotSupported", err)
	}
}

func TestExecuteQueryToRecordsReader_LimitAppliedAfterFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"countryCode":"FR"},{"countryCode":"DE"},{"countryCode":"ES"}]`))
	}))
	defer srv.Close()
	coll := Collection{Name: "all", URLTemplate: srv.URL + "/countries", KeyField: "countryCode"}
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: srv.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("all", nil, 2)
	records, err := dal.ReadAllToRecords(context.Background(), mustReader(t, db, q))
	if err != nil {
		t.Fatalf("ReadAllToRecords: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
}

func mustReader(t *testing.T, db dal.DB, q dal.StructuredQuery) dal.RecordsReader {
	t.Helper()
	reader, err := db.ExecuteQueryToRecordsReader(context.Background(), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	return reader
}

func isNotSupported(err error) bool {
	return errors.Is(err, dal.ErrNotSupported)
}
