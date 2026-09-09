package dalgo2http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildURL(t *testing.T) {
	t.Run("path parameter", func(t *testing.T) {
		coll := Collection{
			Name:        "c",
			URLTemplate: "https://example.test/countries/{code}",
			Params:      map[string]Param{"code": {Location: ParamPath}},
		}
		got, err := buildURL(coll, map[string]string{"code": "fr ance"})
		if err != nil {
			t.Fatalf("buildURL: %v", err)
		}
		want := "https://example.test/countries/fr%20ance"
		if got != want {
			t.Fatalf("buildURL() = %q, want %q", got, want)
		}
	})

	t.Run("query parameter embedded in template", func(t *testing.T) {
		coll := Collection{
			Name:        "fx",
			URLTemplate: "https://example.test/latest?base=USD&symbols={to}",
			Params:      map[string]Param{"to": {Location: ParamQuery}},
		}
		got, err := buildURL(coll, map[string]string{"to": "EUR"})
		if err != nil {
			t.Fatalf("buildURL: %v", err)
		}
		want := "https://example.test/latest?base=USD&symbols=EUR"
		if got != want {
			t.Fatalf("buildURL() = %q, want %q", got, want)
		}
	})

	t.Run("query parameter appended when not a placeholder", func(t *testing.T) {
		coll := Collection{
			Name:        "c",
			URLTemplate: "https://example.test/search",
			Params:      map[string]Param{"q": {Location: ParamQuery}},
		}
		got, err := buildURL(coll, map[string]string{"q": "hello world"})
		if err != nil {
			t.Fatalf("buildURL: %v", err)
		}
		want := "https://example.test/search?q=hello+world"
		if got != want {
			t.Fatalf("buildURL() = %q, want %q", got, want)
		}
	})

	t.Run("query parameter omitted when no value given", func(t *testing.T) {
		coll := Collection{
			Name:        "c",
			URLTemplate: "https://example.test/search",
			Params:      map[string]Param{"q": {Location: ParamQuery}},
		}
		got, err := buildURL(coll, map[string]string{})
		if err != nil {
			t.Fatalf("buildURL: %v", err)
		}
		want := "https://example.test/search"
		if got != want {
			t.Fatalf("buildURL() = %q, want %q", got, want)
		}
	})

	t.Run("missing required path parameter fails closed", func(t *testing.T) {
		coll := Collection{
			Name:        "c",
			URLTemplate: "https://example.test/countries/{code}",
			Params:      map[string]Param{"code": {Location: ParamPath}},
		}
		_, err := buildURL(coll, map[string]string{})
		if !errors.Is(err, ErrMissingParam) {
			t.Fatalf("buildURL() err = %v, want ErrMissingParam", err)
		}
	})
}

// TestDoLiveFetch_ResponseTooLarge proves the Phase 1 HTTP bounds' "Bound
// response bytes to 2 MiB": an oversized body fails explicitly with
// ErrResponseTooLarge rather than being silently truncated by the
// LimitReader and then failing JSON decoding downstream with a misleading
// "unexpected end of JSON input".
func TestDoLiveFetch_ResponseTooLarge(t *testing.T) {
	oversized := strings.Repeat("a", maxBodyBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(oversized))
	}))
	defer srv.Close()

	coll := Collection{Name: "big", URLTemplate: srv.URL, KeyField: "id", InsecureAllowLoopback: true}
	_, _, err := doLiveFetch(context.Background(), srv.Client(), coll, srv.URL)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("doLiveFetch() err = %v, want ErrResponseTooLarge", err)
	}
}

// TestDoLiveFetch_ResponseAtLimitSucceeds proves the limit is exactly
// maxBodyBytes, not one byte short of it: a body of precisely that size is
// not rejected.
func TestDoLiveFetch_ResponseAtLimitSucceeds(t *testing.T) {
	exact := `{"pad":"` + strings.Repeat("a", maxBodyBytes-10) + `"}`
	if len(exact) != maxBodyBytes {
		t.Fatalf("test fixture len = %d, want exactly maxBodyBytes = %d", len(exact), maxBodyBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(exact))
	}))
	defer srv.Close()

	coll := Collection{Name: "exact", URLTemplate: srv.URL, KeyField: "id", InsecureAllowLoopback: true}
	body, _, err := doLiveFetch(context.Background(), srv.Client(), coll, srv.URL)
	if err != nil {
		t.Fatalf("doLiveFetch() at exactly maxBodyBytes = %v, want success", err)
	}
	if len(body) != maxBodyBytes {
		t.Fatalf("len(body) = %d, want %d", len(body), maxBodyBytes)
	}
}
