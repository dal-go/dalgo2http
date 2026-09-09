package dalgo2http

import (
	"net/http"
	"testing"
)

// noCallTransport fails the test if RoundTrip is ever invoked. It is used to
// prove the fail-closed pushdown rule: an unsupported condition must never
// reach the network.
type noCallTransport struct{ t *testing.T }

func (n noCallTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n.t.Fatalf("dalgo2http made an HTTP request when it should have failed closed: %s %s", req.Method, req.URL)
	return nil, nil
}

func noCallClient(t *testing.T) *http.Client {
	return &http.Client{Transport: noCallTransport{t: t}}
}
