package gsql

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Dialect defines an interface for database-specific operations.
type Dialect interface {
	// PlaceholderSQL replaces '?' with the database-specific placeholder.
	PlaceholderSQL(sql string) string
	// Placeholder returns the placeholder for a given index.
	Placeholder(index int) string
	// DataTypeOf returns the database-specific data type for a given Go type.
	DataTypeOf(typ reflect.Type) string
	// AutoIncrement returns the database-specific auto-increment keyword.
	AutoIncrement() string
	// PrimaryKeyStr returns the optimal string type for a primary key.
	PrimaryKeyStr() string
}

func newDialect(driverName string) Dialect {
	switch driverName {
	case "mysql":
		return &mysqlDialect{}
	case "postgres":
		return &postgresDialect{}
	case "sqlite3":
		return &sqliteDialect{}
	default:
		return &genericDialect{}
	}
}

// --- MySQL ---
type mysqlDialect struct{}

func (d *mysqlDialect) PlaceholderSQL(sql string) string { return sql }
func (d *mysqlDialect) Placeholder(index int) string    { return "?" }
func (d *mysqlDialect) DataTypeOf(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Bool:
		return "BOOLEAN"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return "INT"
	case reflect.Int64, reflect.Uint64:
		return "BIGINT"
	case reflect.Float32, reflect.Float64:
		return "DOUBLE"
	case reflect.String:
		return "VARCHAR(255)"
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return "DATETIME"
	}
	if typ == reflect.TypeOf(sql.NullString{}) {
		return "VARCHAR(255)"
	}
	// Add other sql.Null types as needed
	return "TEXT"
}
func (d *mysqlDialect) AutoIncrement() string { return "AUTO_INCREMENT" }
func (d *mysqlDialect) PrimaryKeyStr() string { return "VARCHAR(255)" }

// --- PostgreSQL ---
type postgresDialect struct{}

func (d *postgresDialect) PlaceholderSQL(sql string) string {
	var builder strings.Builder
	i := 1
	for _, r := range sql {
		if r == '?' {
			builder.WriteString(fmt.Sprintf("$%d", i))
			i++
		} else {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
func (d *postgresDialect) Placeholder(index int) string { return fmt.Sprintf("$%d", index+1) }
func (d *postgresDialect) DataTypeOf(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Bool:
		return "BOOLEAN"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return "INTEGER"
	case reflect.Int64, reflect.Uint64:
		return "BIGINT"
	case reflect.Float32, reflect.Float64:
		return "DOUBLE PRECISION"
	case reflect.String:
		return "VARCHAR(255)"
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return "TIMESTAMPTZ"
	}
	return "TEXT"
}
func (d *postgresDialect) AutoIncrement() string { return "" } // PostgreSQL uses SERIAL or IDENTITY columns
func (d *postgresDialect) PrimaryKeyStr() string { return "VARCHAR(255)" }

// --- SQLite ---
type sqliteDialect struct{}

func (d *sqliteDialect) PlaceholderSQL(sql string) string { return sql }
func (d *sqliteDialect) Placeholder(index int) string    { return "?" }
func (d *sqliteDialect) DataTypeOf(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Int64, reflect.Uint64:
		return "INTEGER"
	case reflect.Float32, reflect.Float64:
		return "REAL"
	case reflect.String:
		return "TEXT"
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return "TIMESTAMP"
	}
	return "TEXT"
}
func (d *sqliteDialect) AutoIncrement() string { return "PRIMARY KEY AUTOINCREMENT" }
func (d *sqliteDialect) PrimaryKeyStr() string { return "TEXT" }

// --- Generic ---
type genericDialect struct{}

func (d *genericDialect) PlaceholderSQL(sql string) string { return sql }
func (d *genericDialect) Placeholder(index int) string    { return "?" }
func (d *genericDialect) DataTypeOf(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Bool:
		return "BOOLEAN"
	case reflect.Int, reflect.Int32, reflect.Int64:
		return "BIGINT"
	case reflect.Float32, reflect.Float64:
		return "DOUBLE"
	case reflect.String:
		return "VARCHAR(255)"
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return "TIMESTAMP"
	}
	return "TEXT"
}
func (d *genericDialect) AutoIncrement() string { return "AUTO_INCREMENT" }
func (d *genericDialect) PrimaryKeyStr() string { return "VARCHAR(255)" }
