package gsql

import (
	"database/sql/driver"
	"time"

	"github.com/sofiworker/gk/glog"
)

type Option func(c *driverConfig)

type driverConfig struct {
	logger Logger
}

type tracedDriver struct {
	driver.Driver
	*driverConfig
}

type tracedConn struct {
	driver.Conn
	*driverConfig
}

// 包装语句
type tracedStmt struct {
	driver.Stmt
	query string
	*driverConfig
}

// 包装事务
type tracedTx struct {
	driver.Tx
	*driverConfig
}

var _ driver.Driver = &tracedDriver{}
var _ driver.Conn = &tracedConn{}
var _ driver.Stmt = &tracedStmt{}
var _ driver.Tx = &tracedTx{}

func (d *tracedDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return &tracedConn{conn, d.driverConfig}, nil
}

func (c *tracedConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}

	c.logger.Infof("PREPARE: %s", query)

	return &tracedStmt{stmt, query, c.driverConfig}, nil
}

func (c *tracedConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}

	c.logger.Infof("BEGIN TRANSACTION")

	return &tracedTx{tx, c.driverConfig}, nil
}

func (s *tracedStmt) Exec(args []driver.Value) (driver.Result, error) {
	start := time.Now()
	result, err := s.Stmt.Exec(args)
	duration := time.Since(start)
	s.logger.Infof("EXEC: %s, args: %v, took: %v", s.query, args, duration)
	return result, err
}

func (s *tracedStmt) Query(args []driver.Value) (driver.Rows, error) {
	start := time.Now()
	rows, err := s.Stmt.Query(args)
	duration := time.Since(start)
	s.logger.Infof("QUERY: %s, args: %v, took: %v", s.query, args, duration)
	return rows, err
}

func (t *tracedTx) Commit() error {
	t.logger.Infof("COMMIT")
	return t.Tx.Commit()
}

func (t *tracedTx) Rollback() error {
	t.logger.Infof("COMMIT")
	return t.Tx.Rollback()
}

func WrapDriver(drv driver.Driver) driver.Driver {
	return &tracedDriver{Driver: drv, driverConfig: &driverConfig{logger: glog.Default()}}
}

func WrapDriverWithOptions(drv driver.Driver, opts ...Option) driver.Driver {
	cfg := driverConfig{
		logger: glog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &tracedDriver{Driver: drv, driverConfig: &cfg}
}
