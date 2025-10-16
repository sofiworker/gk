package gsql

import (
	"database/sql/driver"
	"log"
	"time"
)

type Option func(c *driverConfig)

type driverConfig struct {
	logger Logger
}

// 包装驱动
type tracedDriver struct {
	driver.Driver
	config *driverConfig
}

// 包装连接
type tracedConn struct {
	driver.Conn
}

// 包装语句
type tracedStmt struct {
	driver.Stmt
	query string
}

// 包装事务
type tracedTx struct {
	driver.Tx
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
	return &tracedConn{conn}, nil
}

func (c *tracedConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}

	// 打印准备语句
	log.Printf("PREPARE: %s", query)

	return &tracedStmt{stmt, query}, nil
}

func (c *tracedConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}

	log.Printf("BEGIN TRANSACTION")

	return &tracedTx{tx}, nil
}

func (s *tracedStmt) Exec(args []driver.Value) (driver.Result, error) {
	// 打印执行语句和参数
	log.Printf("EXEC: %s, args: %v", s.query, args)
	start := time.Now()
	result, err := s.Stmt.Exec(args)
	duration := time.Since(start)
	log.Printf("EXEC took: %v", duration)
	return result, err
}

func (s *tracedStmt) Query(args []driver.Value) (driver.Rows, error) {
	// 打印查询语句和参数
	log.Printf("QUERY: %s, args: %v", s.query, args)
	start := time.Now()
	rows, err := s.Stmt.Query(args)
	duration := time.Since(start)
	log.Printf("QUERY took: %v", duration)
	return rows, err
}

func (t *tracedTx) Commit() error {
	log.Printf("COMMIT")
	return t.Tx.Commit()
}

func (t *tracedTx) Rollback() error {
	log.Printf("ROLLBACK")
	return t.Tx.Rollback()
}

func WrapDriver(drv driver.Driver) driver.Driver {
	return &tracedDriver{Driver: drv}
}

func WrapDriverWithOptions(drv driver.Driver, opts ...Option) driver.Driver {
	var cfg driverConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return &tracedDriver{Driver: drv, config: &cfg}
}
