package dalgo2http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestCapabilities_ClientSideFilter(t *testing.T) {
	coll := countriesCollection("https://example.invalid")
	coll.ClientSideFilter = true
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	provider, ok := dal.As[CapabilitiesProvider](db)
	if !ok {
		t.Fatal("dal.As[CapabilitiesProvider] failed")
	}
	caps, err := provider.Capabilities("countries")
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if !caps.ClientSideFilter {
		t.Fatal("expected ClientSideFilter true")
	}
}

func TestConfigValidation_EdgeCases(t *testing.T) {
	// validateConfig via LoadConfigJSON (line 123)
	if _, err := LoadConfigJSON([]byte(`{"collections":[{"name":""}]}`)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}

	// URL template parse error (line 198)
	badColl := Collection{Name: "c", KeyField: "id", URLTemplate: "http://[::1]:namedport"}
	if err := badColl.validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for bad urlTemplate, got %v", err)
	}

	// isLoopbackHost localhost (line 232)
	if !isLoopbackHost("localhost") {
		t.Fatal("expected localhost to be loopback")
	}
}

func TestGet_MapToDataError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":false,"data":{"name":"France","currency":"EUR"}}`))
	}))
	defer srv.Close()
	coll := countriesCollection(srv.URL)
	db, _ := NewDB(Config{Collections: []Collection{coll}, Client: srv.Client(), Mode: ModeLive})
	var badTarget int
	rec := record.NewRecordWithData(record.NewKeyWithID("countries", "France"), &badTarget)
	if err := db.Get(context.Background(), rec); err == nil {
		t.Fatal("expected error from MapToData in Get")
	}
}

func TestExists_FetchRowsErrorAndNotFound(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()
	coll := countriesCollection(down.URL)
	db, _ := NewDB(Config{Collections: []Collection{coll}, Client: down.Client(), Mode: ModeLive})
	if _, err := db.Exists(context.Background(), record.NewKeyWithID("countries", "France")); err == nil {
		t.Fatal("expected error on server 500 in Exists")
	}

	// Exists not found returns (false, nil)
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":false,"data":{"name":"Germany"}}`))
	}))
	defer okSrv.Close()
	okColl := countriesCollection(okSrv.URL)
	okDB, _ := NewDB(Config{Collections: []Collection{okColl}, Client: okSrv.Client(), Mode: ModeLive})
	exists, err := okDB.Exists(context.Background(), record.NewKeyWithID("countries", "France"))
	if err != nil || exists {
		t.Fatalf("expected (false, nil), got (%v, %v)", exists, err)
	}
}

func TestFetchRows_BuildURLError(t *testing.T) {
	coll := Collection{Name: "c", URLTemplate: "https://example.invalid/{id}", KeyField: "id", Params: map[string]Param{"id": {Location: ParamPath}}}
	d := &database{cfg: Config{Collections: []Collection{coll}, Client: noCallClient(t), Mode: ModeLive}, collections: map[string]Collection{coll.Name: coll}}
	if _, _, err := d.fetchRows(context.Background(), coll, map[string]string{}); err == nil {
		t.Fatal("expected error on missing param in fetchRows")
	}
}

func TestExtractRows_Null(t *testing.T) {
	rows, err := extractRows([]byte("null"), "")
	if err != nil || rows != nil {
		t.Fatalf("expected nil, nil; got %v, %v", rows, err)
	}
}

type dummyQuery struct{}

func (dummyQuery) String() string                                                                 { return "dummy" }
func (dummyQuery) Offset() int                                                                    { return 0 }
func (dummyQuery) Limit() int                                                                     { return 0 }
func (dummyQuery) GetRecordsReader(context.Context, dal.QueryExecutor) (dal.RecordsReader, error)   { return nil, nil }
func (dummyQuery) GetRecordsetReader(context.Context, dal.QueryExecutor) (dal.RecordsetReader, error) { return nil, nil }

func TestExecuteQueryToRecordsReader_EdgeCases(t *testing.T) {
	coll := countriesCollection("https://example.invalid")
	db, _ := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t)})

	// Query is not StructuredQuery
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), dummyQuery{}); !isNotSupported(err) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}

	// Joins not supported
	joinedSource := dal.NewJoinedSource(dal.NewRootCollectionRef("other", ""), dal.JoinInner)
	qJoins := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", "")).Join(joinedSource)).
		SelectIntoRecord(nil)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), qJoins); !isNotSupported(err) {
		t.Fatalf("expected ErrNotSupported for joins, got %v", err)
	}

	// GroupBy / Having not supported
	qGroup := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		GroupBy(dal.Field("name")).
		SelectIntoRecord(nil)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), qGroup); !isNotSupported(err) {
		t.Fatalf("expected ErrNotSupported for group by, got %v", err)
	}

	// Cursors not supported
	qCursor := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		StartFrom(dal.Cursor("cursor1")).
		SelectIntoRecord(nil)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), qCursor); !isNotSupported(err) {
		t.Fatalf("expected ErrNotSupported for cursor, got %v", err)
	}

	// Where with nil parameter value
	qNilParam := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		Where(dal.WhereField("name", dal.Equal, nil)).
		SelectIntoRecord(nil)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), qNilParam); !isNotSupported(err) {
		t.Fatalf("expected ErrNotSupported for nil param, got %v", err)
	}
}

func TestCollectEqualities_MoreEdges(t *testing.T) {
	coll := countriesCollection("https://example.invalid")
	// Group with AND where sub-condition is residual
	condAnd := dal.NewGroupCondition(dal.And,
		dal.WhereField("name", dal.Equal, "France"),
		dal.WhereField("name", dal.GreaterThen, "A"),
	)
	residual, err := collectEqualities(condAnd, coll, map[string]string{})
	if err != nil || !residual {
		t.Fatalf("expected residual true, err nil; got %v, %v", residual, err)
	}

	// Comparison with non-constant right side
	condNonConst := dal.Comparison{Left: dal.Field("name"), Operator: dal.Equal, Right: dal.Field("other")}
	residual, err = collectEqualities(condNonConst, coll, map[string]string{})
	if err != nil || !residual {
		t.Fatalf("expected residual true, err nil; got %v, %v", residual, err)
	}

	// collectEqualities default
	residual, err = collectEqualities(dummyCondition{}, coll, map[string]string{})
	if err != nil || !residual {
		t.Fatalf("expected residual true, err nil; got %v, %v", residual, err)
	}

	// stringifyParam fmt.Stringer
	s, err := stringifyParam(time.January)
	if err != nil || s != "January" {
		t.Fatalf("expected January, nil; got %q, %v", s, err)
	}
}

func TestRowsToRecords_Edges(t *testing.T) {
	coll := countriesCollection("https://example.invalid")

	// Missing key field
	rowsMissingKey := []map[string]any{{"other": "value"}}
	if _, err := rowsToRecords(coll, rowsMissingKey, func() record.Record { return nil }); err == nil {
		t.Fatal("expected error on missing key field")
	}

	// With IntoRecord returning typed record
	type CountryData struct {
		Name     string `json:"name"`
		Currency string `json:"currency"`
	}
	validRows := []map[string]any{{"name": "France", "currency": "EUR"}}
	dummyKey := record.NewKeyWithID("countries", "dummy")
	recs, err := rowsToRecords(coll, validRows, func() record.Record {
		return record.NewRecordWithData(dummyKey, &CountryData{})
	})
	if err != nil || len(recs) != 1 {
		t.Fatalf("rowsToRecords valid: err %v, len %d", err, len(recs))
	}

	// With IntoRecord returning bad data causing MapToData to error
	var badTarget int
	if _, err := rowsToRecords(coll, validRows, func() record.Record {
		return record.NewRecordWithData(dummyKey, &badTarget)
	}); err == nil {
		t.Fatal("expected error on MapToData failure in rowsToRecords")
	}
}

func TestExecuteQuery_ResidualFilterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":false,"data":[{"name":"France","val":true}]}`))
	}))
	defer srv.Close()
	coll := Collection{Name: "countries", URLTemplate: srv.URL, KeyField: "name", RowsPath: "data", InsecureAllowLoopback: true, ClientSideFilter: true}
	db, _ := NewDB(Config{Collections: []Collection{coll}, Client: srv.Client(), Mode: ModeLive})

	// Incomparable condition in Where triggers error in filterRows
	q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		Where(dal.Comparison{Left: dal.Field("val"), Operator: dal.GreaterThen, Right: dal.Constant{Value: 123}}).
		SelectIntoRecord(nil)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), q); err == nil {
		t.Fatal("expected error on incomparable condition in ExecuteQueryToRecordsReader")
	}

	// Server returning rows missing key field causes rowsToRecords error
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":false,"data":[{"bad":"France"}]}`))
	}))
	defer badSrv.Close()
	badColl := Collection{Name: "countries", URLTemplate: badSrv.URL, KeyField: "name", InsecureAllowLoopback: true}
	badDB, _ := NewDB(Config{Collections: []Collection{badColl}, Client: badSrv.Client(), Mode: ModeLive})
	qOk := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).SelectIntoRecord(nil)
	if _, err := badDB.ExecuteQueryToRecordsReader(context.Background(), qOk); err == nil {
		t.Fatal("expected error on missing key field in ExecuteQueryToRecordsReader")
	}
}

func TestRequest_Edges(t *testing.T) {
	// buildURL undeclared param in template
	coll := Collection{Name: "c", URLTemplate: "https://example.invalid/{undeclared}", Params: map[string]Param{}}
	if _, err := buildURL(coll, map[string]string{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}

	// buildURL parse error on rendered URL
	badColl := Collection{Name: "c", URLTemplate: "http://[::1]:namedport"}
	if _, err := buildURL(badColl, map[string]string{}); err == nil {
		t.Fatal("expected error parsing rendered URL")
	}

	// doLiveFetch request build error
	if _, _, err := doLiveFetch(context.Background(), nil, badColl, "http://[::1]:namedport"); err == nil {
		t.Fatal("expected error building request in doLiveFetch")
	}

	// doLiveFetch with headers from env and nil client
	t.Setenv("TEST_HTTP_HEADER_ENV", "my-header-val")
	var receivedHeader string
	headerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Test-Header")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer headerSrv.Close()
	hColl := Collection{
		Name:                  "h",
		URLTemplate:           headerSrv.URL,
		Headers:               map[string]string{"X-Test-Header": "TEST_HTTP_HEADER_ENV"},
		InsecureAllowLoopback: true,
	}
	body, _, err := doLiveFetch(context.Background(), nil, hColl, headerSrv.URL)
	if err != nil {
		t.Fatalf("doLiveFetch nil client: %v", err)
	}
	if string(body) != "{}" || receivedHeader != "my-header-val" {
		t.Fatalf("unexpected header %q or body %q", receivedHeader, string(body))
	}

	// doLiveFetch io.ReadAll error on response body
	closeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer closeSrv.Close()
	closeColl := Collection{Name: "c", URLTemplate: closeSrv.URL, InsecureAllowLoopback: true}
	if _, _, err := doLiveFetch(context.Background(), closeSrv.Client(), closeColl, closeSrv.URL); err == nil {
		t.Fatal("expected error on closed connection body read")
	}
}

func TestSecurity_Edges(t *testing.T) {
	// lookupIPAddr default
	if addrs, err := lookupIPAddr(context.Background(), "localhost"); err != nil || len(addrs) == 0 {
		t.Fatalf("lookupIPAddr(localhost) = (%v, %v)", addrs, err)
	}

	// guardedDialContext split host port error
	if _, err := guardedDialContext(context.Background(), "tcp", "invalid-no-port"); !errors.Is(err, ErrAddressBlocked) {
		t.Fatalf("expected ErrAddressBlocked, got %v", err)
	}

	// guardedDialContext lookup error
	origLookup := lookupIPAddr
	defer func() { lookupIPAddr = origLookup }()
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return nil, errors.New("simulated DNS failure")
	}
	if _, err := guardedDialContext(context.Background(), "tcp", "some-unresolved-host:80"); err == nil {
		t.Fatal("expected error on DNS failure")
	}
}

func TestSnapshot_Edges(t *testing.T) {
	// readSnapshot missing key
	if _, _, err := readSnapshot(fstest.MapFS{}, "non-existent.json"); !errors.Is(err, ErrSnapshotMiss) {
		t.Fatalf("expected ErrSnapshotMiss, got %v", err)
	}

	// Record with invalid response body JSON bytes
	badJsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{0xff})
	}))
	defer badJsonSrv.Close()
	coll := Collection{Name: "c", URLTemplate: badJsonSrv.URL, InsecureAllowLoopback: true}
	if _, err := Record(context.Background(), badJsonSrv.Client(), coll, map[string]string{}, t.TempDir()); err == nil {
		t.Fatal("expected error on invalid JSON in Record")
	}

	// Record MkdirAll error: pass a regular file path as dir
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "a-file")
	_ = os.WriteFile(filePath, []byte("x"), 0o644)
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer goodSrv.Close()
	goodColl := Collection{Name: "c", URLTemplate: goodSrv.URL, InsecureAllowLoopback: true}
	if _, err := Record(context.Background(), goodSrv.Client(), goodColl, map[string]string{}, filepath.Join(filePath, "sub")); err == nil {
		t.Fatal("expected error on MkdirAll failure")
	}

	// Record WriteFile error: path already exists as directory
	targetPath := filepath.Join(tempDir, SnapshotKey(goodColl.Name, map[string]string{}))
	_ = os.MkdirAll(targetPath, 0o755)
	if _, err := Record(context.Background(), goodSrv.Client(), goodColl, map[string]string{}, tempDir); err == nil {
		t.Fatal("expected error when target snapshot path is a directory")
	}
}

