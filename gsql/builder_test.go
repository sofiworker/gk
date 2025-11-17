package gsql

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

// mockExecutor is a mock implementation of the Executor interface for testing.
type mockExecutor struct {
	// Fields to store the last query and args for inspection
	lastQuery string
	lastArgs  []interface{}

	// Fields to control the mock's behavior
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
	// Simulate finding a result for count queries
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

// newTestBuilder creates a new Builder with a mock executor for testing.
func newTestBuilder() (*Builder, *mockExecutor) {
	executor := &mockExecutor{}
	builder := &Builder{
		executor: executor,
		dialect:  newDialect("mysql"), // Using mysql dialect for predictable placeholders
	}
	return builder, executor
}

func TestBuilder_Subquery(t *testing.T) {
	t.Run("should build where clause with subquery", func(t *testing.T) {
		// Subquery to select user IDs from the 'banned_users' table
		subBuilder, _ := newTestBuilder()
		subBuilder.From("banned_users").Select("user_id")

		// Main query to select users who are not in the banned list
		builder, _ := newTestBuilder()
		builder.From("users").
			Select("id", "name").
			Where("id NOT IN ?", subBuilder)

		expectedSQL := "SELECT id, name FROM users WHERE id NOT IN (SELECT user_id FROM banned_users)"
		var expectedArgs []interface{} // Expect a nil slice to match the implementation's return

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
		// Subquery
		subBuilder, _ := newTestBuilder()
		subBuilder.From("orders").
			Select("user_id").
			Where("status = ?", "shipped")

		// Main query
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
		// Subquery with an error (e.g., no table)
		subBuilder, _ := newTestBuilder()
		// Missing .From("some_table") to cause an error

		// Main query
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

	// The order of columns in the generated SQL can vary with maps, so we check for both possibilities.
	sql, args, err := builder.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL() returned an unexpected error: %v", err)
	}

	// Sort args to make the test deterministic
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
	builder.Update("users", data) // No WHERE clause

	_, _, err := builder.ToSQL()
	if err == nil {
		t.Fatal("Expected an error for unsafe update, but got nil")
	}

	expectedErr := "gsql: unsafe update without where clause"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}
