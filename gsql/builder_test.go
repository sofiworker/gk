package gsql

import (
	"context"
	"database/sql"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

// mockExecutor 是 Executor 接口的测试桩。
// mockExecutor is a mock implementation of Executor for testing.
type mockExecutor struct {
	// 记录最后执行的查询与参数以便断言；store the last query and args for inspection.
	lastQuery string
	lastArgs  []interface{}

	// 控制桩行为；fields to control the mock behavior.
	result sql.Result
	err    error
}

func (m *mockExecutor) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	m.lastQuery = query
	m.lastArgs = args
	return m.result, m.err
}

func (m *mockExecutor) GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	m.lastQuery = query
	m.lastArgs = args
	if m.err != nil {
		return m.err
	}
	// 模拟 count 查询返回结果；simulate a result for count queries.
	if ptr, ok := dest.(*int64); ok {
		*ptr = 1
	}
	return nil
}

func (m *mockExecutor) SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	m.lastQuery = query
	m.lastArgs = args
	return m.err
}

// newTestBuilder 创建带 mock executor 的 Builder 用于测试。
// newTestBuilder creates a Builder with a mock executor.
func newTestBuilder() (*Builder, *mockExecutor) {
	executor := &mockExecutor{}
	builder := &Builder{
		executor: executor,
		dialect:  newDialect("mysql"), // 使用 mysql 方言以获得可预期的占位符；mysql dialect for predictable placeholders.
	}
	return builder, executor
}

func TestBuilder_Subquery(t *testing.T) {
	t.Run("should build where clause with subquery", func(t *testing.T) {
		// 子查询：从 banned_users 表取用户 ID；subquery selecting IDs from banned_users.
		subBuilder, _ := newTestBuilder()
		subBuilder.From("banned_users").Select("user_id")

		// 主查询：选择不在封禁列表中的用户；main query excluding banned users.
		builder, _ := newTestBuilder()
		builder.From("users").
			Select("id", "name").
			Where("id NOT IN ?", subBuilder)

		expectedSQL := "SELECT id, name FROM users WHERE id NOT IN (SELECT user_id FROM banned_users)"
		var expectedArgs []interface{} // 期望 nil 切片与实现返回一致；expect a nil slice to match.

		sql, args, err := builder.ToSQL()
		if err != nil {
			t.Fatalf("ToSQL() returned an unexpected error: %v", err)
		}

		if sql != expectedSQL {
			t.Errorf("Expected SQL:\n%s\nGot:\n%s", expectedSQL, sql)
		}

		if !reflect.DeepEqual(args, expectedArgs) {
			t.Errorf("Expected args %v (nil: %t), got %v (nil: %t)", expectedArgs, expectedArgs == nil, args, args == nil)
		}
	})

	t.Run("should build where clause with subquery with its own where clause", func(t *testing.T) {
		// 子查询；subquery.
		subBuilder, _ := newTestBuilder()
		subBuilder.From("orders").
			Select("user_id").
			Where("status = ?", "shipped")

		// 主查询；main query.
		builder, _ := newTestBuilder()
		builder.From("users").
			Select("id", "name").
			Where("id IN ?", subBuilder)

		expectedSQL := "SELECT id, name FROM users WHERE id IN (SELECT user_id FROM orders WHERE status = ?)"
		expectedArgs := []interface{}{"shipped"}

		sql, args, err := builder.ToSQL()
		if err != nil {
			t.Fatalf("ToSQL() returned an unexpected error: %v", err)
		}

		if sql != expectedSQL {
			t.Errorf("Expected SQL:\n%s\nGot:\n%s", expectedSQL, sql)
		}

		if !reflect.DeepEqual(args, expectedArgs) {
			t.Errorf("Expected args %v, got %v", expectedArgs, args)
		}
	})

	t.Run("should return error if subquery fails", func(t *testing.T) {
		// 构造子查询错误（例如缺表）；subquery that errors (e.g., missing table).
		subBuilder, _ := newTestBuilder()
		// 缺少 .From 以触发错误；missing From to cause an error.

		// 主查询；main query.
		builder, _ := newTestBuilder()
		builder.From("users").Where("id IN ?", subBuilder)

		_, _, err := builder.ToSQL()
		if err == nil {
			t.Fatal("ToSQL() was expected to return an error, but it didn't")
		}

		expectedError := "gsql: failed to build subquery: gsql: table name not specified"
		if err.Error() != expectedError {
			t.Errorf("Expected error message '%s', got '%s'", expectedError, err.Error())
		}
	})
}

func TestBuilder_Select(t *testing.T) {
	builder, _ := newTestBuilder()
	builder.From("users").Select("id", "name").Where("age > ?", 25)

	expectedSQL := "SELECT id, name FROM users WHERE age > ?"
	expectedArgs := []interface{}{25}

	sql, args, err := builder.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL() returned an unexpected error: %v", err)
	}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if !reflect.DeepEqual(args, expectedArgs) {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestBuilder_Insert(t *testing.T) {
	builder, _ := newTestBuilder()
	data := map[string]interface{}{"name": "John Doe", "age": 30}
	builder.Insert("users", data)

	// 使用 map 时生成 SQL 的列顺序可能不同，因此同时检查两种可能；
	// map iteration may vary column order, so check both possibilities.
	sql, args, err := builder.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL() returned an unexpected error: %v", err)
	}

	// 排序参数使测试确定性；sort args for determinism.
	var sortedArgs []interface{}
	if len(args) > 1 && args[0] == "John Doe" {
		sortedArgs = []interface{}{"John Doe", 30}
	} else if len(args) > 1 {
		sortedArgs = []interface{}{30, "John Doe"}
	} else {
		t.Fatalf("Incorrect number of arguments: got %d", len(args))
	}

	if strings.Contains(sql, "name, age") {
		expectedSQL := "INSERT INTO users (name, age) VALUES (?, ?)"
		if sql != expectedSQL {
			t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
		}
	} else if strings.Contains(sql, "age, name") {
		expectedSQL := "INSERT INTO users (age, name) VALUES (?, ?)"
		if sql != expectedSQL {
			t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
		}
	} else {
		t.Errorf("Generated SQL does not contain expected columns: %s", sql)
	}

	if !reflect.DeepEqual(sortedArgs, []interface{}{"John Doe", 30}) && !reflect.DeepEqual(sortedArgs, []interface{}{30, "John Doe"}) {
		t.Errorf("Expected args to contain 'John Doe' and 30, got %v", args)
	}
}

func TestBuilder_Update(t *testing.T) {
	builder, _ := newTestBuilder()
	data := map[string]interface{}{"name": "Jane Doe"}
	builder.Update("users", data).Where("id = ?", 1)

	expectedSQL := "UPDATE users SET name = ? WHERE id = ?"
	expectedArgs := []interface{}{"Jane Doe", 1}

	sql, args, err := builder.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL() returned an unexpected error: %v", err)
	}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if !reflect.DeepEqual(args, expectedArgs) {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestBuilder_Delete(t *testing.T) {
	builder, _ := newTestBuilder()
	builder.Delete("users").Where("id = ?", 1)

	expectedSQL := "DELETE FROM users WHERE id = ?"
	expectedArgs := []interface{}{1}

	sql, args, err := builder.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL() returned an unexpected error: %v", err)
	}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if !reflect.DeepEqual(args, expectedArgs) {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestBuilder_UnsafeUpdate(t *testing.T) {
	builder, _ := newTestBuilder()
	data := map[string]interface{}{"name": "Jane Doe"}
	builder.Update("users", data) // 无 WHERE 子句；no WHERE clause.

	_, _, err := builder.ToSQL()
	if err == nil {
		t.Fatal("Expected an error for unsafe update, but got nil")
	}

	expectedErr := "gsql: unsafe update without where clause"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestBuilderTxContextNestedSavepoint(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer sqlDB.Close()

	db := WrapSQLX(sqlx.NewDb(sqlDB, "mysql"), "mysql")
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT gsql_sp_1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO users (id) VALUES (?)")).
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT gsql_sp_1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err = db.TxContext(ctx, func(tx *Tx) error {
		builder := tx.Builder().Insert("users", map[string]interface{}{"id": 1})
		return builder.TxContext(ctx, func(nested *Tx) error {
			_, execErr := nested.ExecContext(ctx, "INSERT INTO users (id) VALUES (?)", 1)
			return execErr
		})
	})
	if err != nil {
		t.Fatalf("TxContext failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unmet expectations: %v", err)
	}
}
