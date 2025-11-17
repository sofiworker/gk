package gsql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3" // In-memory DB for testing
)

// --- Test Helpers ---

func setupTestDB(t *testing.T) *DB {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func createTestSQLFiles(t *testing.T, dir string, files map[string]string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create test migration directory: %v", err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test migration file %s: %v", name, err)
		}
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
}

type testLogger struct {
	builder strings.Builder
}

func (l *testLogger) Printf(format string, v ...interface{}) {
	l.builder.WriteString(fmt.Sprintf(format, v...))
	l.builder.WriteString("\n")
}
func (l *testLogger) String() string { return l.builder.String() }
func (l *testLogger) Clear()         { l.builder.Reset() }

// --- SQL Collector Strategy Tests ---

func TestMigrator_Run_DefaultCommentCollector(t *testing.T) {
	db := setupTestDB(t)
	migrationsDir := t.TempDir()
	files := map[string]string{
		"001_create_users.sql": `
-- +gsql Up
CREATE TABLE users (id INTEGER);
-- +gsql Down
DROP TABLE users;
`,
		"002_no_comment.sql": "CREATE TABLE should_be_ignored (id INTEGER);",
	}
	createTestSQLFiles(t, migrationsDir, files)

	// Use default collector by not passing one
	migrator := db.Migrate()
	migrator.AddSource(NewSQLDirSource(migrationsDir))
	if err := migrator.Run(context.Background()); err != nil {
		t.Fatalf("migrator.Run() failed: %v", err)
	}

	// Verify only the table from the file with comments was created
	var tableName string
	if err := db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='users'"); err != nil {
		t.Errorf("Failed to find 'users' table which should have been created: %v", err)
	}
	err := db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='should_be_ignored'")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Error("'should_be_ignored' table should not exist, but it does")
	}
}

func TestMigrator_Run_WholeFileCollector(t *testing.T) {
	db := setupTestDB(t)
	migrationsDir := t.TempDir()
	files := map[string]string{
		"001_create_users.sql": "CREATE TABLE users (id INTEGER);",
		"002_create_posts.sql": "CREATE TABLE posts (id INTEGER);",
	}
	createTestSQLFiles(t, migrationsDir, files)

	// Inject the WholeFileCollector strategy
	migrator := db.Migrate()
	migrator.AddSource(NewSQLDirSource(migrationsDir, &WholeFileCollector{}))
	if err := migrator.Run(context.Background()); err != nil {
		t.Fatalf("migrator.Run() failed: %v", err)
	}

	// Verify both tables were created
	var tableName string
	if err := db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='users'"); err != nil {
		t.Errorf("Failed to find 'users' table: %v", err)
	}
	if err := db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='posts'"); err != nil {
		t.Errorf("Failed to find 'posts' table: %v", err)
	}
}

func TestMigrator_Run_FilenameCollector(t *testing.T) {
	db := setupTestDB(t)
	migrationsDir := t.TempDir()
	files := map[string]string{
		"001_users_up.sql":   "CREATE TABLE users (id INTEGER);",
		"001_users_down.sql": "DROP TABLE users;",
		"002_posts_up.sql":   "CREATE TABLE posts (id INTEGER);",
	}
	createTestSQLFiles(t, migrationsDir, files)

	// Inject the FilenameCollector strategy
	migrator := db.Migrate()
	migrator.AddSource(NewSQLDirSource(migrationsDir, &FilenameCollector{}))
	if err := migrator.Run(context.Background()); err != nil {
		t.Fatalf("migrator.Run() failed: %v", err)
	}

	// Verify tables were created
	var tableName string
	if err := db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='users'"); err != nil {
		t.Errorf("Failed to find 'users' table: %v", err)
	}
	if err := db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='posts'"); err != nil {
		t.Errorf("Failed to find 'posts' table: %v", err)
	}

	// Verify migrations were recorded by base name
	var count int
	if err := db.GetContext(context.Background(), &count, "SELECT count(*) FROM gsql_migrations WHERE id IN ('001_users', '002_posts')"); err != nil {
		t.Fatalf("Failed to query migrations table: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 migrations to be recorded with base names, got %d", count)
	}
}

// --- Struct Parser Strategy Tests ---

func TestMigrator_Run_DefaultStructParser(t *testing.T) {
	db := setupTestDB(t)
	type MyUser struct {
		ID int `db:"id"`
	}

	// Use default parser by not passing one
	migrator := db.Migrate()
	migrator.AddSource(NewStructSource(db.dialect, []interface{}{&MyUser{}}))
	if err := migrator.Run(context.Background()); err != nil {
		t.Fatalf("migrator.Run() failed: %v", err)
	}

	// Verify table name is the default snake_case version
	var tableName string
	if err := db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='my_user'"); err != nil {
		t.Errorf("Failed to find table with default name 'my_user': %v", err)
	}
}

func TestMigrator_Run_CustomStructParser_TableName(t *testing.T) {
	db := setupTestDB(t)
	type MyUser struct {
		ID int `db:"id"`
	}

	// Define a custom table naming strategy
	//customNamer := func(t reflect.Type) string {
	//	return "tbl_" + strings.ToLower(t.Name())
	//}

	// Create a parser with the custom strategy
	customParser := NewDefaultStructParser()

	// Inject the custom parser
	migrator := db.Migrate()
	migrator.AddSource(NewStructSource(db.dialect, []interface{}{&MyUser{}}, customParser))
	if err := migrator.Run(context.Background()); err != nil {
		t.Fatalf("migrator.Run() failed: %v", err)
	}

	// Verify table was created with the custom name
	var tableName string
	if err := db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='tbl_myuser'"); err != nil {
		t.Errorf("Failed to find table with custom name 'tbl_myuser': %v", err)
	}
}

// --- Other Basic Tests (Unchanged but still relevant) ---

func TestMigrator_Run_RollbackOnFailure(t *testing.T) {
	db := setupTestDB(t)
	migrationsDir := t.TempDir()
	files := map[string]string{
		"001_create_users.sql": "-- +gsql Up\nCREATE TABLE users (id INTEGER);",
		"002_invalid.sql":      "-- +gsql Up\nCREATE TABLE products (id INTEGER; -- Syntax error",
	}
	createTestSQLFiles(t, migrationsDir, files)

	migrator := db.Migrate()
	migrator.AddSource(NewSQLDirSource(migrationsDir))
	err := migrator.Run(context.Background())
	if err == nil {
		t.Fatal("migrator.Run() was expected to fail, but it succeeded")
	}

	// Verify that the first successful migration was rolled back
	var tableName string
	err = db.GetContext(context.Background(), &tableName, "SELECT name FROM sqlite_master WHERE type='table' AND name='users'")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Error("'users' table should not exist due to rollback, but it does")
	}
}
