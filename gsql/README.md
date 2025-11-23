# GSQL - Go SQL Utilities

A powerful SQL utility package for Go applications featuring:

- Database abstraction layer built on top of `sqlx`
- Query builder with fluent API
- Support for multiple SQL dialects
- Nested transaction support
- Migration tools
- SQL scanning utilities

## Features

### Nested Transactions

The gsql package supports nested transactions, allowing you to create transactions within transactions.
When a nested transaction is created, it shares the same underlying database transaction as its parent,
but provides a clean API for handling complex business logic.

Example usage:

```go
err := db.Tx(func(tx *Tx) error {
    // First level transaction
    _, err := tx.ExecContext(context.Background(), "INSERT INTO users (name) VALUES (?)", "Alice")
    if err != nil {
        return err
    }
    
    // Nested transaction
    err = tx.NestedTx(func(nestedTx *Tx) error {
        // Second level transaction (shares the same underlying sql.Tx)
        _, err := nestedTx.ExecContext(context.Background(), "INSERT INTO orders (user_id, amount) VALUES (?, ?)", 1, 100)
        if err != nil {
            return err // This would cause the nested transaction to fail but not commit
        }
        
        // Another nested transaction
        err = nestedTx.NestedTx(func(nestedTx2 *Tx) error {
            // Third level transaction
            _, err := nestedTx2.ExecContext(context.Background(), "INSERT INTO order_items (order_id, product) VALUES (?, ?)", 1, "Product A")
            return err
        })
        
        return err
    })
    
    if err != nil {
        return err // This would rollback the first level transaction
    }
    
    return nil // This would commit the transaction
})
```

Only the top-level transaction can commit or rollback the database transaction. Nested transactions
simply propagate errors upward, allowing for fine-grained error handling within complex business logic.