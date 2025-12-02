package gsql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/sofiworker/gk/glog"
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
	logger     Logger
	dialect    Dialect
	driverName string
	nested     NestedStrategy
}

type DBOption func(*DB)

func WithDBLogger(logger Logger) DBOption {
	return func(db *DB) {
		db.logger = logger
	}
}

// WithDBNestedStrategy 配置嵌套事务处理策略。
func WithDBNestedStrategy(strategy NestedStrategy) DBOption {
	return func(db *DB) { db.nested = strategy }
}

func Open(driverName, dataSourceName string, opts ...DBOption) (*DB, error) {
	d, err := sqlx.Open(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	return wrapSQLX(d, driverName, opts...), nil
}

func MustOpen(driverName, dataSourceName string, opts ...DBOption) *DB {
	d := sqlx.MustOpen(driverName, dataSourceName)
	return wrapSQLX(d, driverName, opts...)
}

// WrapSQLX 允许将现有的 *sqlx.DB 包装成 gsql.DB，保持方言与配置一致。
func WrapSQLX(db *sqlx.DB, driverName string, opts ...DBOption) *DB {
	return wrapSQLX(db, driverName, opts...)
}

func wrapSQLX(db *sqlx.DB, driverName string, opts ...DBOption) *DB {
	wrapped := &DB{
		DB:         db,
		dialect:    newDialect(driverName),
		driverName: driverName,
	}
	// 默认使用 SAVEPOINT 形式的嵌套事务；如驱动不支持，则在执行时返回错误。
	wrapped.nested = NestedSavepoint
	for _, opt := range opts {
		opt(wrapped)
	}
	wrapped.ensureLogger()
	return wrapped
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

type TxOptions struct {
	SQL            sql.TxOptions
	Nested         NestedStrategy
	SavepointNamer savepointNamer
}

type TxOption func(*TxOptions)

func (db *DB) ensureLogger() Logger {
	if db.logger == nil {
		db.logger = glog.Default()
	}
	return db.logger
}

func WithTxIsolation(isolation sql.IsolationLevel) TxOption {
	return func(opts *TxOptions) {
		opts.SQL.Isolation = isolation
	}
}

func WithTxReadOnly() TxOption {
	return func(opts *TxOptions) {
		opts.SQL.ReadOnly = true
	}
}

// WithTxNestedStrategy 在单次事务上覆盖嵌套策略。
func WithTxNestedStrategy(strategy NestedStrategy) TxOption {
	return func(opts *TxOptions) {
		opts.Nested = strategy
	}
}

// WithTxSavepointNamer 自定义 savepoint 命名。
func WithTxSavepointNamer(namer savepointNamer) TxOption {
	return func(opts *TxOptions) {
		if namer != nil {
			opts.SavepointNamer = namer
		}
	}
}

// Tx begins a transaction
func (db *DB) Tx(fn func(*Tx) error, opts ...TxOption) (err error) {
	return db.TxContext(context.Background(), fn, opts...)
}

// TxContext begins a transaction with context
func (db *DB) TxContext(ctx context.Context, fn func(*Tx) error, opts ...TxOption) (err error) {
	txOpts := db.defaultTxOptions()
	for _, opt := range opts {
		opt(&txOpts)
	}

	txx, err := db.DB.BeginTxx(ctx, &txOpts.SQL)
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

	tx := newTx(txx, db, 0, txOpts)
	err = fn(tx)
	return err
}

func (db *DB) defaultTxOptions() TxOptions {
	return TxOptions{
		Nested:         db.nested,
		SavepointNamer: defaultSavepointNamer,
	}
}
