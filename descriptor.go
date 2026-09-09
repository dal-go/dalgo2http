package dalgo2http

import (
	"regexp"
	"time"
)

// Method is the HTTP method a Collection is fetched with. v0.x supports GET
// only — see README.md "Design constraints".
type Method string

// MethodGET is the only Method a Collection may declare.
const MethodGET Method = "GET"

// ParamLocation says where a declared parameter's value is placed in the
// request: substituted into a {name} placeholder in the path portion of the
// URL template, or attached as a query-string value (either by substituting
// a {name} placeholder embedded in the template's query string, or, if the
// name never appears in the template, appended as an extra query parameter).
type ParamLocation string

const (
	ParamPath  ParamLocation = "path"
	ParamQuery ParamLocation = "query"
)

// Param declares one substitutable field of a Collection's URL template.
type Param struct {
	Location ParamLocation `yaml:"location" json:"location"`
}

// Collection declaratively describes one read-only HTTP/JSON source: the
// endpoint to call, which fields of a dal.Query can be pushed into it, where
// the rows live in the JSON response, and which field identifies a row's
// dal.DB record key.
//
// A Collection carries no secrets: Headers maps a header name to the name of
// an environment variable dalgo2http reads at request time, never a literal
// value (see README.md "Design constraints").
type Collection struct {
	// Name is the collection name a record.Key or dal.Query.From() names.
	Name string `yaml:"name" json:"name"`

	// URLTemplate is the request URL, with {name} placeholders for every
	// declared Param that is substituted directly into the template (a path
	// segment, or a value embedded in a literal query string such as
	// "...?symbols={to}"). A Param declared with ParamLocation "query" whose
	// name does NOT appear in the template is instead appended as an extra
	// "?name=value" query parameter when a value is supplied.
	URLTemplate string `yaml:"urlTemplate" json:"urlTemplate"`

	// Method is the HTTP method. Empty means MethodGET; any other value
	// fails validation.
	Method Method `yaml:"method,omitempty" json:"method,omitempty"`

	// Params declares every field a dal.Query equality condition, or a
	// record.Key ID via KeyField, may be pushed down as.
	Params map[string]Param `yaml:"params,omitempty" json:"params,omitempty"`

	// RowsPath is the dot-separated JSON path (object-field traversal only)
	// to the row array in the response body. Empty means the root of the
	// response IS the rows value: a JSON array of row objects, or a single
	// JSON object treated as one row.
	RowsPath string `yaml:"rowsPath,omitempty" json:"rowsPath,omitempty"`

	// KeyField is the row field whose value becomes a record.Key's ID.
	// dal.DB.Get/Exists for this collection only work when KeyField is also
	// a declared Param (see query.go); otherwise Get/Exists fail closed with
	// an error wrapping dal.ErrNotSupported, and only ExecuteQueryToRecords*
	// is usable.
	KeyField string `yaml:"keyField" json:"keyField"`

	// Headers maps an HTTP header name to the name of an environment
	// variable holding its value. A variable that is unset or empty at
	// request time means the header is simply not sent.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Timeout bounds one request to this collection's endpoint. Zero means
	// no adapter-imposed timeout beyond the context's own deadline.
	//
	// This field is not (de)serialized directly: LoadConfigYAML/LoadConfigJSON
	// read it from a "timeout" string (e.g. "10s", parsed with
	// time.ParseDuration) since YAML/JSON have no native duration type.
	Timeout time.Duration `yaml:"-" json:"-"`

	// ClientSideFilter opts a collection into fetching a broader result than
	// a dal.Query's Where() strictly asks for, and filtering the remainder
	// in memory. It exists ONLY for public, non-confidential reference data:
	// the point of failing closed by default (see query.go) is that an
	// access-policy predicate that cannot be pushed down must refuse, not
	// silently fetch a superset and hide the extra rows after the fact. Set
	// this only on a collection where over-fetching cannot leak anything a
	// caller was not already allowed to see.
	ClientSideFilter bool `yaml:"clientSideFilter,omitempty" json:"clientSideFilter,omitempty"`
}

// placeholderRe matches a {name} placeholder in a URL template.
var placeholderRe = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)
