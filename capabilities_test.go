package dalgo2http

import (
	"testing"

	"github.com/dal-go/dalgo/dal"
)

func TestCapabilities(t *testing.T) {
	coll := countriesCollection("https://example.invalid")
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	provider, ok := dal.As[CapabilitiesProvider](db)
	if !ok {
		t.Fatalf("dal.As[CapabilitiesProvider] did not find the capability")
	}
	caps, err := provider.Capabilities("countries")
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if len(caps.Fields) != 1 || caps.Fields[0] != "name" {
		t.Fatalf("Fields = %v, want [name]", caps.Fields)
	}
	if !caps.SupportsGet {
		t.Fatalf("SupportsGet = false, want true (KeyField %q is a declared param)", coll.KeyField)
	}
	if caps.ClientSideFilter {
		t.Fatalf("ClientSideFilter = true, want false")
	}

	if _, err := provider.Capabilities("no-such-collection"); err == nil {
		t.Fatalf("Capabilities(unknown) = nil error, want ErrUnknownCollection")
	}
}

func TestCapabilities_SupportsGetFalse(t *testing.T) {
	coll := Collection{
		Name:        "fx",
		URLTemplate: "https://example.invalid/latest?base=USD&symbols={to}",
		KeyField:    "base",
		Params:      map[string]Param{"to": {Location: ParamQuery}},
	}
	db, err := NewDB(Config{Collections: []Collection{coll}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	provider, _ := dal.As[CapabilitiesProvider](db)
	caps, err := provider.Capabilities("fx")
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if caps.SupportsGet {
		t.Fatalf("SupportsGet = true, want false (KeyField %q is not a declared param)", coll.KeyField)
	}
}
