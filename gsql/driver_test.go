package gsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{}, nil }

type fakeConn struct{}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) { return &fakeStmt{query: query}, nil }
func (c *fakeConn) Close() error                              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)                 { return &noopTx{}, nil }

type fakeStmt struct {
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) { return &fakeRows{}, nil }

type fakeRows struct{}

func (r *fakeRows) Columns() []string              { return []string{"id"} }
func (r *fakeRows) Close() error                   { return nil }
func (r *fakeRows) Next(dest []driver.Value) error { return io.EOF }

func TestWrapDriverWithOptionsLogsOperations(t *testing.T) {
	driverName := t.Name()
	logger := &recordingLogger{}
	sql.Register(driverName, WrapDriverWithOptions(&fakeDriver{}, func(cfg *driverConfig) {
		cfg.logger = logger
	}))

	db, err := Open(driverName, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.Tx(func(tx *Tx) error {
		_, err := tx.ExecContext(context.Background(), "INSERT INTO demo(id) VALUES (?)", 1)
		return err
	}))

	require.NotEmpty(t, logger.messages)
	require.Contains(t, logger.messages[0], "BEGIN")
	require.Contains(t, logger.messages[len(logger.messages)-1], "COMMIT")
}
