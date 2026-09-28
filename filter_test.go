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

type dummyCondition struct{}

func (dummyCondition) String() string { return "dummy" }

func TestFilterRows_MoreBranches(t *testing.T) {
	rows := []map[string]any{{"name": "France", "population": 68.0}}

	// evalCondition: sub-condition in OR errors
	badSub := dal.Comparison{Left: dal.Constant{Value: "x"}, Operator: dal.Equal, Right: dal.Constant{Value: "x"}}
	orWithErr := dal.NewGroupCondition(dal.Or, badSub)
	if _, err := filterRows(rows, orWithErr); err == nil {
		t.Fatal("expected error in OR sub-condition")
	}

	// evalCondition: unsupported condition shape
	if _, err := filterRows(rows, dummyCondition{}); err == nil {
		t.Fatal("expected error for unsupported condition shape")
	}

	// In operator: missing field in row
	inMissing := dal.Comparison{Left: dal.Field("missing"), Operator: dal.In, Right: dal.NewArray([]string{"France"})}
	got, err := filterRows(rows, inMissing)
	if err != nil || len(got) != 0 {
		t.Fatalf("expected 0 matches for missing field in In, got %v, err %v", got, err)
	}

	// sliceContains: arr not a slice
	if sliceContains(123, "val") {
		t.Fatal("expected false for non-slice")
	}
}

func TestCompareEqual_NumericAndNonNumeric(t *testing.T) {
	// both numeric
	if !compareEqual(10, 10) {
		t.Fatal("expected true")
	}
	if compareEqual(10, 20) {
		t.Fatal("expected false")
	}
	// toFloat(a) is true, toFloat(b) is false
	if compareEqual(10, "not-a-number") {
		t.Fatal("expected false")
	}
	// string equality
	if !compareEqual("abc", "abc") {
		t.Fatal("expected true")
	}
}

func TestToFloat_AllTypes(t *testing.T) {
	vals := []any{
		float32(1), int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
	}
	for _, v := range vals {
		f, ok := toFloat(v)
		if !ok || f != 1.0 {
			t.Fatalf("toFloat(%T(%v)) = (%v, %v), want (1.0, true)", v, v, f, ok)
		}
	}
	if _, ok := toFloat("string"); ok {
		t.Fatal("expected false for string")
	}
}

func TestCompareOrdered_AllOperatorsAndTypes(t *testing.T) {
	ops := []dal.Operator{dal.GreaterThen, dal.GreaterOrEqual, dal.LessThen, dal.LessOrEqual}
	for _, op := range ops {
		if _, err := compareOrdered(op, 10.0, 10.0); err != nil {
			t.Fatalf("compareOrdered(%s) float failed: %v", op, err)
		}
		if _, err := compareOrdered(op, "b", "b"); err != nil {
			t.Fatalf("compareOrdered(%s) string failed: %v", op, err)
		}
	}

	// string compared to non-string
	if _, err := compareOrdered(dal.GreaterThen, "a", 10); err == nil {
		t.Fatal("expected error comparing string to int")
	}
}

