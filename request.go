package dalgo2http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// maxBodyBytes bounds how much of a response body dalgo2http will read, so a
// misbehaving or malicious endpoint cannot exhaust memory. Set to 2 MiB per
// the Phase 1 HTTP bounds ("Bound response bytes to 2 MiB"); a body over this
// limit fails explicitly with ErrResponseTooLarge (see doLiveFetch) rather
// than being silently truncated by a LimitReader and then failing JSON
// decoding downstream with a misleading error.
const maxBodyBytes = 2 << 20 // 2 MiB

// buildURL renders coll.URLTemplate with params. A {name} placeholder is
// substituted using the escaping its declared Param.Location calls for
// (url.PathEscape for "path", url.QueryEscape for "query" — this matters
// because a "query" Param can be embedded directly inside the template's
// literal query string, e.g. "...?symbols={to}", not just appended). Any
// declared query Param whose name never appears as a placeholder is instead
// appended as an extra "?name=value" query parameter when params supplies a
// value for it.
//
// It never fetches and never treats an unresolved parameter permissively: a
// path placeholder with no value in params fails with ErrMissingParam.
func buildURL(coll Collection, params map[string]string) (string, error) {
	used := map[string]bool{}
	var buildErr error
	rendered := placeholderRe.ReplaceAllStringFunc(coll.URLTemplate, func(m string) string {
		name := m[1 : len(m)-1]
		p, declared := coll.Params[name]
		if !declared {
			// validate() rejects this at config time; defensive only.
			buildErr = fmt.Errorf("%w: collection %q: urlTemplate references undeclared parameter %q", ErrInvalidConfig, coll.Name, name)
			return m
		}
		v, ok := params[name]
		if !ok {
			buildErr = fmt.Errorf("%w: collection %q: missing value for required parameter %q", ErrMissingParam, coll.Name, name)
			return m
		}
		used[name] = true
		if p.Location == ParamQuery {
			return url.QueryEscape(v)
		}
		return url.PathEscape(v)
	})
	if buildErr != nil {
		return "", buildErr
	}
	parsed, err := url.Parse(rendered)
	if err != nil {
		return "", fmt.Errorf("dalgo2http: collection %q: parse URL: %w", coll.Name, err)
	}
	q := parsed.Query()
	for name, p := range coll.Params {
		if p.Location != ParamQuery || used[name] {
			continue
		}
		if v, ok := params[name]; ok {
			q.Set(name, v)
		}
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// doLiveFetch issues one GET request for rawURL, applying coll.Timeout (if
// set) and coll.Headers (resolved from the environment; an unset or empty
// variable simply means the header is not sent). ctx carries
// coll.InsecureAllowLoopback (see security.go's contextWithInsecureLoopback)
// to the default client's guarded dialer, if that default client is the one
// in use.
//
// A network error, or a timeout, or a 5xx/429 response is reported wrapping
// ErrUpstream — the class fetchRows treats as eligible for snapshot
// fallback. A 4xx response, a refused redirect (ErrRedirectNotAllowed), or a
// blocked address (ErrAddressBlocked) is reported wrapping ErrUpstreamClient
// instead: each is a caller/config error, and serving a stale snapshot for
// one would mask the problem rather than surface it, so none is ever
// fallback-eligible.
func doLiveFetch(ctx context.Context, client *http.Client, coll Collection, rawURL string) (body []byte, statusCode int, err error) {
	ctx = contextWithInsecureLoopback(ctx, coll.InsecureAllowLoopback)
	if coll.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, coll.Timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, string(MethodGET), rawURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("dalgo2http: collection %q: build request: %w", coll.Name, err)
	}
	for header, envVar := range coll.Headers {
		if v := os.Getenv(envVar); v != "" {
			req.Header.Set(header, v)
		}
	}
	if client == nil {
		client = newDefaultClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirectNotAllowed) || errors.Is(err, ErrAddressBlocked) {
			// Both %w verbs matter here (Go 1.20+ multi-error wrapping): a
			// caller checking errors.Is(returnedErr, ErrRedirectNotAllowed)
			// or errors.Is(returnedErr, ErrAddressBlocked) must still find
			// it through this wrapping, not just ErrUpstreamClient.
			return nil, 0, fmt.Errorf("%w: collection %q: %w", ErrUpstreamClient, coll.Name, err)
		}
		return nil, 0, fmt.Errorf("%w: collection %q: %v", ErrUpstream, coll.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%w: collection %q: read response body: %v", ErrUpstream, coll.Name, err)
	}
	if len(b) > maxBodyBytes {
		return nil, resp.StatusCode, fmt.Errorf("%w: collection %q: response exceeds %d bytes", ErrResponseTooLarge, coll.Name, maxBodyBytes)
	}
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return b, resp.StatusCode, fmt.Errorf("%w: collection %q: status %d", ErrUpstream, coll.Name, resp.StatusCode)
	case resp.StatusCode >= 400:
		return b, resp.StatusCode, fmt.Errorf("%w: collection %q: status %d", ErrUpstreamClient, coll.Name, resp.StatusCode)
	}
	return b, resp.StatusCode, nil
}
