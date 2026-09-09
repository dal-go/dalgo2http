package dalgo2http

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// snapshotEnvelope is the on-disk shape of one recorded fixture: the raw
// upstream response body plus the provenance metadata a live call would have
// produced, so a snapshot answer's Provenance is exact rather than
// synthesized at read time.
type snapshotEnvelope struct {
	FetchedAt  time.Time       `json:"fetchedAt"`
	StatusCode int             `json:"statusCode"`
	Body       json.RawMessage `json:"body"`
}

type snapshotMeta struct {
	StatusCode int
	FetchedAt  time.Time
}

// SnapshotKey returns the deterministic file name a recorded snapshot for
// collection with the given request params is stored under: the collection
// name followed by its params sorted by key, so the same logical request
// always resolves to the same file regardless of map iteration order.
func SnapshotKey(collection string, params map[string]string) string {
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(url.PathEscape(collection))
	for _, k := range names {
		b.WriteByte('_')
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(params[k]))
	}
	b.WriteString(".json")
	return b.String()
}

func readSnapshot(fsys fs.FS, key string) ([]byte, snapshotMeta, error) {
	if fsys == nil {
		return nil, snapshotMeta{}, fmt.Errorf("%w: no snapshot store configured", ErrSnapshotMiss)
	}
	data, err := fs.ReadFile(fsys, key)
	if err != nil {
		return nil, snapshotMeta{}, fmt.Errorf("%w: %s: %v", ErrSnapshotMiss, key, err)
	}
	var env snapshotEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, snapshotMeta{}, fmt.Errorf("dalgo2http: corrupt snapshot %s: %w", key, err)
	}
	return env.Body, snapshotMeta{StatusCode: env.StatusCode, FetchedAt: env.FetchedAt}, nil
}

// Record performs one live GET for coll with params — ignoring Config.Mode
// entirely, it always calls the network — and writes the response as a
// snapshot fixture file under dir, named by SnapshotKey. It is a
// development/test tool for building fixtures (see examples/), not part of
// request-serving at runtime.
func Record(ctx context.Context, client *http.Client, coll Collection, params map[string]string, dir string) (path string, err error) {
	rawURL, err := buildURL(coll, params)
	if err != nil {
		return "", err
	}
	body, status, err := doLiveFetch(ctx, client, coll, rawURL)
	if err != nil {
		return "", err
	}
	env := snapshotEnvelope{FetchedAt: time.Now().UTC(), StatusCode: status, Body: json.RawMessage(body)}
	encoded, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", fmt.Errorf("dalgo2http: encode snapshot: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("dalgo2http: create snapshot dir %s: %w", dir, err)
	}
	path = filepath.Join(dir, SnapshotKey(coll.Name, params))
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return "", fmt.Errorf("dalgo2http: write snapshot %s: %w", path, err)
	}
	return path, nil
}
