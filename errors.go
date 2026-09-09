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
)
