package dalgo2http

import (
	"fmt"
	"reflect"

	"github.com/dal-go/dalgo/dal"
)

// filterRows evaluates cond against each row and keeps only the matching
// ones. It is only ever called for a collection that set ClientSideFilter —
// see query.go's planQuery — because it is exactly the "fetch a superset and
// filter after" behaviour the fail-closed default refuses.
func filterRows(rows []map[string]any, cond dal.Condition) ([]map[string]any, error) {
	if cond == nil {
		return rows, nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		ok, err := evalCondition(row, cond)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func evalCondition(row map[string]any, cond dal.Condition) (bool, error) {
	switch c := cond.(type) {
	case dal.GroupCondition:
		switch c.Operator() {
		case dal.And:
			for _, sub := range c.Conditions() {
				ok, err := evalCondition(row, sub)
				if err != nil || !ok {
					return ok, err
				}
			}
			return true, nil
		case dal.Or:
			for _, sub := range c.Conditions() {
				ok, err := evalCondition(row, sub)
				if err != nil {
					return false, err
				}
				if ok {
					return true, nil
				}
			}
			return false, nil
		default:
			return false, fmt.Errorf("%w: unsupported group operator %q", dal.ErrNotSupported, c.Operator())
		}
	case dal.Comparison:
		return evalComparison(row, c)
	default:
		return false, fmt.Errorf("%w: unsupported condition shape %T", dal.ErrNotSupported, cond)
	}
}

func evalComparison(row map[string]any, cmp dal.Comparison) (bool, error) {
	field, ok := cmp.Left.(dal.FieldRef)
	if !ok {
		return false, fmt.Errorf("%w: left side of a comparison must be a field", dal.ErrNotSupported)
	}
	actual, present := row[field.Name()]
	switch right := cmp.Right.(type) {
	case dal.Constant:
		if !present {
			return false, nil
		}
		switch cmp.Operator {
		case dal.Equal:
			return compareEqual(actual, right.Value), nil
		case dal.GreaterThen, dal.GreaterOrEqual, dal.LessThen, dal.LessOrEqual:
			return compareOrdered(cmp.Operator, actual, right.Value)
		default:
			return false, fmt.Errorf("%w: unsupported operator %q", dal.ErrNotSupported, cmp.Operator)
		}
	case dal.Array:
		if cmp.Operator != dal.In {
			return false, fmt.Errorf("%w: an array operand requires the In operator", dal.ErrNotSupported)
		}
		if !present {
			return false, nil
		}
		return sliceContains(right.Value, actual), nil
	default:
		return false, fmt.Errorf("%w: unsupported right-hand operand %T", dal.ErrNotSupported, right)
	}
}

// compareEqual compares two JSON-decoded values: numerically when both are
// numeric, otherwise by their %v string form (covers string and bool).
func compareEqual(a, b any) bool {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			return af == bf
		}
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int8:
		return float64(t), true
	case int16:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	default:
		return 0, false
	}
}

func compareOrdered(op dal.Operator, a, b any) (bool, error) {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			switch op {
			case dal.GreaterThen:
				return af > bf, nil
			case dal.GreaterOrEqual:
				return af >= bf, nil
			case dal.LessThen:
				return af < bf, nil
			case dal.LessOrEqual:
				return af <= bf, nil
			}
		}
	}
	if as, aok := a.(string); aok {
		if bs, bok := b.(string); bok {
			switch op {
			case dal.GreaterThen:
				return as > bs, nil
			case dal.GreaterOrEqual:
				return as >= bs, nil
			case dal.LessThen:
				return as < bs, nil
			case dal.LessOrEqual:
				return as <= bs, nil
			}
		}
	}
	return false, fmt.Errorf("%w: cannot compare %T and %T with %q", dal.ErrNotSupported, a, b, op)
}

func sliceContains(arr any, v any) bool {
	rv := reflect.ValueOf(arr)
	if rv.Kind() != reflect.Slice {
		return false
	}
	for i := 0; i < rv.Len(); i++ {
		if compareEqual(rv.Index(i).Interface(), v) {
			return true
		}
	}
	return false
}
