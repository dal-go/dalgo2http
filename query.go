package dalgo2http

import (
	"context"
	"fmt"
	"time"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
)

// ExecuteQueryToRecordsReader implements dal.QueryExecutor. Only structured
// queries are supported (a raw dal.TextQuery has no field-level structure to
// push down, so it fails closed with dal.ErrNotSupported); see planQuery for
// what a structured query must look like to be answerable at all, and
// collectEqualities for exactly which Where() shapes are pushed into the URL.
func (d *database) ExecuteQueryToRecordsReader(ctx context.Context, query dal.Query) (dal.RecordsReader, error) {
	q, ok := query.(dal.StructuredQuery)
	if !ok {
		return nil, fmt.Errorf("%w: dalgo2http answers structured queries only", dal.ErrNotSupported)
	}
	plan, err := d.planQuery(q)
	if err != nil {
		return nil, err
	}
	rows, _, err := d.fetchRows(ctx, plan.collection, plan.equalities)
	if err != nil {
		return nil, err
	}
	if plan.residual {
		if rows, err = filterRows(rows, q.Where()); err != nil {
			return nil, err
		}
	}
	// Projection is applied AFTER residual filtering (a client-side
	// condition may reference a field the caller did not SELECT) and BEFORE
	// rowsToRecords, so an un-requested field never reaches the returned
	// record at all — see applyProjection.
	if plan.projection != nil {
		rows = applyProjection(rows, plan.projection, plan.collection.KeyField)
	}
	records, err := rowsToRecords(plan.collection, rows, q.IntoRecord)
	if err != nil {
		return nil, err
	}
	// Limit is the one shaping operation this adapter applies after fetch
	// (see README.md "Design constraints"); Offset is refused earlier in
	// planQuery, alongside OrderBy, GroupBy/Having, joins and cursors.
	if limit := q.Limit(); limit > 0 && limit < len(records) {
		records = records[:limit]
	}
	return dal.NewRecordsReader(records), nil
}

// queryPlan is the outcome of successfully planning a structured query: the
// resolved Collection, the equality constraints to push into the URL,
// whether any part of Where() could not be reduced to a pushable equality
// (residual) — meaningful only when the collection allows ClientSideFilter,
// since planQuery itself refuses a residual condition otherwise — and the
// requested column projection, if any (see applyProjection; nil means every
// field the response returns).
type queryPlan struct {
	collection Collection
	equalities map[string]string
	residual   bool
	projection []string
}

// planQuery fails closed on anything this adapter cannot express as one GET:
// joins, GROUP BY / HAVING, ORDER BY, a non-zero Offset, and start
// cursors are always refused. Where() is walked by collectEqualities; a part
// of it that cannot be reduced to `declaredParam == constant` combined with
// AND is refused too, UNLESS the collection sets ClientSideFilter, in which
// case the pushable equalities still narrow the request and the residual
// condition is evaluated in memory afterwards (see filterRows).
func (d *database) planQuery(q dal.StructuredQuery) (queryPlan, error) {
	base := q.From().Base()
	coll, err := d.collection(base.Name())
	if err != nil {
		return queryPlan{}, err
	}
	if len(q.From().Joins()) > 0 {
		return queryPlan{}, fmt.Errorf("%w: collection %q: joins are not supported", dal.ErrNotSupported, coll.Name)
	}
	if len(q.GroupBy()) > 0 || q.Having() != nil {
		return queryPlan{}, fmt.Errorf("%w: collection %q: GROUP BY / HAVING are not supported", dal.ErrNotSupported, coll.Name)
	}
	if len(q.OrderBy()) > 0 {
		return queryPlan{}, fmt.Errorf("%w: collection %q: ORDER BY is not supported (limit is applied after fetch; ordering is not)", dal.ErrNotSupported, coll.Name)
	}
	if q.Offset() > 0 {
		return queryPlan{}, fmt.Errorf("%w: collection %q: Offset is not supported", dal.ErrNotSupported, coll.Name)
	}
	if q.StartFrom() != "" || q.StartAfter() != "" {
		return queryPlan{}, fmt.Errorf("%w: collection %q: cursors are not supported", dal.ErrNotSupported, coll.Name)
	}
	// A requested column projection IS enforceable at this adapter's own
	// boundary, even though the adapter has no schema: unlike a predicate
	// (which must be pushed into the request URL or evaluated against a
	// fetch that already happened), a projection only needs to drop fields
	// from the rows this adapter already holds after extractRows, before
	// they ever leave the adapter — see applyProjection, called from
	// ExecuteQueryToRecordsReader after any residual filtering. Only a bare
	// field reference is something this adapter can enforce that way; a
	// computed/aliased expression is not, and is refused rather than
	// silently ignored, per the Phase 1 HTTP bounds ("if a requested
	// protected predicate/projection cannot be enforced safely, reject it
	// rather than fetching an unrestricted result and claiming
	// enforcement").
	projection, err := columnFieldNames(q.Columns(), coll)
	if err != nil {
		return queryPlan{}, err
	}

	equalities := map[string]string{}
	residual, err := collectEqualities(q.Where(), coll, equalities)
	if err != nil {
		return queryPlan{}, err
	}
	if residual && !coll.ClientSideFilter {
		return queryPlan{}, fmt.Errorf("%w: collection %q: condition cannot be fully pushed into the URL, and this collection does not set ClientSideFilter", dal.ErrNotSupported, coll.Name)
	}
	return queryPlan{collection: coll, equalities: equalities, residual: residual, projection: projection}, nil
}

// columnFieldNames extracts the plain field names q.Columns() requests, or
// nil when none were requested (select every field the response returns).
// Only a bare dal.FieldRef column is enforceable at this adapter's boundary
// (see applyProjection); any other expression this adapter cannot evaluate
// — a computed value, an alias over something other than a plain field —
// fails closed with dal.ErrNotSupported rather than being silently ignored.
func columnFieldNames(columns []dal.Column, coll Collection) ([]string, error) {
	if len(columns) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(columns))
	for _, col := range columns {
		field, ok := col.Expression.(dal.FieldRef)
		if !ok {
			return nil, fmt.Errorf("%w: collection %q: column %s is not a plain field reference and cannot be enforced by this adapter", dal.ErrNotSupported, coll.Name, col.String())
		}
		names = append(names, field.Name())
	}
	return names, nil
}

// applyProjection keeps only fields and keyField in each row, dropping every
// other field before a row ever leaves the adapter — the enforcement point
// for a requested column projection (see columnFieldNames/planQuery).
// keyField is always retained even when not explicitly requested: it
// identifies the row (rowsToRecords needs it to build the record's key), it
// is not a value being redacted or exposed, and every row already carries it
// regardless of what was SELECTed. A requested field absent from a given
// row's live response is left absent in the projected row too — never
// synthesized as a null value — so a caller can still distinguish "this
// field doesn't exist here" from "this field is explicitly null".
func applyProjection(rows []map[string]any, fields []string, keyField string) []map[string]any {
	keep := make(map[string]bool, len(fields)+1)
	for _, f := range fields {
		keep[f] = true
	}
	keep[keyField] = true
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		projected := make(map[string]any, len(keep))
		for k, v := range row {
			if keep[k] {
				projected[k] = v
			}
		}
		out[i] = projected
	}
	return out
}

// collectEqualities walks an AND-only tree of `declaredParamField == constant`
// comparisons, filling equalities and reporting whether any part of the tree
// could NOT be reduced to that shape: an OR group, a non-equality operator, a
// non-constant right-hand side, or a field with no declared Param. It never
// calls the network — this is pure condition-tree inspection, which is what
// makes the fail-closed test in query_test.go able to assert "an unsupported
// condition never triggers an HTTP call" by using a http.RoundTripper that
// fails the test if invoked.
func collectEqualities(cond dal.Condition, coll Collection, equalities map[string]string) (residual bool, err error) {
	switch c := cond.(type) {
	case nil:
		return false, nil
	case dal.GroupCondition:
		if c.Operator() != dal.And {
			return true, nil
		}
		for _, sub := range c.Conditions() {
			r, err := collectEqualities(sub, coll, equalities)
			if err != nil {
				return false, err
			}
			residual = residual || r
		}
		return residual, nil
	case dal.Comparison:
		field, ok := c.Left.(dal.FieldRef)
		if !ok || c.Operator != dal.Equal {
			return true, nil
		}
		constVal, ok := c.Right.(dal.Constant)
		if !ok {
			return true, nil
		}
		if _, declared := coll.Params[field.Name()]; !declared {
			return true, nil
		}
		v, err := stringifyParam(constVal.Value)
		if err != nil {
			return false, err
		}
		if existing, ok := equalities[field.Name()]; ok && existing != v {
			return false, fmt.Errorf("%w: collection %q: conflicting equality constraints on %q", dal.ErrNotSupported, coll.Name, field.Name())
		}
		equalities[field.Name()] = v
		return false, nil
	default:
		return true, nil
	}
}

func stringifyParam(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case time.Time:
		return t.Format(time.RFC3339), nil
	case fmt.Stringer:
		return t.String(), nil
	case nil:
		return "", fmt.Errorf("%w: cannot use a nil value as a parameter", dal.ErrNotSupported)
	default:
		return fmt.Sprintf("%v", t), nil
	}
}

// rowsToRecords converts fetched rows into record.Record values keyed by
// coll.KeyField. When the caller supplied a record factory via
// dal.IQueryBuilder.SelectIntoRecord, each row is unmarshaled into a fresh
// instance from that factory (record.MapToData, the same mechanism Get uses);
// otherwise each record's data is the row's map[string]any directly.
func rowsToRecords(coll Collection, rows []map[string]any, intoRecord func() record.Record) ([]record.Record, error) {
	records := make([]record.Record, 0, len(rows))
	for _, row := range rows {
		idVal, ok := row[coll.KeyField]
		if !ok {
			return nil, fmt.Errorf("dalgo2http: collection %q: response row is missing key field %q", coll.Name, coll.KeyField)
		}
		key := record.NewKeyWithID(coll.Name, fmt.Sprintf("%v", idVal))
		var rec record.Record
		if template := intoRecord(); template != nil {
			if err := record.MapToData(template.Data(), row); err != nil {
				return nil, fmt.Errorf("dalgo2http: collection %q: %w", coll.Name, err)
			}
			rec = record.NewRecordWithData(key, template.Data())
		} else {
			rec = record.NewRecordWithData(key, row)
		}
		rec.SetError(nil)
		records = append(records, rec)
	}
	return records, nil
}
