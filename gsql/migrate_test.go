package gsql

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

type mockMigrationSource struct {
	migrations []*Migration
	err        error
}

func (m mockMigrationSource) Collect() ([]*Migration, error) {
	return m.migrations, m.err
}

func newMockDB(t *testing.T) (*DB, sqlmock.Sqlmock) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return WrapSQLX(sqlx.NewDb(sqlDB, "sqlmock"), "sqlmock"), mock
}

func TestMigratorSetsDefaultLogger(t *testing.T) {
	db, mock := newMockDB(t)

	mock.ExpectBegin()
	mock.ExpectCommit()

	migrator := db.Migrate()
	migrator.AddSource(mockMigrationSource{
		migrations: []*Migration{
			{ID: "001", Name: "noop", Up: func(*Tx) error { return nil }},
		},
	})

	require.NoError(t, migrator.Run(context.Background()))
	require.NotNil(t, db.logger)
	require.NotNil(t, migrator.Logger)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMigratorPassesContextToMigration(t *testing.T) {
	db, mock := newMockDB(t)

	mock.ExpectBegin()
	mock.ExpectCommit()

	ctxKey := struct{}{}
	ctx := context.WithValue(context.Background(), ctxKey, "value")
	var received context.Context

	migrator := db.Migrate()
	migrator.AddSource(mockMigrationSource{
		migrations: []*Migration{
			{
				ID:   "ctx-mig",
				Name: "ctx-migration",
				UpWithContext: func(runCtx context.Context, _ *Tx) error {
					received = runCtx
					return nil
				},
			},
		},
	})

	require.NoError(t, migrator.Run(ctx))
	require.Same(t, ctx, received)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMigratorUsesContextForCollectorQueries(t *testing.T) {
	db, mock := newMockDB(t)

	dir := t.TempDir()
	sqlContent := "-- +gsql Up\nCREATE TABLE users (id INT);"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "001_create_users.sql"), []byte(sqlContent), 0o644))

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE users").
		WillDelayFor(50 * time.Millisecond).
		WillReturnResult(sqlmock.NewResult(0, 0))

	migrator := db.Migrate()
	migrator.AddSource(NewSQLDirSource(dir))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := migrator.Run(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cancel")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDefaultStructParserSetsMigrationID(t *testing.T) {
	parser := NewDefaultStructParser()
	type MyUser struct {
		ID int `db:"id"`
	}

	mig, err := parser.Parse(newDialect("mysql"), &MyUser{})
	require.NoError(t, err)
	require.NotNil(t, mig)
	require.Equal(t, "structs_my_user", mig.ID)
	require.Contains(t, mig.Name, "my_user")
}

type recordingLogger struct {
	messages []string
}

func (l *recordingLogger) Debugf(string, ...interface{}) {}
func (l *recordingLogger) Infof(format string, v ...interface{}) {
	l.messages = append(l.messages, fmt.Sprintf(format, v...))
}
func (l *recordingLogger) Warnf(string, ...interface{})  {}
func (l *recordingLogger) Errorf(string, ...interface{}) {}

type noopTx struct{}

func (noopTx) Commit() error   { return nil }
func (noopTx) Rollback() error { return nil }

func TestTracedTxRollbackLogsRollback(t *testing.T) {
	logger := &recordingLogger{}
	tx := &tracedTx{
		Tx: &noopTx{},
		driverConfig: &driverConfig{
			logger: logger,
		},
	}

	require.NoError(t, tx.Rollback())
	require.Equal(t, []string{"ROLLBACK"}, logger.messages)
}
