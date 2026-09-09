package dalgo2http

import (
	"errors"
	"testing"
	"time"
)

func TestLoadConfigYAML(t *testing.T) {
	data := []byte(`
mode: live
collections:
  - name: countries
    urlTemplate: "https://example.test/countries/currency/q?country={name}"
    keyField: name
    rowsPath: data
    timeout: 5s
    params:
      name:
        location: query
`)
	cfg, err := LoadConfigYAML(data)
	if err != nil {
		t.Fatalf("LoadConfigYAML: %v", err)
	}
	if cfg.Mode != ModeLive {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeLive)
	}
	if len(cfg.Collections) != 1 {
		t.Fatalf("len(Collections) = %d, want 1", len(cfg.Collections))
	}
	coll := cfg.Collections[0]
	if coll.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %v, want 5s", coll.Timeout)
	}
	if coll.Params["name"].Location != ParamQuery {
		t.Fatalf("Params[name].Location = %q, want %q", coll.Params["name"].Location, ParamQuery)
	}
}

func TestLoadConfigJSON(t *testing.T) {
	data := []byte(`{
		"collections": [{
			"name": "fx",
			"urlTemplate": "https://example.test/latest?base=USD&symbols={to}",
			"keyField": "base",
			"params": {"to": {"location": "query"}}
		}]
	}`)
	cfg, err := LoadConfigJSON(data)
	if err != nil {
		t.Fatalf("LoadConfigJSON: %v", err)
	}
	if len(cfg.Collections) != 1 || cfg.Collections[0].Name != "fx" {
		t.Fatalf("unexpected collections: %+v", cfg.Collections)
	}
}

func TestLoadConfigYAML_InvalidTimeout(t *testing.T) {
	_, err := LoadConfigYAML([]byte(`
collections:
  - name: c
    urlTemplate: "https://example.test/{id}"
    keyField: id
    timeout: "not-a-duration"
    params: {id: {location: path}}
`))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want error
	}{
		{
			name: "no collections",
			cfg:  Config{},
			want: ErrInvalidConfig,
		},
		{
			name: "missing name",
			cfg: Config{Collections: []Collection{{
				URLTemplate: "https://example.test", KeyField: "id",
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "missing urlTemplate",
			cfg: Config{Collections: []Collection{{
				Name: "c", KeyField: "id",
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "missing keyField",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test",
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "non-GET method",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test", KeyField: "id", Method: "POST",
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "duplicate collection name",
			cfg: Config{Collections: []Collection{
				{Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id", Params: map[string]Param{"id": {Location: ParamPath}}},
				{Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id", Params: map[string]Param{"id": {Location: ParamPath}}},
			}},
			want: ErrInvalidConfig,
		},
		{
			name: "undeclared placeholder",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id",
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "declared path param not in template",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/fixed", KeyField: "id",
				Params: map[string]Param{"id": {Location: ParamPath}},
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "invalid param location",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id",
				Params: map[string]Param{"id": {Location: "header"}},
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "invalid mode",
			cfg: Config{Mode: "sometimes", Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id",
				Params: map[string]Param{"id": {Location: ParamPath}},
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "header entry missing env var name",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id",
				Params:  map[string]Param{"id": {Location: ParamPath}},
				Headers: map[string]string{"Authorization": ""},
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "plain http rejected by default",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "http://example.test/{id}", KeyField: "id",
				Params: map[string]Param{"id": {Location: ParamPath}},
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "unsupported scheme rejected",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "ftp://example.test/{id}", KeyField: "id",
				Params: map[string]Param{"id": {Location: ParamPath}},
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "InsecureAllowLoopback with a non-loopback host is still rejected",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "http://example.test/{id}", KeyField: "id",
				Params:                map[string]Param{"id": {Location: ParamPath}},
				InsecureAllowLoopback: true,
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "InsecureAllowLoopback with a loopback host is accepted",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "http://127.0.0.1:1234/{id}", KeyField: "id",
				Params:                map[string]Param{"id": {Location: ParamPath}},
				InsecureAllowLoopback: true,
			}}},
			want: nil,
		},
		{
			name: "declared query param named apikey is rejected",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id",
				Params: map[string]Param{"id": {Location: ParamPath}, "apikey": {Location: ParamQuery}},
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "literal query string key named token is rejected",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/data?token=abc123", KeyField: "id",
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "literal query string key containing api_key is rejected",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/data?client_api_key=abc123", KeyField: "id",
			}}},
			want: ErrInvalidConfig,
		},
		{
			name: "header named Authorization is NOT rejected (headers are the correct place)",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id",
				Params:  map[string]Param{"id": {Location: ParamPath}},
				Headers: map[string]string{"Authorization": "MY_TOKEN_ENV"},
			}}},
			want: nil,
		},
		{
			name: "valid",
			cfg: Config{Collections: []Collection{{
				Name: "c", URLTemplate: "https://example.test/{id}", KeyField: "id",
				Params: map[string]Param{"id": {Location: ParamPath}},
			}}},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateConfig(tc.cfg)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("validateConfig() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("validateConfig() = %v, want wrapping %v", err, tc.want)
			}
		})
	}
}

func TestNewDB_InvalidConfig(t *testing.T) {
	if _, err := NewDB(Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewDB(empty) = %v, want ErrInvalidConfig", err)
	}
}
