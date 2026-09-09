package dalgo2http

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Mode selects where a collection's rows come from.
type Mode string

const (
	// ModeLive always fetches live and never falls back to a snapshot: a
	// live failure is returned to the caller as-is.
	ModeLive Mode = "live"

	// ModeSnapshot never calls the network: every read is served from
	// Config.Snapshots, failing with ErrSnapshotMiss when none exists.
	ModeSnapshot Mode = "snapshot"

	// ModeLiveThenSnapshot (the default — see NewDB) fetches live and, only
	// when the live request fails for a transient reason (network error,
	// timeout, or a 5xx/429 response — never a 4xx, which is treated as a
	// caller/config error) AND Config.Snapshots has a matching snapshot,
	// falls back to it. This is the behaviour item 4 of the stream brief
	// describes; naming it as its own Mode (rather than making it the only
	// behaviour) lets a caller pin ModeLive for "never serve stale data" or
	// ModeSnapshot for fully offline runs.
	ModeLiveThenSnapshot Mode = "live-then-snapshot"
)

// Config configures a dalgo2http database: the collections it serves, the
// HTTP client and optional recorded-snapshot store it reads through, and the
// Mode governing live-vs-snapshot behaviour.
type Config struct {
	Collections []Collection

	// Client performs live HTTP requests. Nil means http.DefaultClient.
	Client *http.Client

	// Snapshots is an optional read-only store of recorded fixtures, keyed
	// by SnapshotKey(collection, params). Nil means no snapshot fallback is
	// possible regardless of Mode. Use Record to populate a directory that
	// can then be wrapped with os.DirFS (or embed.FS for shipped fixtures).
	Snapshots fs.FS

	// Mode governs live-vs-snapshot behaviour. Empty means
	// ModeLiveThenSnapshot.
	Mode Mode
}

// configFile is the YAML/JSON-serializable shape of a Config. It exists
// separately from Config because Config carries non-serializable runtime
// values (Client, Snapshots) and because Collection.Timeout is a
// time.Duration, which YAML/JSON have no native representation for.
type configFile struct {
	Collections []collectionFile `yaml:"collections" json:"collections"`
	Mode        string           `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type collectionFile struct {
	Name             string            `yaml:"name" json:"name"`
	URLTemplate      string            `yaml:"urlTemplate" json:"urlTemplate"`
	Method           string            `yaml:"method,omitempty" json:"method,omitempty"`
	Params           map[string]Param  `yaml:"params,omitempty" json:"params,omitempty"`
	RowsPath         string            `yaml:"rowsPath,omitempty" json:"rowsPath,omitempty"`
	KeyField         string            `yaml:"keyField" json:"keyField"`
	Headers          map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Timeout          string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	ClientSideFilter bool              `yaml:"clientSideFilter,omitempty" json:"clientSideFilter,omitempty"`
}

// LoadConfigYAML parses a Config from YAML in the shape documented on
// configFile/collectionFile (see also the example descriptors under
// examples/), and validates it exactly as NewDB does.
func LoadConfigYAML(data []byte) (Config, error) {
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return Config{}, fmt.Errorf("%w: parse YAML: %v", ErrInvalidConfig, err)
	}
	return configFromFile(file)
}

// LoadConfigJSON parses a Config from JSON. See LoadConfigYAML.
func LoadConfigJSON(data []byte) (Config, error) {
	var file configFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Config{}, fmt.Errorf("%w: parse JSON: %v", ErrInvalidConfig, err)
	}
	return configFromFile(file)
}

func configFromFile(file configFile) (Config, error) {
	cfg := Config{Mode: Mode(file.Mode)}
	for _, cf := range file.Collections {
		coll := Collection{
			Name:             cf.Name,
			URLTemplate:      cf.URLTemplate,
			Method:           Method(cf.Method),
			Params:           cf.Params,
			RowsPath:         cf.RowsPath,
			KeyField:         cf.KeyField,
			Headers:          cf.Headers,
			ClientSideFilter: cf.ClientSideFilter,
		}
		if cf.Timeout != "" {
			d, err := time.ParseDuration(cf.Timeout)
			if err != nil {
				return Config{}, fmt.Errorf("%w: collection %q: invalid timeout %q: %v", ErrInvalidConfig, cf.Name, cf.Timeout, err)
			}
			coll.Timeout = d
		}
		cfg.Collections = append(cfg.Collections, coll)
	}
	if _, err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validateConfig validates cfg and returns its collections indexed by name.
// It is called both by LoadConfigYAML/LoadConfigJSON (so a bad file fails
// fast, with a file-shaped error) and by NewDB (so a Config built by hand in
// Go gets the same validation a loaded one does).
func validateConfig(cfg Config) (map[string]Collection, error) {
	if len(cfg.Collections) == 0 {
		return nil, fmt.Errorf("%w: at least one collection is required", ErrInvalidConfig)
	}
	switch cfg.Mode {
	case "", ModeLive, ModeSnapshot, ModeLiveThenSnapshot:
	default:
		return nil, fmt.Errorf("%w: mode %q is not one of %q, %q, %q", ErrInvalidConfig, cfg.Mode, ModeLive, ModeSnapshot, ModeLiveThenSnapshot)
	}
	index := make(map[string]Collection, len(cfg.Collections))
	for _, coll := range cfg.Collections {
		if err := coll.validate(); err != nil {
			return nil, err
		}
		if _, dup := index[coll.Name]; dup {
			return nil, fmt.Errorf("%w: duplicate collection name %q", ErrInvalidConfig, coll.Name)
		}
		index[coll.Name] = coll
	}
	return index, nil
}

// validate checks one Collection in isolation: required fields, a supported
// Method, valid Param locations, and that every {name} placeholder in
// URLTemplate has a declared Param (and every declared path Param appears as
// a placeholder — a path Param that is never substituted anywhere is very
// likely a config mistake, so this is rejected rather than silently ignored).
func (coll Collection) validate() error {
	if coll.Name == "" {
		return fmt.Errorf("%w: collection name is required", ErrInvalidConfig)
	}
	if coll.URLTemplate == "" {
		return fmt.Errorf("%w: collection %q: urlTemplate is required", ErrInvalidConfig, coll.Name)
	}
	if coll.Method != "" && coll.Method != MethodGET {
		return fmt.Errorf("%w: collection %q: method %q is not supported, this adapter is read-only GET-only", ErrInvalidConfig, coll.Name, coll.Method)
	}
	if coll.KeyField == "" {
		return fmt.Errorf("%w: collection %q: keyField is required", ErrInvalidConfig, coll.Name)
	}
	for name, p := range coll.Params {
		if p.Location != ParamPath && p.Location != ParamQuery {
			return fmt.Errorf("%w: collection %q: param %q has location %q, want %q or %q", ErrInvalidConfig, coll.Name, name, p.Location, ParamPath, ParamQuery)
		}
	}
	seen := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(coll.URLTemplate, -1) {
		name := m[1]
		seen[name] = true
		if _, declared := coll.Params[name]; !declared {
			return fmt.Errorf("%w: collection %q: urlTemplate references undeclared parameter %q", ErrInvalidConfig, coll.Name, name)
		}
	}
	for name, p := range coll.Params {
		if p.Location == ParamPath && !seen[name] {
			return fmt.Errorf("%w: collection %q: path parameter %q does not appear in urlTemplate", ErrInvalidConfig, coll.Name, name)
		}
	}
	for header, envVar := range coll.Headers {
		if header == "" || envVar == "" {
			return fmt.Errorf("%w: collection %q: headers entries require both a header name and an environment variable name", ErrInvalidConfig, coll.Name)
		}
	}

	parsed, err := url.Parse(coll.URLTemplate)
	if err != nil {
		return fmt.Errorf("%w: collection %q: urlTemplate is not a valid URL: %v", ErrInvalidConfig, coll.Name, err)
	}
	if err := coll.validateScheme(parsed); err != nil {
		return err
	}
	if err := coll.validateNoSecretsInQueryString(parsed); err != nil {
		return err
	}
	return nil
}

// validateScheme enforces the Phase 1 HTTP bounds' "the descriptor fixes the
// HTTPS host/path": only https:// is allowed, unless InsecureAllowLoopback is
// set AND parsed's host is literally a loopback address — see
// Collection.InsecureAllowLoopback's doc comment for why, and its scope.
func (coll Collection) validateScheme(parsed *url.URL) error {
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if !coll.InsecureAllowLoopback {
			return fmt.Errorf("%w: collection %q: urlTemplate must use https:// (got %q); set InsecureAllowLoopback (test-only) for a loopback httptest server", ErrInvalidConfig, coll.Name, parsed.Scheme)
		}
		if !isLoopbackHost(parsed.Hostname()) {
			return fmt.Errorf("%w: collection %q: InsecureAllowLoopback only permits a loopback host (127.0.0.1, ::1, localhost), got %q", ErrInvalidConfig, coll.Name, parsed.Hostname())
		}
		return nil
	default:
		return fmt.Errorf("%w: collection %q: urlTemplate scheme %q is not supported (want https://, or http:// with InsecureAllowLoopback set for a loopback test server)", ErrInvalidConfig, coll.Name, parsed.Scheme)
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// secretQueryKeyMarkers lists case-insensitive substrings the Phase 1 HTTP
// bounds forbid in a query-string parameter name ("Never send secrets in
// query strings"): a name containing one of these almost always carries a
// credential a request log, browser history entry, or proxy could capture in
// plaintext. Collection.Headers (resolved from an environment variable at
// request time, never a literal) is the documented, correct place for a
// value like this instead — see descriptor.go's Collection doc comment.
var secretQueryKeyMarkers = []string{"token", "apikey", "api_key", "secret", "password", "authorization"}

func isSecretLikeName(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range secretQueryKeyMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// validateNoSecretsInQueryString enforces the Phase 1 HTTP bounds' "never
// send secrets in query strings": neither a declared query-location Param
// name nor a literal query-string key already present in urlTemplate may
// look like a credential (see secretQueryKeyMarkers). Path-location Params
// and header names are out of scope — the contract names query strings
// specifically, and Headers is the intended place for a credential.
func (coll Collection) validateNoSecretsInQueryString(parsed *url.URL) error {
	for name, p := range coll.Params {
		if p.Location == ParamQuery && isSecretLikeName(name) {
			return fmt.Errorf("%w: collection %q: query parameter %q looks like a credential name; secrets must never be sent in a query string — use Headers (an environment variable) instead", ErrInvalidConfig, coll.Name, name)
		}
	}
	for key := range parsed.Query() {
		if isSecretLikeName(key) {
			return fmt.Errorf("%w: collection %q: urlTemplate's query string has a parameter named %q, which looks like a credential; secrets must never be sent in a query string", ErrInvalidConfig, coll.Name, key)
		}
	}
	return nil
}
