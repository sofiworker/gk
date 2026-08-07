# GSQL - Go SQL Utilities

English | [中文](README.md)

A SQL utility package for Go: a `sqlx`-based abstraction layer, fluent query builder, multiple SQL dialects, nested transactions, migrations and SQL scanning.

## Features

### Nested Transactions

`gsql` supports nested transactions: they share the parent's underlying database transaction while providing a clean API for complex business logic.

Example:

```go
err := db.Tx(func(tx *Tx) error {
	// first level transaction
	_, err := tx.ExecContext(context.Background(), "INSERT INTO users (name) VALUES (?)", "Alice")
	if err != nil {
		return err
	}

	// nested transaction
	err = tx.NestedTx(func(nestedTx *Tx) error {
		_, err := nestedTx.ExecContext(context.Background(), "INSERT INTO orders (user_id, amount) VALUES (?, ?)", 1, 100)
		if err != nil {
			return err // nested failure does not commit
		}

		// another nested level
		err = nestedTx.NestedTx(func(nestedTx2 *Tx) error {
			_, err := nestedTx2.ExecContext(context.Background(), "INSERT INTO order_items (order_id, product) VALUES (?, ?)", 1, "Product A")
			return err
		})

		return err
	})

	if err != nil {
		return err // roll back the first level
	}

	return nil // commit
})
```

Only the top-level transaction can commit or roll back; nested transactions propagate errors upward.
