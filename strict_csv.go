package dalgo2http

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"
	"unicode/utf8"
)

const (
	maxStrictCSV3Bytes int64 = 64 << 10
	maxStrictCSV3Rows        = 512
	maxStrictCSV3Time        = 10 * time.Second
)

// ErrInvalidCSV reports a shape or encoding violation without exposing source
// content in diagnostics.
var ErrInvalidCSV = errors.New("dalgo2http: invalid CSV response")

func (coll Collection) validateStrictCSV3() error {
	if coll.RowsPath != "" || coll.KeyField != "Value" || len(coll.Params) != 0 ||
		len(coll.Headers) != 0 || !coll.ClientSideFilter || coll.Timeout <= 0 || coll.Timeout > maxStrictCSV3Time {
		return fmt.Errorf("%w: collection %q: strict CSV requires Value key, no rows path, params or headers, client-side filtering and a timeout up to 10s", ErrInvalidConfig, coll.Name)
	}
	parsed, err := url.Parse(coll.URLTemplate)
	if err != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("%w: collection %q: strict CSV requires a fixed URL without credentials, query or fragment", ErrInvalidConfig, coll.Name)
	}
	return nil
}

func decodeStrictCSV3(body []byte) ([]map[string]any, error) {
	if int64(len(body)) > maxStrictCSV3Bytes {
		return nil, ErrResponseTooLarge
	}
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return nil, fmt.Errorf("%w: invalid text encoding", ErrInvalidCSV)
	}
	reader := csv.NewReader(bytes.NewReader(body))
	reader.FieldsPerRecord = 3
	// encoding/csv handles quoted commas, CRLF and multiline quoted fields;
	// LazyQuotes stays false, so malformed quoting fails closed.
	header, err := reader.Read()
	if err != nil || len(header) != 3 || header[0] != "Value" ||
		header[1] != "Description" || header[2] != "Reference" {
		return nil, fmt.Errorf("%w: expected Value,Description,Reference header", ErrInvalidCSV)
	}
	rows := make([]map[string]any, 0)
	seen := make(map[string]bool)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: malformed record", ErrInvalidCSV)
		}
		if record[0] == "" || seen[record[0]] {
			return nil, fmt.Errorf("%w: empty or duplicate Value", ErrInvalidCSV)
		}
		if len(rows) >= maxStrictCSV3Rows {
			return nil, fmt.Errorf("%w: row bound exceeded", ErrInvalidCSV)
		}
		seen[record[0]] = true
		rows = append(rows, map[string]any{
			"Value": record[0], "Description": record[1], "Reference": record[2],
		})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: no records", ErrInvalidCSV)
	}
	return rows, nil
}
