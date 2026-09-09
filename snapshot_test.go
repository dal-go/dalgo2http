package dalgo2http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"testing/fstest"

	"github.com/dal-go/dalgo/dal"
)

func TestRecord_And_SnapshotFallback(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR"}}`))
	}))
	coll := countriesCollection(up.URL)

	dir := t.TempDir()
	path, err := Record(context.Background(), up.Client(), coll, map[string]string{"name": "France"}, dir)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recorded snapshot file missing: %v", err)
	}
	up.Close() // the recorded live endpoint is now gone.

	// down is a live endpoint that always fails: fetchRows must fall back to
	// the snapshot Record just wrote.
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()

	fallbackColl := countriesCollection(down.URL)
	rec := NewRecorder()
	db, err := NewDB(Config{
		Collections: []Collection{fallbackColl},
		Client:      down.Client(),
		Snapshots:   os.DirFS(dir),
		Mode:        ModeLiveThenSnapshot,
	})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("countries", dal.WhereField("name", dal.Equal, "France"), 0)
	reader, err := db.ExecuteQueryToRecordsReader(rec.WithContext(context.Background()), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	record, err := reader.Next()
	if err != nil {
		t.Fatalf("reader.Next: %v", err)
	}
	if record.Key().ID != "France" {
		t.Fatalf("record key ID = %v, want France", record.Key().ID)
	}
	prov, seen := rec.Last()
	if !seen {
		t.Fatalf("Recorder observed no Provenance")
	}
	if prov.Source != SourceSnapshot {
		t.Fatalf("Provenance.Source = %q, want %q", prov.Source, SourceSnapshot)
	}
}

func TestFetchRows_ModeLive_NoFallbackOn5xx(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	coll := countriesCollection(down.URL)
	// A snapshot exists, but ModeLive must never consult it.
	snapshots := fstest.MapFS{
		SnapshotKey("countries", map[string]string{"name": "France"}): &fstest.MapFile{
			Data: []byte(`{"fetchedAt":"2020-01-01T00:00:00Z","statusCode":200,"body":{"data":{"name":"France","currency":"EUR"}}}`),
		},
	}
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: down.Client(), Snapshots: snapshots, Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("countries", dal.WhereField("name", dal.Equal, "France"), 0)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), q); !errors.Is(err, ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream (ModeLive must not fall back)", err)
	}
}

func TestFetchRows_ModeLive_NoFallbackOn4xx(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer down.Close()
	coll := countriesCollection(down.URL)
	snapshots := fstest.MapFS{
		SnapshotKey("countries", map[string]string{"name": "France"}): &fstest.MapFile{
			Data: []byte(`{"fetchedAt":"2020-01-01T00:00:00Z","statusCode":200,"body":{"data":{"name":"France","currency":"EUR"}}}`),
		},
	}
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: down.Client(), Snapshots: snapshots, Mode: ModeLiveThenSnapshot})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("countries", dal.WhereField("name", dal.Equal, "France"), 0)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), q); !errors.Is(err, ErrUpstreamClient) {
		t.Fatalf("err = %v, want ErrUpstreamClient (a 4xx must never fall back to a snapshot)", err)
	}
}

func TestFetchRows_ModeSnapshot_NeverCallsNetwork(t *testing.T) {
	coll := countriesCollection("https://example.invalid")
	snapshots := fstest.MapFS{
		SnapshotKey("countries", map[string]string{"name": "France"}): &fstest.MapFile{
			Data: []byte(`{"fetchedAt":"2020-01-01T00:00:00Z","statusCode":200,"body":{"data":{"name":"France","currency":"EUR"}}}`),
		},
	}
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t), Snapshots: snapshots, Mode: ModeSnapshot})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("countries", dal.WhereField("name", dal.Equal, "France"), 0)
	reader, err := db.ExecuteQueryToRecordsReader(context.Background(), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader: %v", err)
	}
	if _, err := reader.Next(); err != nil {
		t.Fatalf("reader.Next: %v", err)
	}
}

func TestFetchRows_ModeSnapshot_Miss(t *testing.T) {
	coll := countriesCollection("https://example.invalid")
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t), Mode: ModeSnapshot})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("countries", dal.WhereField("name", dal.Equal, "France"), 0)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), q); !errors.Is(err, ErrSnapshotMiss) {
		t.Fatalf("err = %v, want ErrSnapshotMiss", err)
	}
}
