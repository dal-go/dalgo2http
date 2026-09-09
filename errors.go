package dalgo2http

import "errors"

// Sentinel errors this adapter returns. All are typed so callers can use
// errors.Is; several also wrap dal.ErrNotSupported (see descriptor.go and
// query.go) so a caller that only checks for dal.ErrNotSupported still sees
// the fail-closed refusal.
var (
	// ErrInvalidConfig indicates a Collection or Config value failed
	// validation (LoadConfigYAML, LoadConfigJSON or NewDB).
	ErrInvalidConfig = errors.New("dalgo2http: invalid config")

	// ErrUnknownCollection indicates a record.Key or query named a collection
	// that is not declared in the Config.
	ErrUnknownCollection = errors.New("dalgo2http: unknown collection")

	// ErrMissingParam indicates a request could not be built because a
	// required URL template parameter had no value.
	ErrMissingParam = errors.New("dalgo2http: missing required parameter")

	// ErrUpstream indicates a live request failed for a reason snapshot
	// fallback treats as transient: a network error, a timeout, or a 5xx /
	// 429 response.
	ErrUpstream = errors.New("dalgo2http: upstream request failed")

	// ErrUpstreamClient indicates a live request received a 4xx response.
	// This is treated as a caller/config error, not a transient failure, so
	// it never triggers snapshot fallback.
	ErrUpstreamClient = errors.New("dalgo2http: upstream rejected the request")

	// ErrSnapshotMiss indicates no recorded snapshot exists for a request.
	ErrSnapshotMiss = errors.New("dalgo2http: no snapshot available")

	// ErrRedirectNotAllowed indicates a live request received a redirect
	// response. Redirects are disabled by default (Phase 1 HTTP bounds:
	// "Redirects are disabled for the demo") — a redirecting endpoint is a
	// caller/config problem (the descriptor should target the final host
	// directly), never a transient failure, so doLiveFetch reports it
	// wrapping ErrUpstreamClient, never ErrUpstream: it is never
	// fallback-eligible.
	ErrRedirectNotAllowed = errors.New("dalgo2http: redirects are not allowed")

	// ErrResponseTooLarge indicates a live response body exceeded
	// maxBodyBytes. Reported explicitly rather than silently truncated
	// (which would otherwise fail JSON decoding downstream with a
	// misleading "unexpected end of JSON input").
	ErrResponseTooLarge = errors.New("dalgo2http: response body exceeds the size limit")

	// ErrAddressBlocked indicates the guarded dialer refused to connect to a
	// resolved address because it is private, loopback, link-local or a
	// metadata-service address (Phase 1 HTTP bounds: "Deny private,
	// loopback, link-local and metadata-service addresses"). A caller/config
	// problem, not a transient failure.
	ErrAddressBlocked = errors.New("dalgo2http: target address is blocked (private/loopback/link-local/metadata)")
)
