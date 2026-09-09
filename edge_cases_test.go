package dalgo2http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
)

func TestLoadConfig_MalformedSyntax(t *testing.T) {
	if _, err := LoadConfigYAML([]byte("not: valid: yaml: [")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("LoadConfigYAML(malformed) err = %v, want ErrInvalidConfig", err)
	}
	if _, err := LoadConfigJSON([]byte("{not valid json")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("LoadConfigJSON(malformed) err = %v, want ErrInvalidConfig", err)
	}
}

func TestStringifyParam(t *testing.T) {
	if v, err := stringifyParam("s"); err != nil || v != "s" {
		t.Fatalf("stringifyParam(string) = (%q, %v)", v, err)
	}
	if v, err := stringifyParam(42); err != nil || v != "42" {
		t.Fatalf("stringifyParam(int) = (%q, %v)", v, err)
	}
	ts := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	if v, err := stringifyParam(ts); err != nil || v != ts.Format(time.RFC3339) {
		t.Fatalf("stringifyParam(time.Time) = (%q, %v)", v, err)
	}
	if _, err := stringifyParam(nil); !isNotSupported(err) {
		t.Fatalf("stringifyParam(nil) err = %v, want dal.ErrNotSupported", err)
	}
}

func TestCollectEqualities_ConflictingConstraints(t *testing.T) {
	coll := countriesCollection("https://example.invalid")
	cond := dal.NewGroupCondition(dal.And,
		dal.WhereField("name", dal.Equal, "France"),
		dal.WhereField("name", dal.Equal, "Germany"),
	)
	if _, err := collectEqualities(cond, coll, map[string]string{}); !isNotSupported(err) {
		t.Fatalf("collectEqualities() err = %v, want dal.ErrNotSupported", err)
	}
}

func TestDoLiveFetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	coll := Collection{Name: "slow", URLTemplate: srv.URL, KeyField: "id", Timeout: time.Millisecond, InsecureAllowLoopback: true}
	_, _, err := doLiveFetch(context.Background(), srv.Client(), coll, srv.URL)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("doLiveFetch() err = %v, want ErrUpstream", err)
	}
}

func TestReadSnapshot_Corrupt(t *testing.T) {
	fsys := fstest.MapFS{"countries.json": &fstest.MapFile{Data: []byte("not json")}}
	if _, _, err := readSnapshot(fsys, "countries.json"); err == nil {
		t.Fatalf("readSnapshot(corrupt) = nil error, want error")
	}
}

func TestRecord_PropagatesBuildAndFetchErrors(t *testing.T) {
	// Missing required path parameter: buildURL fails before any request.
	coll := Collection{
		Name: "c", URLTemplate: "https://example.invalid/{id}", KeyField: "id",
		Params: map[string]Param{"id": {Location: ParamPath}},
	}
	if _, err := Record(context.Background(), noCallClient(t), coll, map[string]string{}, t.TempDir()); !errors.Is(err, ErrMissingParam) {
		t.Fatalf("Record() err = %v, want ErrMissingParam", err)
	}

	// Endpoint reachable but returns 500: doLiveFetch fails.
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()
	fxColl := Collection{Name: "fx", URLTemplate: down.URL, KeyField: "base", InsecureAllowLoopback: true}
	if _, err := Record(context.Background(), down.Client(), fxColl, map[string]string{}, t.TempDir()); !errors.Is(err, ErrUpstream) {
		t.Fatalf("Record() err = %v, want ErrUpstream", err)
	}
}

func TestExists_UnknownCollection(t *testing.T) {
	db, err := NewDB(Config{Collections: []Collection{countriesCollection("https://example.invalid")}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	if _, err := db.Exists(context.Background(), record.NewKeyWithID("no-such", "x")); !errors.Is(err, ErrUnknownCollection) {
		t.Fatalf("Exists() err = %v, want ErrUnknownCollection", err)
	}
}

func TestGetMulti_PropagatesNonNotFoundError(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()
	coll := countriesCollection(down.URL)
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: down.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	target := map[string]any{}
	rec := record.NewRecordWithData(record.NewKeyWithID("countries", "France"), &target)
	if err := db.GetMulti(context.Background(), []record.Record{rec}); !errors.Is(err, ErrUpstream) {
		t.Fatalf("GetMulti() err = %v, want ErrUpstream", err)
	}
}
