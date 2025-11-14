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
	err = db.Get(&userID, `SELECT id FROM userss WHERE id = ?`, 1)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
}

// TestTransaction 测试新的事务实现
func TestTransaction(t *testing.T) {
	sql.Register(DriverName, WrapDriver(&mysql.MySQLDriver{}))

	dsn := "root:123456@tcp(192.168.191.135:3306)/testdb?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := Open(DriverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 创建测试表
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS test_users (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}

	// 测试事务操作
	err = db.From("test_users").Tx(func(tx *Tx) error {
		// 插入数据
		_, err := tx.ExecContext(nil, "INSERT INTO test_users (name, email) VALUES (?, ?)", "Alice", "alice@example.com")
		if err != nil {
			return err
		}

		// 更新数据
		_, err = tx.ExecContext(nil, "UPDATE test_users SET email = ? WHERE name = ?", "alice.new@example.com", "Alice")
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	// 验证数据是否正确更新
	var email string
	err = db.Get(&email, "SELECT email FROM test_users WHERE name = ?", "Alice")
	if err != nil {
		t.Fatal(err)
	}

	if email != "alice.new@example.com" {
		t.Fatalf("Expected email to be alice.new@example.com, got %s", email)
	}
}
