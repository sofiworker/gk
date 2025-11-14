package gsql

import "fmt"

// Dialect defines the interface for different SQL dialects.
type Dialect interface {
	// Placeholder returns the placeholder for the n-th argument.
	Placeholder(n int) string
}

// mysqlDialect implements the Dialect for MySQL.
type mysqlDialect struct{}

func (d *mysqlDialect) Placeholder(n int) string {
	return "?"
}

// postgresDialect implements the Dialect for PostgreSQL.
type postgresDialect struct{}

func (d *postgresDialect) Placeholder(n int) string {
	return fmt.Sprintf("$%d", n+1)
}

// sqliteDialect implements the Dialect for SQLite.
type sqliteDialect struct{}

func (d *sqliteDialect) Placeholder(n int) string {
	return "?"
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
		// Default to MySQL style placeholders
		return &mysqlDialect{}
	}
}
