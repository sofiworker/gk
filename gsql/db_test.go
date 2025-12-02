package gsql

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

type stubLogger struct{}

func (stubLogger) Debugf(string, ...interface{}) {}
func (stubLogger) Infof(string, ...interface{})  {}
func (stubLogger) Warnf(string, ...interface{})  {}
func (stubLogger) Errorf(string, ...interface{}) {}

func TestWrapSQLXDefaultNestedStrategy(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := WrapSQLX(sqlx.NewDb(sqlDB, "mysql"), "mysql")
	require.Equal(t, NestedSavepoint, db.nested)
}

func TestWrapSQLXOverrideNestedStrategy(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := WrapSQLX(sqlx.NewDb(sqlDB, "generic"), "generic", WithDBNestedStrategy(NestedReuse))
	require.Equal(t, NestedReuse, db.nested)
}

func TestWithDBLogger(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	logger := &stubLogger{}
	db := WrapSQLX(sqlx.NewDb(sqlDB, "mysql"), "mysql", WithDBLogger(logger))
	require.Equal(t, logger, db.logger)
}
