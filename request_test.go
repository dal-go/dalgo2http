package dalgo2http

import (
	"errors"
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
