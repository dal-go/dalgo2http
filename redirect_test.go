package dalgo2http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dal-go/record"
)

// TestDoLiveFetch_RedirectRefused proves the Phase 1 HTTP bounds' "Redirects
// are disabled for the demo": a 302 response is not followed, is reported
// wrapping ErrRedirectNotAllowed (and, per the non-transient classification
// documented on ErrRedirectNotAllowed, ErrUpstreamClient too — never
// ErrUpstream, so ModeLiveThenSnapshot must not treat it as fallback-eligible).
func TestDoLiveFetch_RedirectRefused(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("the redirect target must never be reached once CheckRedirect refuses")
	}))
	defer target.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirecting.Close()

	coll := Collection{Name: "redir", URLTemplate: redirecting.URL, KeyField: "id", InsecureAllowLoopback: true}
	client := redirecting.Client()
	client.CheckRedirect = denyRedirect // same guard newDefaultClient installs
	_, _, err := doLiveFetch(context.Background(), client, coll, redirecting.URL)
	if !errors.Is(err, ErrRedirectNotAllowed) {
		t.Fatalf("doLiveFetch() err = %v, want it to wrap ErrRedirectNotAllowed", err)
	}
	if !errors.Is(err, ErrUpstreamClient) {
		t.Fatalf("doLiveFetch() err = %v, want it to wrap ErrUpstreamClient (not transient — never fallback-eligible)", err)
	}
	if errors.Is(err, ErrUpstream) {
		t.Fatalf("doLiveFetch() err = %v must NOT wrap ErrUpstream (that would make a refused redirect snapshot-fallback-eligible)", err)
	}
}

// TestNewDefaultClient_RedirectRefused proves the SAME refusal using the
// package's own default client (see security.go's newDefaultClient), not a
// test-supplied one with CheckRedirect bolted on — end-to-end proof that
// NewDB's default is redirect-safe with no config knob needed to make it so.
func TestNewDefaultClient_RedirectRefused(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("the redirect target must never be reached once CheckRedirect refuses")
	}))
	defer target.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirecting.Close()

	coll := countriesCollection(redirecting.URL) // InsecureAllowLoopback: true
	db, err := NewDB(Config{Collections: []Collection{coll}, Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	target2 := map[string]any{}
	rec := record.NewRecordWithData(record.NewKeyWithID("countries", "France"), &target2)
	err = db.Get(context.Background(), rec)
	if !errors.Is(err, ErrRedirectNotAllowed) {
		t.Fatalf("Get() err = %v, want it to wrap ErrRedirectNotAllowed", err)
	}
}

// TestFetchRows_RedirectDoesNotTriggerSnapshotFallback proves the same rule
// end to end through fetchRows/ModeLiveThenSnapshot: even with a matching
// snapshot recorded and available, a refused redirect fails outright rather
// than silently serving stale recorded data — a redirecting endpoint is a
// config problem to surface, not something a fixture should paper over.
func TestFetchRows_RedirectDoesNotTriggerSnapshotFallback(t *testing.T) {
	// Record a real snapshot first, from a server that answers directly (no
	// redirect), matching TestRecord_And_SnapshotFallback's own pattern.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR"}}`))
	}))
	coll := countriesCollection(up.URL)
	dir := t.TempDir()
	if _, err := Record(context.Background(), up.Client(), coll, map[string]string{"name": "France"}, dir); err != nil {
		t.Fatalf("Record: %v", err)
	}
	up.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("the redirect target must never be reached")
	}))
	defer target.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusMovedPermanently)
	}))
	defer redirecting.Close()

	redirColl := countriesCollection(redirecting.URL)
	db, err := NewDB(Config{
		Collections: []Collection{redirColl},
		Snapshots:   os.DirFS(dir),
		Mode:        ModeLiveThenSnapshot,
	})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	target2 := map[string]any{}
	rec := record.NewRecordWithData(record.NewKeyWithID("countries", "France"), &target2)
	err = db.Get(context.Background(), rec)
	if err == nil {
		t.Fatalf("Get() through a refused redirect = nil error, want it refused (no snapshot fallback despite a matching recorded snapshot)")
	}
	if !errors.Is(err, ErrRedirectNotAllowed) {
		t.Fatalf("Get() err = %v, want it to wrap ErrRedirectNotAllowed", err)
	}
}
