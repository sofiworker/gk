# GSQL - Go SQL 工具 / Go SQL Utilities

面向 Go 应用的 SQL 工具包：基于 `sqlx` 的数据库抽象层、流畅 API 查询构建器、多 SQL 方言、嵌套事务、迁移工具与 SQL 扫描工具。
A SQL utility package for Go: a `sqlx`-based abstraction layer, fluent query builder, multiple SQL dialects, nested transactions, migrations and SQL scanning.

## 特性 / Features

### 嵌套事务 / Nested Transactions

`gsql` 支持嵌套事务：嵌套事务与父事务共享同一个底层数据库事务，但提供干净的 API 用于组织复杂业务逻辑。
`gsql` supports nested transactions: they share the parent's underlying database transaction while providing a clean API for complex business logic.

示例 / Example：

```go
err := db.Tx(func(tx *Tx) error {
	// 第一层事务 / first level transaction
	_, err := tx.ExecContext(context.Background(), "INSERT INTO users (name) VALUES (?)", "Alice")
	if err != nil {
		return err
	}

	// 嵌套事务 / nested transaction
	err = tx.NestedTx(func(nestedTx *Tx) error {
		_, err := nestedTx.ExecContext(context.Background(), "INSERT INTO orders (user_id, amount) VALUES (?, ?)", 1, 100)
		if err != nil {
			return err // 嵌套事务失败但不提交 / nested failure does not commit
		}

		// 再嵌套一层 / another nested level
		err = nestedTx.NestedTx(func(nestedTx2 *Tx) error {
			_, err := nestedTx2.ExecContext(context.Background(), "INSERT INTO order_items (order_id, product) VALUES (?, ?)", 1, "Product A")
			return err
		})

		return err
	})

	if err != nil {
		return err // 回滚第一层事务 / roll back the first level
	}

	return nil // 提交事务 / commit
})
```

只有顶层事务可以提交或回滚数据库事务；嵌套事务只向上传播错误。
Only the top-level transaction can commit or roll back; nested transactions propagate errors upward.
