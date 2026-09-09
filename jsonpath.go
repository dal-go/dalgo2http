package dalgo2http

import (
	"encoding/json"
	"fmt"
	"strings"
)

// extractRows decodes body and resolves rowsPath (a dot-separated path of
// JSON object field names; empty means the root) to the rows value, then
// requires that value to be either a JSON array of objects, or a single JSON
// object treated as one row — the shape Collection.RowsPath documents.
func extractRows(body []byte, rowsPath string) ([]map[string]any, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("dalgo2http: decode response JSON: %w", err)
	}
	if rowsPath != "" {
		for _, seg := range strings.Split(rowsPath, ".") {
			m, ok := decoded.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("dalgo2http: rowsPath %q: %q is not a JSON object", rowsPath, seg)
			}
			decoded, ok = m[seg]
			if !ok {
				return nil, fmt.Errorf("dalgo2http: rowsPath %q: field %q not found in response", rowsPath, seg)
			}
		}
	}
	switch v := decoded.(type) {
	case []any:
		rows := make([]map[string]any, 0, len(v))
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("dalgo2http: rowsPath %q: row %d is not a JSON object (got %T)", rowsPath, i, item)
			}
			rows = append(rows, m)
		}
		return rows, nil
	case map[string]any:
		return []map[string]any{v}, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("dalgo2http: rowsPath %q: value is neither a JSON array nor a JSON object (got %T)", rowsPath, decoded)
	}
}
