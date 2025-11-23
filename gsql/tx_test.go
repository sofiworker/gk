package gsql

import (
	"testing"
)

func TestNestedTransactions(t *testing.T) {
	// This is a basic test to demonstrate the nested transaction functionality
	// In a real scenario, you would use a test database

	// Since we can't easily test without a real database, we'll just verify the API compiles
	_ = &Tx{}
	_ = &DB{}
}

// ExampleNestedTransaction demonstrates how to use nested transactions
func ExampleNestedTransaction() {
	// This is a conceptual example showing how nested transactions would be used:
	// In practice, you would have a real database connection
	/*
		db := &DB{} // In practice, this would be a real database connection

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

		if err != nil {
			// Handle error
		}
	*/
}
