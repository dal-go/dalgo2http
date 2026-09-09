package dalgo2http

import (
	"context"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/dalgo/recordset"
	"github.com/dal-go/record"
	"github.com/dal-go/record/update"
)

// transaction is the dal.ReadTransaction / dal.ReadwriteTransaction dalgo2http
// hands to a RunReadonlyTransaction/RunReadwriteTransaction worker. Reads
// forward to the database (there is no per-transaction state to isolate: each
// call is an independent HTTP request). Every write method returns
// dal.ErrNotSupported trivially, satisfying WriteSession/ReadwriteSession so
// this type — and RunReadwriteTransaction, which is unconditionally wrapped
// by the framework's write pipeline regardless of whether the Backend itself
// implements WriteSession — type-checks. This is also what makes the
// dalgotest conformance suite pass meaningfully rather than trivially: an
// invalid record is rejected by pipeline validation before ever reaching
// these stubs (see conformance_test.go).
type transaction struct {
	db     *database
	txOpts dal.TransactionOptions
}

var (
	_ dal.ReadTransaction      = transaction{}
	_ dal.ReadwriteTransaction = transaction{}
)

func (t transaction) Options() dal.TransactionOptions { return t.txOpts }

// ID reports no native transaction id: dalgo2http has no server-side
// transaction to identify — every read is an independent stateless request.
func (t transaction) ID() string { return "" }

func (t transaction) Get(ctx context.Context, rec record.Record) error {
	return t.db.Get(ctx, rec)
}

func (t transaction) Exists(ctx context.Context, key *record.Key) (bool, error) {
	return t.db.Exists(ctx, key)
}

func (t transaction) GetMulti(ctx context.Context, records []record.Record) error {
	return t.db.GetMulti(ctx, records)
}

func (t transaction) ExecuteQueryToRecordsReader(ctx context.Context, query dal.Query) (dal.RecordsReader, error) {
	return t.db.ExecuteQueryToRecordsReader(ctx, query)
}

func (t transaction) ExecuteQueryToRecordsetReader(ctx context.Context, query dal.Query, opts ...recordset.Option) (dal.RecordsetReader, error) {
	return t.db.ExecuteQueryToRecordsetReader(ctx, query, opts...)
}

func (t transaction) Set(context.Context, record.Record) error { return dal.ErrNotSupported }
func (t transaction) SetMulti(context.Context, []record.Record) error {
	return dal.ErrNotSupported
}
func (t transaction) Delete(context.Context, *record.Key) error { return dal.ErrNotSupported }
func (t transaction) DeleteMulti(context.Context, []*record.Key) error {
	return dal.ErrNotSupported
}
func (t transaction) Update(context.Context, *record.Key, []update.Update, ...dal.Precondition) error {
	return dal.ErrNotSupported
}
func (t transaction) UpdateRecord(context.Context, record.Record, []update.Update, ...dal.Precondition) error {
	return dal.ErrNotSupported
}
func (t transaction) UpdateMulti(context.Context, []*record.Key, []update.Update, ...dal.Precondition) error {
	return dal.ErrNotSupported
}
func (t transaction) Insert(context.Context, record.Record, ...dal.InsertOption) error {
	return dal.ErrNotSupported
}
func (t transaction) InsertMulti(context.Context, []record.Record, ...dal.InsertOption) error {
	return dal.ErrNotSupported
}
