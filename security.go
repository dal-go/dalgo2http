package dalgo2http

import (
	"context"
	"fmt"
	"net"
	"net/http"
)

// insecureLoopbackContextKey carries, per outgoing request, whether the
// collection being fetched set Collection.InsecureAllowLoopback — read by
// guardedDialContext to decide whether a loopback target is permitted for
// THIS one dial. It is unexported and has no other purpose; see
// Collection.InsecureAllowLoopback's doc comment (descriptor.go) for why
// this exists at all (httptest-based tests). A context value, not a
// Config-wide setting, because one Config can mix a real HTTPS collection
// with a test-only loopback one, and each must be dialed correctly.
type insecureLoopbackContextKey struct{}

func contextWithInsecureLoopback(ctx context.Context, allow bool) context.Context {
	if !allow {
		return ctx
	}
	return context.WithValue(ctx, insecureLoopbackContextKey{}, true)
}

func insecureLoopbackFrom(ctx context.Context) bool {
	allow, _ := ctx.Value(insecureLoopbackContextKey{}).(bool)
	return allow
}

// lookupIPAddr resolves host to its candidate IP addresses. A package var so
// tests can inject a fake resolver — proving guardedDialContext validates
// the resolved address and dials ONLY that literal IP, never re-resolving at
// dial time (the DNS-rebinding defense the Phase 1 HTTP bounds require:
// "Deny ... addresses, including DNS resolution/rebinding ... to them").
var lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// isBlockedIP reports whether ip must never be dialed: private (RFC1918 +
// IPv6 ULA fc00::/7 — Go's IsPrivate covers both), loopback (127.0.0.0/8,
// ::1), link-local (169.254.0.0/16 — which includes the cloud metadata
// address 169.254.169.254 — and fe80::/10), multicast, or unspecified
// (0.0.0.0, ::). allowLoopback — set only via Collection.InsecureAllowLoopback
// for a request to that one collection — relaxes ONLY the loopback check;
// every other class stays blocked even then, so the opt-in cannot be used to
// reach anything but an httptest server on loopback.
//
// ip.To4() normalizes an IPv4-mapped IPv6 address (::ffff:a.b.c.d) to plain
// IPv4 before classification, so e.g. ::ffff:127.0.0.1 is recognized as
// loopback exactly like 127.0.0.1 — explicit handling, not left to whichever
// net.IP method happens to unmap it internally.
func isBlockedIP(ip net.IP, allowLoopback bool) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() {
		return !allowLoopback
	}
	switch {
	case ip.IsPrivate(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(),
		ip.IsMulticast(),
		ip.IsUnspecified():
		return true
	}
	return false
}

// guardedDialContext is the http.Transport.DialContext newDefaultClient
// installs: it resolves addr's host at most once, rejects every disallowed
// candidate address (see isBlockedIP), and dials ONLY the first validated IP
// literal — never the original hostname again — so a second DNS lookup
// happening to return a different (attacker-controlled or rebound) address
// at dial time cannot redirect the connection. This is the guarded dialer
// the Phase 1 HTTP bounds require: "implement a guarded dialer ... that
// resolves the host, rejects every [disallowed class] ... and dials ONLY the
// validated IP."
func guardedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("dalgo2http: %w: invalid dial address %q: %v", ErrAddressBlocked, addr, err)
	}
	allowLoopback := insecureLoopbackFrom(ctx)

	// addr's host may already be an IP literal (the common case: a
	// descriptor's declared host, or an httptest server's 127.0.0.1) — no
	// resolution needed, and so nothing to rebind between resolve and dial.
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip, allowLoopback) {
			return nil, fmt.Errorf("dalgo2http: %w: %s", ErrAddressBlocked, ip)
		}
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}

	addrs, err := lookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("dalgo2http: resolve %q: %w", host, err)
	}
	for _, candidate := range addrs {
		if isBlockedIP(candidate.IP, allowLoopback) {
			continue
		}
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
	}
	return nil, fmt.Errorf("dalgo2http: %w: every address %q resolved to is blocked", ErrAddressBlocked, host)
}

// denyRedirect is the default http.Client.CheckRedirect: a redirect is
// always refused ("Redirects are disabled for the demo" — no config knob
// relaxes this in this package, per the stream brief).
func denyRedirect(*http.Request, []*http.Request) error {
	return ErrRedirectNotAllowed
}

// newDefaultClient builds the *http.Client NewDB installs when Config.Client
// is nil: a guarded DialContext (see guardedDialContext) and redirects
// disabled (see denyRedirect). A caller who supplies their own Config.Client
// is responsible for equivalent protections on it — this default is what
// every collection gets unless a caller opts out by supplying a Client of
// their own.
func newDefaultClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: guardedDialContext,
		},
		CheckRedirect: denyRedirect,
	}
}
