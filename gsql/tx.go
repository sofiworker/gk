package gsql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// NestedStrategy 定义嵌套事务的处理方式。
type NestedStrategy int

const (
	// NestedSavepoint 使用 SAVEPOINT/RELEASE 来实现可回滚的嵌套事务。
	NestedSavepoint NestedStrategy = iota
	// NestedReuse 复用当前事务，不做额外隔离（错误会影响最外层）。
	NestedReuse
	// NestedError 禁止嵌套事务。
	NestedError
	// NestedDialect 按当前 dialect 能力选择：支持 savepoint 则使用 Savepoint，否则报错。
	NestedDialect
)

type savepointNamer func(level int) string

type Tx struct {
	*sqlx.Tx
	db     *DB
	nested NestedStrategy
	level  int // Transaction nesting层级，0 为最外层
	namer  savepointNamer
}

func newTx(txx *sqlx.Tx, db *DB, level int, opts TxOptions) *Tx {
	namer := opts.SavepointNamer
	if namer == nil {
		namer = defaultSavepointNamer
	}
	return &Tx{
		Tx:     txx,
		db:     db,
		nested: opts.Nested,
		level:  level,
		namer:  namer,
	}
}

func defaultSavepointNamer(level int) string {
	return fmt.Sprintf("gsql_sp_%d", level)
}

func (tx *Tx) Builder() *Builder {
	return &Builder{
		executor: tx,
		dialect:  tx.db.dialect,
	}
}

// NestedTx 在当前事务中创建嵌套事务。
func (tx *Tx) NestedTx(fn func(*Tx) error, opts ...TxOption) error {
	return tx.NestedTxContext(context.Background(), fn, opts...)
}

func (tx *Tx) NestedTxContext(ctx context.Context, fn func(*Tx) error, _ ...TxOption) error {
	nestedLevel := tx.level + 1
	nested := newTx(tx.Tx, tx.db, nestedLevel, TxOptions{
		Nested:         tx.nested,
		SavepointNamer: tx.namer,
	})

	switch tx.effectiveStrategy() {
	case NestedReuse:
		return fn(nested)
	case NestedError:
		return fmt.Errorf("gsql: nested transaction is disabled for driver %s", tx.db.driverName)
	case NestedSavepoint:
		// continue with savepoint
	default:
		return fmt.Errorf("gsql: unknown nested strategy")
	}

	spName := tx.namer(nestedLevel)
	if err := tx.execSavepoint(ctx, spName); err != nil {
		return fmt.Errorf("create savepoint failed: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.execRollbackTo(ctx, spName)
			panic(p)
		}
	}()

	if err := fn(nested); err != nil {
		_ = tx.execRollbackTo(ctx, spName)
		return err
	}

	if err := tx.execRelease(ctx, spName); err != nil {
		return fmt.Errorf("release savepoint failed: %w", err)
	}
	return nil
}

func (tx *Tx) execSavepoint(ctx context.Context, name string) error {
	_, err := tx.ExecContext(ctx, "SAVEPOINT "+name)
	return err
}

func (tx *Tx) execRollbackTo(ctx context.Context, name string) error {
	_, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name)
	return err
}

func (tx *Tx) execRelease(ctx context.Context, name string) error {
	_, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+name)
	return err
}

// Commit 仅允许最外层事务调用。
func (tx *Tx) Commit() error {
	if tx.level > 0 {
		return fmt.Errorf("gsql: cannot commit nested transaction")
	}
	return tx.Tx.Commit()
}

// Rollback 仅允许最外层事务调用。
func (tx *Tx) Rollback() error {
	if tx.level > 0 {
		return fmt.Errorf("gsql: cannot rollback nested transaction")
	}
	return tx.Tx.Rollback()
}

func (tx *Tx) effectiveStrategy() NestedStrategy {
	if tx.nested == NestedDialect {
		if tx.db.dialect.SupportsSavepoint() {
			return NestedSavepoint
		}
		return NestedError
	}
	return tx.nested
}
