package dalgo2http

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dal-go/dalgo/dal"
)

const syntheticCSV3 = "Value,Description,Reference\r\n" +
	"\"400-499\",\"synthetic, quoted\",[Invented]\r\n" +
	"700,Imaginary,[Invented]\r\n"

func strictCSV3Collection(url string) Collection {
	return Collection{
		Name: "registry", URLTemplate: url, Decoder: DecoderStrictCSV3,
		KeyField: "Value", ClientSideFilter: true, Timeout: time.Second,
		InsecureAllowLoopback: strings.HasPrefix(url, "http://127.0.0.1"),
	}
}

func TestStrictCSV3Decoder(t *testing.T) {
	rows, err := decodeStrictCSV3([]byte(syntheticCSV3))
	if err != nil || len(rows) != 2 || len(rows[0]) != 3 ||
		rows[0]["Value"] != "400-499" || rows[0]["Description"] != "synthetic, quoted" ||
		rows[0]["Reference"] != "[Invented]" {
		t.Fatalf("native lexical CSV: rows=%#v err=%v", rows, err)
	}
	if _, err := decodeStrictCSV3([]byte(strings.Repeat("x", int(maxStrictCSV3Bytes)+1))); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("byte bound: %v", err)
	}
	var many strings.Builder
	many.WriteString("Value,Description,Reference\n")
	for i := 0; i <= maxStrictCSV3Rows; i++ {
		fmt.Fprintf(&many, "synthetic-%d,description,reference\n", i)
	}
	invalid := map[string][]byte{
		"missing header":   nil,
		"duplicate header": []byte("Value,Value,Reference\na,b,c\n"),
		"reordered header": []byte("Description,Value,Reference\na,b,c\n"),
		"extra header":     []byte("Value,Description,Reference,Extra\na,b,c,d\n"),
		"extra row column": []byte("Value,Description,Reference\na,b,c,d\n"),
		"short row":        []byte("Value,Description,Reference\na,b\n"),
		"malformed quote":  []byte("Value,Description,Reference\na,\"b,c\n"),
		"invalid UTF-8":    append([]byte("Value,Description,Reference\na,"), 0xff),
		"NUL":              []byte("Value,Description,Reference\na,\x00,c\n"),
		"empty Value":      []byte("Value,Description,Reference\n,b,c\n"),
		"duplicate Value":  []byte("Value,Description,Reference\na,b,c\na,d,e\n"),
		"no records":       []byte("Value,Description,Reference\n"),
		"row bound":        []byte(many.String()),
	}
	for name, body := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeStrictCSV3(body); !errors.Is(err, ErrInvalidCSV) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestStrictCSV3LiveQueryAndObservation(t *testing.T) {
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if r.Method != http.MethodGet || r.URL.RawQuery != "" ||
			r.Header.Get("Cache-Control") != "no-store, no-cache" || r.Header.Get("Pragma") != "no-cache" {
			t.Errorf("unexpected upstream request: %s %s %v", r.Method, r.URL, r.Header)
		}
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("ETag", "invented")
		_, _ = fmt.Fprint(w, syntheticCSV3)
	}))
	defer srv.Close()
	coll := strictCSV3Collection(srv.URL)
	db, err := NewDB(Config{Mode: ModeLive, Collections: []Collection{coll}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder()
	reader, err := db.ExecuteQueryToRecordsReader(recorder.WithContext(context.Background()),
		newQuery("registry", dal.WhereField("Value", dal.Equal, "400-499"), 1))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := reader.Next()
	if err != nil || rec.Key().ID != "400-499" || rec.Data().(map[string]any)["Description"] != "synthetic, quoted" {
		t.Fatalf("record=%#v err=%v", rec, err)
	}
	if _, err := reader.Next(); err != dal.ErrNoMoreRecords || reads != 1 {
		t.Fatalf("end=%v reads=%d", err, reads)
	}
	prov, ok := recorder.Last()
	if !ok || prov.Source != SourceLive || prov.Decoder != DecoderStrictCSV3 ||
		prov.UpstreamURL != srv.URL || prov.ContentType != "text/csv" || prov.ETag != "invented" ||
		prov.Bytes != len(syntheticCSV3) || prov.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(syntheticCSV3))) ||
		prov.BaseCurrency != "" || prov.ReferenceDate != "" {
		t.Fatalf("provenance=%#v seen=%v", prov, ok)
	}
	if path, err := Record(context.Background(), noCallClient(t), coll, nil, t.TempDir()); path != "" || !errors.Is(err, dal.ErrNotSupported) {
		t.Fatalf("unauthorized recording: path=%q err=%v", path, err)
	}
	if reads != 1 {
		t.Fatalf("recording attempted upstream read: %d", reads)
	}
}

func TestStrictCSV3ConfigurationAndBounds(t *testing.T) {
	coll := strictCSV3Collection("https://example.invalid/registry.csv")
	for _, cfg := range []Config{
		{Collections: []Collection{coll}},
		{Mode: ModeSnapshot, Collections: []Collection{coll}},
		{Mode: ModeLiveThenSnapshot, Collections: []Collection{coll}},
		{Mode: ModeLive, Snapshots: fstest.MapFS{}, Collections: []Collection{coll}},
	} {
		if _, err := NewDB(cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("mode/snapshot config: %v", err)
		}
	}
	invalid := []Collection{coll, coll, coll, coll, coll, coll, coll, coll, coll, coll, coll}
	invalid[0].RowsPath = "data"
	invalid[1].KeyField = "Description"
	invalid[2].Params = map[string]Param{"Value": {Location: ParamQuery}}
	invalid[3].Headers = map[string]string{"X-Test": "TEST_ENV"}
	invalid[4].ClientSideFilter = false
	invalid[5].Timeout = 0
	invalid[6].Timeout = maxStrictCSV3Time + time.Nanosecond
	invalid[7].URLTemplate += "?filter=x"
	invalid[8].URLTemplate += "#part"
	invalid[9].URLTemplate = "https://user@example.invalid/registry.csv"
	invalid[10].URLTemplate += "?"
	for i, c := range invalid {
		if _, err := NewDB(Config{Mode: ModeLive, Collections: []Collection{c}}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("invalid[%d]: %v", i, err)
		}
	}
	for _, raw := range []string{":invalid", "https:///missing-host"} {
		c := coll
		c.URLTemplate = raw
		if _, err := NewDB(Config{Mode: ModeLive, Collections: []Collection{c}}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("invalid URL %q: %v", raw, err)
		}
	}
	cfg, err := LoadConfigJSON([]byte(`{"mode":"live","collections":[{"name":"registry","urlTemplate":"https://example.invalid/registry.csv","keyField":"Value","decoder":"strict-csv-three-column/1","clientSideFilter":true,"timeout":"10s"}]}`))
	if err != nil || cfg.Collections[0].Decoder != DecoderStrictCSV3 {
		t.Fatalf("loaded config=%#v err=%v", cfg, err)
	}
	if limit := responseByteLimit(DecoderStrictCSV3); limit != maxStrictCSV3Bytes {
		t.Fatalf("byte limit=%d", limit)
	}
	if limit := responseByteLimit(DecoderJSON); limit != maxBodyBytes {
		t.Fatalf("JSON byte limit=%d", limit)
	}
}

func TestStrictCSV3HTTPBoundAndRedirect(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body == "redirect" {
			w.Header().Set("Location", "https://example.invalid/elsewhere")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer srv.Close()
	db, err := NewDB(Config{Mode: ModeLive, Collections: []Collection{strictCSV3Collection(srv.URL)}})
	if err != nil {
		t.Fatal(err)
	}
	body = strings.Repeat("x", int(maxStrictCSV3Bytes)+1)
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), newQuery("registry", nil, 0)); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("overbound response: %v", err)
	}
	body = "redirect"
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), newQuery("registry", nil, 0)); !errors.Is(err, ErrRedirectNotAllowed) {
		t.Fatalf("redirect: %v", err)
	}
	// A custom client must not silently follow redirects for a live-only
	// decoder, even when it would for a generic JSON collection.
	db, err = NewDB(Config{Mode: ModeLive, Client: srv.Client(), Collections: []Collection{strictCSV3Collection(srv.URL)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecuteQueryToRecordsReader(context.Background(), newQuery("registry", nil, 0)); !errors.Is(err, ErrRedirectNotAllowed) {
		t.Fatalf("custom-client redirect: %v", err)
	}
}
