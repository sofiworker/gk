package gsql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
)

const (
	DriverName = "gsql-driver"
)

// Executor 定义了执行 SQL 操作的接口，DB 和 Tx 都需要实现这个接口
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
}

type DB struct {
	*sqlx.DB
	logger  Logger
	dialect Dialect
}

func Open(driverName, dataSourceName string) (*DB, error) {
	d, err := sqlx.Open(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	return &DB{
		DB:      d,
		dialect: newDialect(driverName),
	}, nil
}

func MustOpen(driverName, dataSourceName string) *DB {
	d := sqlx.MustOpen(driverName, dataSourceName)
	return &DB{
		DB:      d,
		dialect: newDialect(driverName),
	}
}

// Builder creates a new query builder.
func (db *DB) Builder() *Builder {
	return &Builder{
		executor: db,
		dialect:  db.dialect,
	}
}

// From creates a new query builder.
func (db *DB) From(table string) *Builder {
	b := &Builder{
		executor: db,
		dialect:  db.dialect,
	}
	return b.From(table)
}

func (db *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return db.DB.ExecContext(ctx, query, args...)
}

func (db *DB) GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return db.DB.GetContext(ctx, dest, query, args...)
}

func (db *DB) SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return db.DB.SelectContext(ctx, dest, query, args...)
}

type TxOption func(*sql.TxOptions)

func WithTxIsolation(isolation sql.IsolationLevel) TxOption {
	return func(opts *sql.TxOptions) {
		opts.Isolation = isolation
	}
}

func WithTxReadOnly() TxOption {
	return func(opts *sql.TxOptions) {
		opts.ReadOnly = true
	}
}

// Tx begins a transaction
func (db *DB) Tx(fn func(*Tx) error, opts ...TxOption) (err error) {
	return db.TxContext(context.Background(), fn, opts...)
}

// TxContext begins a transaction with context
func (db *DB) TxContext(ctx context.Context, fn func(*Tx) error, opts ...TxOption) (err error) {
	var txOpts sql.TxOptions
	for _, opt := range opts {
		opt(&txOpts)
	}

	txx, err := db.DB.BeginTxx(ctx, &txOpts)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = txx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = txx.Rollback()
			return
		}
		if commitErr := txx.Commit(); commitErr != nil {
			err = fmt.Errorf("commit tx failed: %w", commitErr)
		}
	}()

	tx := &Tx{txx, db, 0} // Level 0 indicates top-level transaction
	err = fn(tx)
	return err
}
