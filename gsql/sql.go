package gsql

import (
	"github.com/jmoiron/sqlx"
)

const (
	DriverName = "gtrace"
)

type DB struct {
	*sqlx.DB
	logger Logger
}

func Open(driverName, dataSourceName string) (*DB, error) {
	d, err := sqlx.Open(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	return &DB{DB: d}, nil
}

func MustOpen(driverName, dataSourceName string) (*DB, error) {
	d := sqlx.MustOpen(driverName, dataSourceName)
	return &DB{DB: d}, nil
}
