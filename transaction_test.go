package dalgo2http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
)

func TestDatabaseMetadata(t *testing.T) {
	db, err := NewDB(Config{Collections: []Collection{countriesCollection("https://example.invalid")}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	if db.ID() != "dalgo2http" {
		t.Fatalf("ID() = %q, want dalgo2http", db.ID())
	}
	if db.Adapter() != nil {
		t.Fatalf("Adapter() = %v, want nil", db.Adapter())
	}
	if db.Schema() != nil {
		t.Fatalf("Schema() = %v, want nil", db.Schema())
	}
	if db.SupportsConcurrentConnections() {
		t.Fatalf("SupportsConcurrentConnections() = true, want false")
	}
}

func TestExecuteQueryToRecordsetReader_NotSupported(t *testing.T) {
	db, err := NewDB(Config{Collections: []Collection{countriesCollection("https://example.invalid")}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	q := newQuery("countries", nil, 0)
	if _, err := db.ExecuteQueryToRecordsetReader(context.Background(), q); !isNotSupported(err) {
		t.Fatalf("err = %v, want dal.ErrNotSupported", err)
	}
}

func TestReadonlyTransaction_ForwardsReads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"France","currency":"EUR"}}`))
	}))
	defer srv.Close()
	db, err := NewDB(Config{Collections: []Collection{countriesCollection(srv.URL)}, Client: srv.Client(), Mode: ModeLive})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	err = db.RunReadonlyTransaction(context.Background(), func(ctx context.Context, tx dal.ReadTransaction) error {
		if tx.Options() == nil {
			t.Fatalf("tx.Options() = nil")
		}
		exists, err := tx.Exists(ctx, record.NewKeyWithID("countries", "France"))
		if err != nil {
			return err
		}
		if !exists {
			t.Fatalf("tx.Exists() = false, want true")
		}
		target := map[string]any{}
		rec := record.NewRecordWithData(record.NewKeyWithID("countries", "France"), &target)
		if err := tx.Get(ctx, rec); err != nil {
			return err
		}
		if err := tx.GetMulti(ctx, []record.Record{rec}); err != nil {
			return err
		}
		q := newQuery("countries", dal.WhereField("name", dal.Equal, "France"), 0)
		reader, err := tx.ExecuteQueryToRecordsReader(ctx, q)
		if err != nil {
			return err
		}
		if _, err := reader.Next(); err != nil {
			return err
		}
		if _, err := tx.ExecuteQueryToRecordsetReader(ctx, q); !isNotSupported(err) {
			t.Fatalf("tx.ExecuteQueryToRecordsetReader() err = %v, want dal.ErrNotSupported", err)
		}
		return nil
	}, dal.TxWithMessage("test"))
	if err != nil {
		t.Fatalf("RunReadonlyTransaction: %v", err)
	}
}

func TestReadwriteTransaction_WritesAreNotSupported(t *testing.T) {
	db, err := NewDB(Config{Collections: []Collection{countriesCollection("https://example.invalid")}, Client: noCallClient(t)})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	err = db.RunReadwriteTransaction(context.Background(), func(ctx context.Context, tx dal.ReadwriteTransaction) error {
		if tx.ID() != "" {
			t.Fatalf("tx.ID() = %q, want empty", tx.ID())
		}
		key := record.NewKeyWithID("countries", "France")
		if err := tx.Delete(ctx, key); !isNotSupported(err) {
			t.Fatalf("Delete() err = %v, want dal.ErrNotSupported", err)
		}
		if err := tx.DeleteMulti(ctx, []*record.Key{key}); !isNotSupported(err) {
			t.Fatalf("DeleteMulti() err = %v, want dal.ErrNotSupported", err)
		}
		if err := tx.Update(ctx, key, nil); !isNotSupported(err) {
			t.Fatalf("Update() err = %v, want dal.ErrNotSupported", err)
		}
		if err := tx.UpdateMulti(ctx, []*record.Key{key}, nil); !isNotSupported(err) {
			t.Fatalf("UpdateMulti() err = %v, want dal.ErrNotSupported", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunReadwriteTransaction: %v", err)
	}
}
