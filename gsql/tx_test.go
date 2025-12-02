package gsql

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func newTxMockDB(t *testing.T, opts ...DBOption) (*DB, sqlmock.Sqlmock) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db := WrapSQLX(sqlx.NewDb(sqlDB, "sqlmock"), "sqlmock", opts...)
	return db, mock
}

func TestNestedTxUsesSavepoint(t *testing.T) {
	db, mock := newTxMockDB(t, WithDBNestedStrategy(NestedSavepoint))
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT gsql_sp_1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO users").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT gsql_sp_1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := db.TxContext(ctx, func(tx *Tx) error {
		return tx.NestedTxContext(ctx, func(nested *Tx) error {
			_, err := nested.ExecContext(ctx, "INSERT INTO users(id) VALUES (?)", 1)
			return err
		})
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNestedTxRollbackToSavepoint(t *testing.T) {
	db, mock := newTxMockDB(t, WithDBNestedStrategy(NestedSavepoint))
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT gsql_sp_1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO users").
		WithArgs(1).
		WillReturnError(errors.New("boom"))
	mock.ExpectExec(regexp.QuoteMeta("ROLLBACK TO SAVEPOINT gsql_sp_1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := db.TxContext(ctx, func(tx *Tx) error {
		return tx.NestedTxContext(ctx, func(nested *Tx) error {
			_, err := nested.ExecContext(ctx, "INSERT INTO users(id) VALUES (?)", 1)
			return err
		})
	})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNestedTxWithoutSavepointFallsBack(t *testing.T) {
	db, mock := newTxMockDB(t, WithDBNestedStrategy(NestedReuse))
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO users").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := db.TxContext(ctx, func(tx *Tx) error {
		return tx.NestedTxContext(ctx, func(nested *Tx) error {
			_, err := nested.ExecContext(ctx, "INSERT INTO users(id) VALUES (?)", 1)
			return err
		})
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNestedTxDisabled(t *testing.T) {
	db, mock := newTxMockDB(t, WithDBNestedStrategy(NestedError))
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectRollback()

	err := db.TxContext(ctx, func(tx *Tx) error {
		return tx.NestedTxContext(ctx, func(*Tx) error {
			return nil
		})
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nested transaction is disabled")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNestedCommitNotAllowed(t *testing.T) {
	db, mock := newTxMockDB(t, WithDBNestedStrategy(NestedReuse))
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectRollback()

	err := db.TxContext(ctx, func(tx *Tx) error {
		return tx.NestedTxContext(ctx, func(nested *Tx) error {
			return nested.Commit()
		})
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot commit nested transaction")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNestedDialectUsesSavepointWhenSupported(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db := WrapSQLX(sqlx.NewDb(sqlDB, "postgres"), "postgres", WithDBNestedStrategy(NestedDialect))
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT gsql_sp_1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO users").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT gsql_sp_1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err = db.TxContext(ctx, func(tx *Tx) error {
		return tx.NestedTxContext(ctx, func(nested *Tx) error {
			_, e := nested.ExecContext(ctx, "INSERT INTO users(id) VALUES (?)", 1)
			return e
		})
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNestedDialectErrorsWhenUnsupported(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db := WrapSQLX(sqlx.NewDb(sqlDB, "generic"), "generic", WithDBNestedStrategy(NestedDialect))
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectRollback()

	err = db.TxContext(ctx, func(tx *Tx) error {
		return tx.NestedTxContext(ctx, func(*Tx) error { return nil })
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nested transaction is disabled")
	require.NoError(t, mock.ExpectationsWereMet())
}
