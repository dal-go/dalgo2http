package dalgo2http

import (
	"testing"

	"github.com/dal-go/dalgo/dal"
)

func TestFilterRows(t *testing.T) {
	rows := []map[string]any{
		{"name": "France", "population": 68.0, "region": "Europe"},
		{"name": "Germany", "population": 84.0, "region": "Europe"},
		{"name": "Japan", "population": 125.0, "region": "Asia"},
	}

	cases := []struct {
		name string
		cond dal.Condition
		want []string
	}{
		{
			name: "nil condition keeps everything",
			cond: nil,
			want: []string{"France", "Germany", "Japan"},
		},
		{
			name: "equality",
			cond: dal.WhereField("region", dal.Equal, "Europe"),
			want: []string{"France", "Germany"},
		},
		{
			name: "greater than",
			cond: dal.WhereField("population", dal.GreaterThen, 70.0),
			want: []string{"Germany", "Japan"},
		},
		{
			name: "less or equal",
			cond: dal.WhereField("population", dal.LessOrEqual, 84.0),
			want: []string{"France", "Germany"},
		},
		{
			name: "AND group",
			cond: dal.NewGroupCondition(dal.And,
				dal.WhereField("region", dal.Equal, "Europe"),
				dal.WhereField("population", dal.GreaterThen, 70.0)),
			want: []string{"Germany"},
		},
		{
			name: "OR group",
			cond: dal.NewGroupCondition(dal.Or,
				dal.WhereField("region", dal.Equal, "Asia"),
				dal.WhereField("name", dal.Equal, "France")),
			want: []string{"France", "Japan"},
		},
		{
			name: "In array",
			cond: dal.Comparison{Left: dal.Field("name"), Operator: dal.In, Right: dal.NewArray([]string{"Japan", "France"})},
			want: []string{"France", "Japan"},
		},
		{
			name: "missing field never matches",
			cond: dal.WhereField("continent", dal.Equal, "Europe"),
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := filterRows(rows, tc.cond)
			if err != nil {
				t.Fatalf("filterRows: %v", err)
			}
			var names []string
			for _, row := range got {
				names = append(names, row["name"].(string))
			}
			if !equalStrings(names, tc.want) {
				t.Fatalf("filterRows() names = %v, want %v", names, tc.want)
			}
		})
	}
}

func TestFilterRows_UnsupportedShapes(t *testing.T) {
	rows := []map[string]any{{"name": "France"}}
	if _, err := filterRows(rows, dal.NewGroupCondition("XOR", dal.WhereField("name", dal.Equal, "France"))); !isNotSupported(err) {
		t.Fatalf("err = %v, want dal.ErrNotSupported", err)
	}
	if _, err := filterRows(rows, dal.Comparison{Left: dal.Constant{Value: "x"}, Operator: dal.Equal, Right: dal.Constant{Value: "x"}}); !isNotSupported(err) {
		t.Fatalf("non-field left side: err = %v, want dal.ErrNotSupported", err)
	}
	if _, err := filterRows(rows, dal.Comparison{Left: dal.Field("name"), Operator: dal.Equal, Right: dal.Field("other")}); !isNotSupported(err) {
		t.Fatalf("non-constant/array right side: err = %v, want dal.ErrNotSupported", err)
	}
	if _, err := filterRows(rows, dal.Comparison{Left: dal.Field("name"), Operator: dal.In, Right: dal.Constant{Value: "France"}}); !isNotSupported(err) {
		t.Fatalf("In with non-array right side: err = %v, want dal.ErrNotSupported", err)
	}
	if _, err := filterRows(rows, dal.Comparison{Left: dal.Field("name"), Operator: dal.GreaterThen, Right: dal.Array{Value: []string{"a"}}}); !isNotSupported(err) {
		t.Fatalf("array with non-In operator: err = %v, want dal.ErrNotSupported", err)
	}
	if _, err := filterRows(rows, dal.Comparison{Left: dal.Field("name"), Operator: dal.GreaterThen, Right: dal.Constant{Value: true}}); !isNotSupported(err) {
		t.Fatalf("incomparable types: err = %v, want dal.ErrNotSupported", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
