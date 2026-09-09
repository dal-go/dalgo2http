package dalgo2http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dal-go/dalgo/dal"
)

// countriesCollection builds a test descriptor targeting url, which may be
// either a real https://... host (used with noCallClient, never dialed) or
// an httptest.Server's http://127.0.0.1:<port> URL (used with srv.Client()
// or the default client) — InsecureAllowLoopback is set unconditionally
// since it only relaxes anything when the scheme is actually http:// with a
// loopback host; it is a no-op for the https:// fake-host cases.
func countriesCollection(url string) Collection {
	return Collection{
		Name:                  "countries",
		URLTemplate:           url + "/countries/currency/q?country={name}",
		KeyField:              "name",
		RowsPath:              "data",
		Params:                map[string]Param{"name": {Location: ParamQuery}},
		InsecureAllowLoopback: true,
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

// TestExecuteQueryToRecordsReader_ProjectionDropsUnrequestedFields proves
// the Phase 1 HTTP bounds' "if a requested protected predicate/projection
// cannot be enforced safely, reject it ... " read the other way round for
// this schemaless-but-keyless-JSON adapter: a column projection over a bare
// field reference CAN be enforced, at the adapter boundary — after
// extractRows, before a row is ever converted into the returned record. The
// live response carries "iso2"/"iso3" too; the caller asked only for
// "currency", and the record's Data() must not carry the un-requested
// fields at all (not blanked, absent) — proving they never reach the
// reader, matching the same "not blanked, absent" precedent
// query_output.go-style redaction already uses elsewhere in this ecosystem.
// KeyField ("name") is retained even though it was not itself SELECTed,
// since it identifies the row rather than being a value under projection.
func TestExecuteQueryToRecordsReader_ProjectionDropsUnrequestedFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR","iso2":"FR","iso3":"FRA"}}`))
	}))
	defer srv.Close()

	coll := countriesCollection(srv.URL) // InsecureAllowLoopback: true
	db, err := NewDB(Config{Collections: []Collection{coll}, Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		Where(dal.WhereField("name", dal.Equal, "France")).
		SelectColumns(dal.Column{Expression: dal.Field("currency")})
	reader, err := db.ExecuteQueryToRecordsReader(context.Background(), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("reader.Next: %v", err)
	}
	data, ok := rec.Data().(map[string]any)
	if !ok {
		t.Fatalf("record data = %#v, want map[string]any", rec.Data())
	}
	if data["currency"] != "EUR" {
		t.Fatalf("data[currency] = %v, want EUR (the requested field must still be present)", data["currency"])
	}
	if data["name"] != "France" {
		t.Fatalf("data[name] = %v, want France (keyField is always retained, even unrequested)", data["name"])
	}
	if _, present := data["iso2"]; present {
		t.Fatalf("data has un-requested field iso2 = %v, want it absent (never reaching the reader)", data["iso2"])
	}
	if _, present := data["iso3"]; present {
		t.Fatalf("data has un-requested field iso3 = %v, want it absent (never reaching the reader)", data["iso3"])
	}
}

// TestExecuteQueryToRecordsReader_ProjectionMakesExactlyOneGET proves a
// projected query is still answered by exactly one live GET — projection is
// enforced entirely in memory, after the single fetch, not by issuing
// extra requests or falling back to a broader "fetch everything" path.
func TestExecuteQueryToRecordsReader_ProjectionMakesExactlyOneGET(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR","iso2":"FR"}}`))
	}))
	defer srv.Close()

	coll := countriesCollection(srv.URL)
	db, err := NewDB(Config{Collections: []Collection{coll}, Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		Where(dal.WhereField("name", dal.Equal, "France")).
		SelectColumns(dal.Column{Expression: dal.Field("currency")}, dal.Column{Expression: dal.Field("iso2")})
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), q); err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want exactly 1", requests)
	}
}

// TestExecuteQueryToRecordsReader_ProjectionAbsentFieldStaysAbsent proves a
// requested column the live response simply does not carry is left absent
// in the projected row — never synthesized as an explicit null value — so a
// caller can still distinguish "this field doesn't exist here" from "this
// field is present and explicitly null".
func TestExecuteQueryToRecordsReader_ProjectionAbsentFieldStaysAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR"}}`)) // no "population" field at all
	}))
	defer srv.Close()

	coll := countriesCollection(srv.URL)
	db, err := NewDB(Config{Collections: []Collection{coll}, Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		Where(dal.WhereField("name", dal.Equal, "France")).
		SelectColumns(dal.Column{Expression: dal.Field("population")})
	reader, err := db.ExecuteQueryToRecordsReader(context.Background(), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("reader.Next: %v", err)
	}
	data, ok := rec.Data().(map[string]any)
	if !ok {
		t.Fatalf("record data = %#v, want map[string]any", rec.Data())
	}
	if v, present := data["population"]; present {
		t.Fatalf("data[population] = %v (present), want the key entirely absent, not a null value", v)
	}
}

// TestExecuteQueryToRecordsReader_NonFieldColumnStillRefused proves the
// fail-closed rule survives for what genuinely cannot be enforced: a
// SelectColumns() entry whose Expression is not a bare field reference
// (something this adapter cannot evaluate against a fetched row) is refused
// before any GET is issued — noCallClient fails the test outright if the
// adapter ever makes an HTTP request.
func TestExecuteQueryToRecordsReader_NonFieldColumnStillRefused(t *testing.T) {
	coll := countriesCollection("https://example.invalid")
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		Where(dal.WhereField("name", dal.Equal, "France")).
		SelectColumns(dal.Column{Alias: "computed", Expression: dal.Constant{Value: 1}})
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), q); !isNotSupported(err) {
		t.Fatalf("ExecuteQueryToRecordsReader() with a non-field column = %v, want dal.ErrNotSupported (no request sent)", err)
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
		Name:                  "countries-all",
		URLTemplate:           srv.URL + "/countries",
		KeyField:              "countryCode",
		ClientSideFilter:      true,
		InsecureAllowLoopback: true,
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
	coll := Collection{Name: "all", URLTemplate: srv.URL + "/countries", KeyField: "countryCode", InsecureAllowLoopback: true}
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
