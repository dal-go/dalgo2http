package dalgo2http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
)

// TestIsBlockedIP proves isBlockedIP's exhaustive address-class coverage —
// the Phase 1 HTTP bounds' "Deny private, loopback, link-local and
// metadata-service addresses" — as a pure, deterministic table (no network
// I/O needed to exercise every class).
func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		name          string
		ip            string
		allowLoopback bool
		want          bool
	}{
		{name: "loopback v4 blocked by default", ip: "127.0.0.1", want: true},
		{name: "loopback v6 blocked by default", ip: "::1", want: true},
		{name: "loopback v4 allowed with opt-in", ip: "127.0.0.1", allowLoopback: true, want: false},
		{name: "loopback v6 allowed with opt-in", ip: "::1", allowLoopback: true, want: false},
		{name: "loopback range other than .1 allowed with opt-in", ip: "127.5.6.7", allowLoopback: true, want: false},

		{name: "RFC1918 10/8", ip: "10.1.2.3", want: true},
		{name: "RFC1918 172.16/12", ip: "172.16.5.4", want: true},
		{name: "RFC1918 192.168/16", ip: "192.168.1.1", want: true},
		{name: "RFC1918 stays blocked even with loopback opt-in", ip: "10.1.2.3", allowLoopback: true, want: true},

		{name: "link-local v4 169.254/16", ip: "169.254.1.1", want: true},
		{name: "cloud metadata address", ip: "169.254.169.254", want: true},
		{name: "metadata address stays blocked even with loopback opt-in", ip: "169.254.169.254", allowLoopback: true, want: true},
		{name: "link-local v6 fe80::/10", ip: "fe80::1", want: true},

		{name: "IPv6 ULA fc00::/7", ip: "fd12:3456:789a::1", want: true},

		{name: "unspecified v4", ip: "0.0.0.0", want: true},
		{name: "unspecified v6", ip: "::", want: true},

		{name: "multicast v4", ip: "224.0.0.1", want: true},
		{name: "multicast v6", ip: "ff02::1", want: true},

		{name: "IPv4-mapped IPv6 loopback blocked by default", ip: "::ffff:127.0.0.1", want: true},
		{name: "IPv4-mapped IPv6 loopback allowed with opt-in", ip: "::ffff:127.0.0.1", allowLoopback: true, want: false},
		{name: "IPv4-mapped IPv6 private", ip: "::ffff:10.0.0.1", want: true},
		{name: "IPv4-mapped IPv6 metadata", ip: "::ffff:169.254.169.254", want: true},

		{name: "public v4 allowed", ip: "93.184.216.34", want: false},
		{name: "public v6 allowed", ip: "2606:2800:220:1:248:1893:25c8:1946", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) = nil", tc.ip)
			}
			if got := isBlockedIP(ip, tc.allowLoopback); got != tc.want {
				t.Errorf("isBlockedIP(%s, allowLoopback=%v) = %v, want %v", tc.ip, tc.allowLoopback, got, tc.want)
			}
		})
	}
}

// TestGuardedDialContext_BlocksLiteralIP proves the guard rejects a
// blocked-class dial target given as a literal IP:port — the common,
// realistic shape (a descriptor's declared https:// host resolving, or
// being, an internal/metadata address) — entirely offline: guardedDialContext
// refuses before any socket is opened, so this needs no network access and
// cannot hang.
func TestGuardedDialContext_BlocksLiteralIP(t *testing.T) {
	_, err := guardedDialContext(context.Background(), "tcp", "169.254.169.254:80")
	if !errors.Is(err, ErrAddressBlocked) {
		t.Fatalf("guardedDialContext(metadata IP) err = %v, want ErrAddressBlocked", err)
	}
}

func TestGuardedDialContext_BlocksLoopbackByDefault(t *testing.T) {
	_, err := guardedDialContext(context.Background(), "tcp", "127.0.0.1:80")
	if !errors.Is(err, ErrAddressBlocked) {
		t.Fatalf("guardedDialContext(loopback, no opt-in) err = %v, want ErrAddressBlocked", err)
	}
}

// TestGuardedDialContext_ResolvesOnceAndDialsOnlyTheValidatedIP proves the
// DNS-rebinding defense the Phase 1 HTTP bounds require: lookupIPAddr is
// swapped for a fake resolver that returns a blocked address first and an
// allowed one second (a plausible multi-A-record shape), and records how
// many times it is called. guardedDialContext must call it exactly once (no
// second, later resolution that a rebound record could exploit) and must
// dial the validated address literally — proven here by pointing the second
// "allowed" address at a real, local listener and confirming the connection
// actually reaches it.
func TestGuardedDialContext_ResolvesOnceAndDialsOnlyTheValidatedIP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	calls := 0
	origLookup := lookupIPAddr
	defer func() { lookupIPAddr = origLookup }()
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		calls++
		return []net.IPAddr{
			{IP: net.ParseIP("169.254.169.254")}, // blocked: must be skipped
			{IP: net.ParseIP("127.0.0.1")},       // allowed only via the test's loopback opt-in
		}, nil
	}

	ctx := contextWithInsecureLoopback(context.Background(), true)
	// host is not an IP literal, so guardedDialContext must call lookupIPAddr
	// to resolve it — that is exactly the call this test counts.
	addr := net.JoinHostPort("test.invalid.example", strconv.Itoa(port))
	conn, err := guardedDialContext(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("guardedDialContext: %v", err)
	}
	_ = conn.Close()

	if calls != 1 {
		t.Errorf("lookupIPAddr called %d times, want exactly 1 (no re-resolution window for rebinding)", calls)
	}
}

// TestGuardedDialContext_AllBlockedFails proves that when every resolved
// candidate is blocked, the dial fails closed rather than falling back to
// one of them.
func TestGuardedDialContext_AllBlockedFails(t *testing.T) {
	origLookup := lookupIPAddr
	defer func() { lookupIPAddr = origLookup }()
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}, {IP: net.ParseIP("169.254.169.254")}}, nil
	}
	_, err := guardedDialContext(context.Background(), "tcp", "internal.example.test:443")
	if !errors.Is(err, ErrAddressBlocked) {
		t.Fatalf("guardedDialContext(all blocked) err = %v, want ErrAddressBlocked", err)
	}
}

// TestNewDefaultClient_RealLoopbackRequestSucceeds proves the guarded
// default client (not a test-supplied one) actually completes a real HTTP
// round trip against an httptest.Server when InsecureAllowLoopback is set —
// end-to-end proof the guard's loopback opt-in is not merely theoretical.
func TestNewDefaultClient_RealLoopbackRequestSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR"}}`))
	}))
	defer srv.Close()

	coll := countriesCollection(srv.URL) // sets InsecureAllowLoopback: true
	// Client deliberately omitted: this test wants NewDB's own default
	// guarded client (see security.go's newDefaultClient), not a
	// test-supplied one, to prove the guard's loopback opt-in genuinely
	// permits a real round trip end to end.
	db, err := NewDB(Config{Collections: []Collection{coll}, Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := dal.NewQueryBuilder(dal.From(dal.NewRootCollectionRef("countries", ""))).
		Where(dal.WhereField("name", dal.Equal, "France")).
		SelectIntoRecord(nil)
	reader, err := db.ExecuteQueryToRecordsReader(context.Background(), q)
	if err != nil {
		t.Fatalf("ExecuteQueryToRecordsReader (default client, guarded, loopback opt-in): %v", err)
	}
	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("reader.Next: %v", err)
	}
	data, ok := rec.Data().(map[string]any)
	if !ok || data["currency"] != "EUR" {
		t.Fatalf("record data = %#v, want currency=EUR", rec.Data())
	}
}

// TestNewDefaultClient_MetadataAddressRejected proves the guarded default
// client refuses a real https:// descriptor whose host IS the cloud metadata
// address — no InsecureAllowLoopback opt-in exists for it, and none should:
// this runs fully offline (the guard refuses before any socket opens, so no
// network access is needed and the test cannot hang).
func TestNewDefaultClient_MetadataAddressRejected(t *testing.T) {
	coll := Collection{
		Name:        "metadata",
		URLTemplate: "https://169.254.169.254/latest/meta-data/{path}",
		KeyField:    "path",
		Params:      map[string]Param{"path": {Location: ParamPath}},
	}
	db, err := NewDB(Config{Collections: []Collection{coll}, Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	target := map[string]any{}
	rec := record.NewRecordWithData(record.NewKeyWithID("metadata", "hostname"), &target)
	err = db.Get(context.Background(), rec)
	if err == nil {
		t.Fatalf("Get() against the metadata address = nil error, want it refused")
	}
	if !errors.Is(err, ErrUpstreamClient) {
		t.Fatalf("Get() err = %v, want it to wrap ErrUpstreamClient (the address guard, not a transient failure)", err)
	}
}
