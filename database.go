package dalgo2http

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo/recordset"
	"github.com/dal-go/record"
)

// database is the dal.Backend this package implements. It is read-only: see
// transaction.go for the WriteSession stubs every mutation returns
// dal.ErrNotSupported from.
type database struct {
	// dal.NoConcurrency: nothing here prevents concurrent open connections
	// (there is no connection at all — every call is a stateless HTTP
	// request), but adopting the conservative default costs nothing and
	// matches dalgo2fs, the adapter this package's shape follows most
	// closely. Revisit if a concurrency-sensitive consumer needs otherwise.
	dal.NoConcurrency

	cfg         Config
	collections map[string]Collection
}

var _ dal.Backend = (*database)(nil)

// NewDB validates cfg and returns a dal.DB backed by it. cfg.Client defaults
// to a guarded client (see security.go's newDefaultClient — a custom
// DialContext rejecting private/loopback/link-local/metadata addresses, and
// redirects disabled) when nil, per the Phase 1 HTTP bounds; cfg.Mode
// defaults to ModeLiveThenSnapshot when empty.
func NewDB(cfg Config) (dal.DB, error) {
	index, err := validateConfig(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Client == nil {
		cfg.Client = newDefaultClient()
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeLiveThenSnapshot
	}
	return dal.NewDB(&database{cfg: cfg, collections: index}), nil
}

func (d *database) ID() string           { return "dalgo2http" }
func (d *database) Adapter() dal.Adapter { return nil }
func (d *database) Schema() dal.Schema   { return nil }

func (d *database) RunReadonlyTransaction(ctx context.Context, f dal.ROTxWorker, opts ...dal.TransactionOption) error {
	return f(ctx, transaction{db: d, txOpts: dal.NewTransactionOptions(opts...)})
}

func (d *database) RunReadwriteTransaction(ctx context.Context, f dal.RWTxWorker, opts ...dal.TransactionOption) error {
	return f(ctx, transaction{db: d, txOpts: dal.NewTransactionOptions(opts...)})
}

func (d *database) collection(name string) (Collection, error) {
	coll, ok := d.collections[name]
	if !ok {
		return Collection{}, fmt.Errorf("%w: %q", ErrUnknownCollection, name)
	}
	return coll, nil
}

// Get implements dal.Getter. It requires coll.KeyField to be a declared
// Param (see Capabilities.SupportsGet): a key field the endpoint cannot be
// asked for by equality has no URL to fetch, so this fails closed with
// dal.ErrNotSupported rather than fetching a broader listing and scanning it
// — the same "never fetch a superset" rule query.go applies to queries,
// applied to Get.
func (d *database) Get(ctx context.Context, rec record.Record) error {
	key := rec.Key()
	coll, err := d.collection(key.Collection())
	if err != nil {
		rec.SetError(err)
		return err
	}
	if _, declared := coll.Params[coll.KeyField]; !declared {
		err := fmt.Errorf("%w: collection %q: Get requires key field %q to be a declared parameter", dal.ErrNotSupported, coll.Name, coll.KeyField)
		rec.SetError(err)
		return err
	}
	idStr := fmt.Sprintf("%v", key.ID)
	rows, _, err := d.fetchRows(ctx, coll, map[string]string{coll.KeyField: idStr})
	if err != nil {
		rec.SetError(err)
		return err
	}
	for _, row := range rows {
		if v, ok := row[coll.KeyField]; ok && fmt.Sprintf("%v", v) == idStr {
			rec.SetError(nil)
			if err := record.MapToData(rec.Data(), row); err != nil {
				rec.SetError(err)
				return err
			}
			return nil
		}
	}
	notFound := dal.NewErrNotFoundByKey(key, nil)
	rec.SetError(notFound)
	return notFound
}

// Exists implements dal.Getter. Same KeyField requirement as Get.
func (d *database) Exists(ctx context.Context, key *record.Key) (bool, error) {
	coll, err := d.collection(key.Collection())
	if err != nil {
		return false, err
	}
	if _, declared := coll.Params[coll.KeyField]; !declared {
		return false, fmt.Errorf("%w: collection %q: Exists requires key field %q to be a declared parameter", dal.ErrNotSupported, coll.Name, coll.KeyField)
	}
	idStr := fmt.Sprintf("%v", key.ID)
	rows, _, err := d.fetchRows(ctx, coll, map[string]string{coll.KeyField: idStr})
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if v, ok := row[coll.KeyField]; ok && fmt.Sprintf("%v", v) == idStr {
			return true, nil
		}
	}
	return false, nil
}

// GetMulti implements dal.MultiGetter as a sequential loop over Get, matching
// dalgo2fs/dalgo2memory: this adapter has no batch HTTP endpoint to exploit,
// so there is nothing a bespoke implementation would buy over the loop.
func (d *database) GetMulti(ctx context.Context, records []record.Record) error {
	for _, rec := range records {
		if err := d.Get(ctx, rec); err != nil && !record.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// ExecuteQueryToRecordsetReader is not implemented: this adapter's rows are
// schemaless HTTP/JSON objects, and recordset.Recordset's typed columnar
// shape is not something a declarative descriptor can derive without a
// schema. dalgo2fs, the reference minimal read adapter, makes the same
// choice for the same reason. ExecuteQueryToRecordsReader is the supported
// query surface.
func (d *database) ExecuteQueryToRecordsetReader(_ context.Context, _ dal.Query, _ ...recordset.Option) (dal.RecordsetReader, error) {
	return nil, dal.ErrNotSupported
}

// fetchRows resolves rows for coll given equality params, honouring
// cfg.Mode:
//   - ModeSnapshot reads Config.Snapshots only.
//   - ModeLive always calls the network; a failure is returned as-is.
//   - ModeLiveThenSnapshot calls the network first and falls back to a
//     matching snapshot only when the live failure wraps ErrUpstream (never
//     ErrUpstreamClient — a 4xx is a caller/config error, not something
//     stale data should paper over) and Config.Snapshots has one.
//
// Every path reports Provenance to the ctx-attached Observer, if any, before
// returning.
func (d *database) fetchRows(ctx context.Context, coll Collection, params map[string]string) ([]map[string]any, Provenance, error) {
	key := SnapshotKey(coll.Name, params)

	if d.cfg.Mode == ModeSnapshot {
		body, meta, err := readSnapshot(d.cfg.Snapshots, key)
		if err != nil {
			return nil, Provenance{}, fmt.Errorf("dalgo2http: collection %q: %w", coll.Name, err)
		}
		rows, err := extractRows(body, coll.RowsPath)
		prov := Provenance{Collection: coll.Name, Source: SourceSnapshot, StatusCode: meta.StatusCode, FetchedAt: meta.FetchedAt}
		d.observe(ctx, prov)
		return rows, prov, err
	}

	rawURL, err := buildURL(coll, params)
	if err != nil {
		return nil, Provenance{}, err
	}
	body, status, liveErr := doLiveFetch(ctx, d.cfg.Client, coll, rawURL)
	if liveErr == nil {
		rows, err := extractRows(body, coll.RowsPath)
		prov := Provenance{Collection: coll.Name, Source: SourceLive, StatusCode: status, FetchedAt: time.Now().UTC()}
		d.observe(ctx, prov)
		return rows, prov, err
	}

	if d.cfg.Mode == ModeLiveThenSnapshot && !errors.Is(liveErr, ErrUpstreamClient) {
		if body, meta, snapErr := readSnapshot(d.cfg.Snapshots, key); snapErr == nil {
			rows, err := extractRows(body, coll.RowsPath)
			prov := Provenance{Collection: coll.Name, Source: SourceSnapshot, StatusCode: meta.StatusCode, FetchedAt: meta.FetchedAt}
			d.observe(ctx, prov)
			return rows, prov, err
		}
	}
	return nil, Provenance{}, liveErr
}

func (d *database) observe(ctx context.Context, p Provenance) {
	if obs := provenanceObserverFrom(ctx); obs != nil {
		obs(p)
	}
}
