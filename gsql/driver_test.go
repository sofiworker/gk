package gsql

import (
	"database/sql"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestOpen(t *testing.T) {
	sql.Register(DriverName, WrapDriver(&mysql.MySQLDriver{}))

	dsn := "root:123456@tcp(192.168.191.135:3306)/testdb?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := Open(DriverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("DROP TABLE IF EXISTS users")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(result.RowsAffected())
	var userID int64
	err = db.Get(&userID, `SELECT id FROM users WHERE id = ?`, 1)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
}
