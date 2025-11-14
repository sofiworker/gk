package gsql

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type Tx struct {
	*sqlx.Tx
	db *DB
}

// Executor interface implementation for Tx
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
