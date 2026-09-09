package dalgo2http

import (
	"strings"
	"testing"
)

func TestExtractRows(t *testing.T) {
	cases := []struct {
		name            string
		body            string
		rowsPath        string
		wantLen         int
		wantErr         bool
		wantErrContains string // when set, the failing path/segment the Phase 1 HTTP bounds require ("validate JSON/row shape" — errors must name the path that failed)
	}{
		{name: "root array", body: `[{"a":1},{"a":2}]`, wantLen: 2},
		{name: "root object is one row", body: `{"a":1}`, wantLen: 1},
		{name: "nested path to array", body: `{"data":{"rows":[{"a":1}]}}`, rowsPath: "data.rows", wantLen: 1},
		{name: "nested path to object", body: `{"data":{"a":1}}`, rowsPath: "data", wantLen: 1},
		{name: "missing path segment", body: `{"data":{}}`, rowsPath: "data.rows", wantErr: true, wantErrContains: `rowsPath "data.rows"`},
		{name: "path through non-object", body: `{"data":1}`, rowsPath: "data.rows", wantErr: true, wantErrContains: `rowsPath "data.rows"`},
		{name: "row is not an object", body: `[1,2]`, wantErr: true, wantErrContains: "row 0"},
		{name: "rows value is a scalar", body: `{"data":1}`, rowsPath: "data", wantErr: true, wantErrContains: `rowsPath "data"`},
		{name: "invalid JSON", body: `not json`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := extractRows([]byte(tc.body), tc.rowsPath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("extractRows() = nil error, want error")
				}
				if tc.wantErrContains != "" && !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("extractRows() err = %q, want it to contain %q (the failing path)", err.Error(), tc.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractRows() = %v", err)
			}
			if len(rows) != tc.wantLen {
				t.Fatalf("len(rows) = %d, want %d", len(rows), tc.wantLen)
			}
		})
	}
}
