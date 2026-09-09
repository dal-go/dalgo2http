package dalgo2http

import (
	"sort"

	"github.com/dal-go/dalgo/dal"
)

// Capabilities describes what a policy layer may push down to one
// collection, so it can decide whether a query is answerable before
// executing it (see query.go for the fail-closed enforcement this describes).
type Capabilities struct {
	Collection string

	// Fields lists the declared parameter fields an equality condition may
	// target and have pushed into the request.
	Fields []string

	// Operators lists the dal.Operator values usable against Fields.
	// Equality is always supported (it is the only operator ever pushed into
	// the URL); the ordering/inequality operators appear only when
	// ClientSideFilter is set, since those are evaluated in memory after a
	// broader fetch, never pushed down.
	Operators []dal.Operator

	// SupportsGet reports whether dal.DB.Get/Exists work for this
	// collection: true only when KeyField is itself a declared Param.
	SupportsGet bool

	// ClientSideFilter mirrors Collection.ClientSideFilter.
	ClientSideFilter bool
}

// CapabilitiesProvider is the optional capability a dalgo2http database
// implements. dal.NewDB decorates the Backend passed to it, so a plain type
// assertion on a dal.DB does not see Capabilities; use
// dal.As[CapabilitiesProvider](db) instead, per the convention dal.As
// documents (the same one dbschema.SchemaReader and ddl.SchemaModifier use).
type CapabilitiesProvider interface {
	Capabilities(collection string) (Capabilities, error)
}

var _ CapabilitiesProvider = (*database)(nil)

// Capabilities implements CapabilitiesProvider.
func (d *database) Capabilities(collection string) (Capabilities, error) {
	coll, err := d.collection(collection)
	if err != nil {
		return Capabilities{}, err
	}
	fields := make([]string, 0, len(coll.Params))
	for name := range coll.Params {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	ops := []dal.Operator{dal.Equal}
	if coll.ClientSideFilter {
		ops = append(ops, dal.GreaterThen, dal.GreaterOrEqual, dal.LessThen, dal.LessOrEqual, dal.In)
	}
	_, supportsGet := coll.Params[coll.KeyField]
	return Capabilities{
		Collection:       coll.Name,
		Fields:           fields,
		Operators:        ops,
		SupportsGet:      supportsGet,
		ClientSideFilter: coll.ClientSideFilter,
	}, nil
}
