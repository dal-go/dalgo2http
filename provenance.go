package dalgo2http

import (
	"context"
	"sync"
	"time"
)

// Source says whether a result came from a live request or a recorded
// snapshot.
type Source string

const (
	SourceLive     Source = "live"
	SourceSnapshot Source = "snapshot"
)

// Provenance describes where one collection read's rows came from.
type Provenance struct {
	Collection string
	Source     Source
	StatusCode int
	FetchedAt  time.Time
}

// Observer is called once per collection read (Get, Exists, or a query) with
// that read's Provenance.
type Observer func(Provenance)

type provenanceContextKey struct{}

// ContextWithProvenanceObserver returns a context that, when passed to a
// dalgo2http database method, calls obs with that call's Provenance before
// the method returns. See also Recorder for a ready-made Observer that keeps
// the last value for the caller to read back afterwards.
func ContextWithProvenanceObserver(ctx context.Context, obs Observer) context.Context {
	return context.WithValue(ctx, provenanceContextKey{}, obs)
}

func provenanceObserverFrom(ctx context.Context) Observer {
	obs, _ := ctx.Value(provenanceContextKey{}).(Observer)
	return obs
}

// Recorder captures the Provenance of the most recent call made with its
// context, so a caller that just wants "was that live or snapshot?" back
// after a call does not have to write its own Observer.
type Recorder struct {
	mu   sync.Mutex
	last Provenance
	seen bool
}

// NewRecorder creates a Recorder with no observed Provenance yet.
func NewRecorder() *Recorder { return &Recorder{} }

// WithContext returns ctx wired so calls made with it are observed by r.
func (r *Recorder) WithContext(ctx context.Context) context.Context {
	return ContextWithProvenanceObserver(ctx, r.observe)
}

func (r *Recorder) observe(p Provenance) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = p
	r.seen = true
}

// Last returns the most recently observed Provenance, and whether any call
// has been observed yet.
func (r *Recorder) Last() (Provenance, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last, r.seen
}
