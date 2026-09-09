package dalgo2http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// maxBodyBytes bounds how much of a response body dalgo2http will read, so a
// misbehaving or malicious endpoint cannot exhaust memory.
const maxBodyBytes = 10 << 20 // 10 MiB

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
// variable simply means the header is not sent).
//
// A network error, a timeout, or a 5xx/429 response is reported wrapping
// ErrUpstream — the class fetchRows treats as eligible for snapshot
// fallback. A 4xx response is reported wrapping ErrUpstreamClient instead:
// that is a caller/config error (a bad parameter, most often), and serving a
// stale snapshot for it would mask the bug rather than surface it, so it is
// never fallback-eligible.
func doLiveFetch(ctx context.Context, client *http.Client, coll Collection, rawURL string) (body []byte, statusCode int, err error) {
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
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: collection %q: %v", ErrUpstream, coll.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%w: collection %q: read response body: %v", ErrUpstream, coll.Name, err)
	}
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return b, resp.StatusCode, fmt.Errorf("%w: collection %q: status %d", ErrUpstream, coll.Name, resp.StatusCode)
	case resp.StatusCode >= 400:
		return b, resp.StatusCode, fmt.Errorf("%w: collection %q: status %d", ErrUpstreamClient, coll.Name, resp.StatusCode)
	}
	return b, resp.StatusCode, nil
}
