package dalgo2http

import (
	"context"
	"testing"
)

func TestRecorder(t *testing.T) {
	r := NewRecorder()
	if _, seen := r.Last(); seen {
		t.Fatalf("Last() reported seen=true before any observation")
	}
	ctx := r.WithContext(context.Background())
	obs := provenanceObserverFrom(ctx)
	if obs == nil {
		t.Fatalf("provenanceObserverFrom(ctx) = nil")
	}
	want := Provenance{Collection: "countries", Source: SourceLive, StatusCode: 200}
	obs(want)
	got, seen := r.Last()
	if !seen || got != want {
		t.Fatalf("Last() = (%+v, %v), want (%+v, true)", got, seen, want)
	}
}

func TestProvenanceObserverFrom_NoObserver(t *testing.T) {
	if obs := provenanceObserverFrom(context.Background()); obs != nil {
		t.Fatalf("provenanceObserverFrom(plain context) = non-nil, want nil")
	}
}
