# GSQL - Go SQL 工具

面向 Go 应用的 SQL 工具包：

- 基于 `sqlx` 的数据库抽象层
- 流畅 API 的查询构建器（Builder）
- 多 SQL 方言支持
- 嵌套事务支持
- 迁移工具
- SQL 扫描工具

## 特性

### 嵌套事务

`gsql` 支持嵌套事务：嵌套事务与父事务共享同一个底层数据库事务，但提供干净的
API 用于组织复杂业务逻辑。

示例：

```go
err := db.Tx(func(tx *Tx) error {
	// 第一层事务
	_, err := tx.ExecContext(context.Background(), "INSERT INTO users (name) VALUES (?)", "Alice")
	if err != nil {
		return err
	}

	// 嵌套事务
	err = tx.NestedTx(func(nestedTx *Tx) error {
		// 第二层事务（共享底层 sql.Tx）
		_, err := nestedTx.ExecContext(context.Background(), "INSERT INTO orders (user_id, amount) VALUES (?, ?)", 1, 100)
		if err != nil {
			return err // 嵌套事务失败但不提交
		}

		// 再嵌套一层
		err = nestedTx.NestedTx(func(nestedTx2 *Tx) error {
			// 第三层事务
			_, err := nestedTx2.ExecContext(context.Background(), "INSERT INTO order_items (order_id, product) VALUES (?, ?)", 1, "Product A")
			return err
		})

		return err
	})

	if err != nil {
		return err // 回滚第一层事务
	}

	return nil // 提交事务
})
```

只有顶层事务可以提交或回滚数据库事务；嵌套事务只向上传播错误，便于在复杂业务
逻辑中做细粒度错误处理。
