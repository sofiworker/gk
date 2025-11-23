package gsql

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type Tx struct {
	*sqlx.Tx
	db    *DB
	level int // Transaction nesting level
}

func (tx *Tx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, query, args...)
}

func (tx *Tx) GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return tx.Tx.GetContext(ctx, dest, query, args...)
}

func (tx *Tx) SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return tx.Tx.SelectContext(ctx, dest, query, args...)
}

func (tx *Tx) Builder() *Builder {
	return &Builder{
		executor: tx,
		dialect:  tx.db.dialect,
	}
}

// NestedTx begins a nested transaction or returns the existing transaction if already in one
func (tx *Tx) NestedTx(fn func(*Tx) error, opts ...TxOption) (err error) {
	// Increment nesting level for the new transaction
	newLevel := tx.level + 1

	// For nested transactions, we reuse the existing transaction
	// but wrap it in a new Tx struct with incremented level
	nestedTx := &Tx{
		Tx:    tx.Tx,
		db:    tx.db,
		level: newLevel,
	}

	// Execute the function with the nested transaction
	err = fn(nestedTx)

	// For nested transactions, we don't commit or rollback
	// The top-level transaction will handle that
	return err
}
