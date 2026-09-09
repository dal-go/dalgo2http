package dalgo2http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dal-go/record"
)

func TestGet(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR"}}`))
	}))
	defer srv.Close()

	db, err := NewDB(Config{Collections: []Collection{countriesCollection(srv.URL)}, Client: srv.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	key := record.NewKeyWithID("countries", "France")
	target := map[string]any{}
	rec := record.NewRecordWithData(key, &target)
	if err := db.Get(context.Background(), rec); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if target["currency"] != "EUR" {
		t.Fatalf("target = %#v, want currency=EUR", target)
	}
	if gotQuery != "country=France" {
		t.Fatalf("upstream query = %q, want country=France", gotQuery)
	}
}

func TestGet_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"Elsewhere","currency":"XXX"}}`))
	}))
	defer srv.Close()
	db, err := NewDB(Config{Collections: []Collection{countriesCollection(srv.URL)}, Client: srv.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	key := record.NewKeyWithID("countries", "France")
	target := map[string]any{}
	rec := record.NewRecordWithData(key, &target)
	err = db.Get(context.Background(), rec)
	if !record.IsNotFound(err) {
		t.Fatalf("Get() err = %v, want record.IsNotFound", err)
	}
	// record.Record.Error() returns nil for a not-found record by design
	// (it is a normal outcome, not a storage error); Exists() is how a
	// caller who already called Get distinguishes the two.
	if rec.Exists() {
		t.Fatalf("rec.Exists() = true, want false after a not-found Get")
	}
}

func TestGet_KeyFieldNotDeclaredFailsClosed(t *testing.T) {
	coll := Collection{
		Name:        "fx",
		URLTemplate: "https://example.invalid/latest?base=USD&symbols={to}",
		KeyField:    "base", // "base" is fixed by the template, never a declared Param
		Params:      map[string]Param{"to": {Location: ParamQuery}},
	}
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	key := record.NewKeyWithID("fx", "USD")
	target := map[string]any{}
	rec := record.NewRecordWithData(key, &target)
	err = db.Get(context.Background(), rec)
	if !isNotSupported(err) {
		t.Fatalf("Get() err = %v, want dal.ErrNotSupported", err)
	}
	if !isNotSupported(rec.Error()) {
		t.Fatalf("rec.Error() = %v, want dal.ErrNotSupported recorded on rec too", rec.Error())
	}
}

func TestGet_UnknownCollection(t *testing.T) {
	db, err := NewDB(Config{Collections: []Collection{countriesCollection("https://example.invalid")}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	key := record.NewKeyWithID("no-such-collection", "x")
	target := map[string]any{}
	rec := record.NewRecordWithData(key, &target)
	if err := db.Get(context.Background(), rec); !errors.Is(err, ErrUnknownCollection) {
		t.Fatalf("Get() err = %v, want ErrUnknownCollection", err)
	}
}

func TestExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR"}}`))
	}))
	defer srv.Close()
	db, err := NewDB(Config{Collections: []Collection{countriesCollection(srv.URL)}, Client: srv.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	exists, err := db.Exists(context.Background(), record.NewKeyWithID("countries", "France"))
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatalf("Exists() = false, want true")
	}
}

func TestGetMulti(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("country")
		_, _ = w.Write([]byte(`{"data":{"name":"` + name + `","currency":"EUR"}}`))
	}))
	defer srv.Close()
	db, err := NewDB(Config{Collections: []Collection{countriesCollection(srv.URL)}, Client: srv.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	targets := []map[string]any{{}, {}}
	records := []record.Record{
		record.NewRecordWithData(record.NewKeyWithID("countries", "France"), &targets[0]),
		record.NewRecordWithData(record.NewKeyWithID("countries", "Germany"), &targets[1]),
	}
	if err := db.GetMulti(context.Background(), records); err != nil {
		t.Fatalf("GetMulti: %v", err)
	}
	if targets[0]["name"] != "France" || targets[1]["name"] != "Germany" {
		t.Fatalf("targets = %#v", targets)
	}
}
