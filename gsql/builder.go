package gsql

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
)

type operation int

const (
	opSelect operation = iota
	opInsert
	opUpdate
	opDelete
)

type Builder struct {
	executor Executor
	dialect  Dialect
	table    string

	operation operation
	columns   []string    // Default is ["*"]
	values    interface{} // Can be map[string]interface{} or a struct
	joins     []string
	where     []string
	args      []interface{}
	orderBy   []string
	groupBy   []string
	having    string
	limit     *int
	offset    *int
}

// From sets the table for the query. This is the entry point for a SELECT query.
func (b *Builder) From(table string) *Builder {
	b.table = table
	b.operation = opSelect
	return b
}

// Select specifies the columns to retrieve.
func (b *Builder) Select(columns ...string) *Builder {
	if len(columns) == 0 {
		b.columns = []string{"*"}
	} else {
		b.columns = columns
	}
	return b
}

// Insert initializes an INSERT operation for the given table.
func (b *Builder) Insert(table string, data interface{}) *Builder {
	b.table = table
	b.operation = opInsert
	b.values = data
	return b
}

// Update initializes an UPDATE operation for the given table.
func (b *Builder) Update(table string, data interface{}) *Builder {
	b.table = table
	b.operation = opUpdate
	b.values = data
	return b
}

// Delete initializes a DELETE operation for the given table.
func (b *Builder) Delete(table string) *Builder {
	b.table = table
	b.operation = opDelete
	return b
}

// Where adds a WHERE condition.
func (b *Builder) Where(query string, args ...interface{}) *Builder {
	b.where = append(b.where, query)
	b.args = append(b.args, args...)
	return b
}

// Join adds an INNER JOIN clause.
func (b *Builder) Join(join string) *Builder {
	b.joins = append(b.joins, "JOIN "+join)
	return b
}

// LeftJoin adds a LEFT JOIN clause.
func (b *Builder) LeftJoin(join string) *Builder {
	b.joins = append(b.joins, "LEFT JOIN "+join)
	return b
}

// RightJoin adds a RIGHT JOIN clause.
func (b *Builder) RightJoin(join string) *Builder {
	b.joins = append(b.joins, "RIGHT JOIN "+join)
	return b
}

// OrderBy adds an ORDER BY clause.
func (b *Builder) OrderBy(columns ...string) *Builder {
	b.orderBy = columns
	return b
}

// GroupBy adds a GROUP BY clause.
func (b *Builder) GroupBy(columns ...string) *Builder {
	b.groupBy = columns
	return b
}

// Having adds a HAVING clause.
func (b *Builder) Having(query string, args ...interface{}) *Builder {
	b.having = query
	b.args = append(b.args, args...)
	return b
}

// Limit sets the LIMIT count.
func (b *Builder) Limit(limit int) *Builder {
	b.limit = &limit
	return b
}

// Offset sets the OFFSET count.
func (b *Builder) Offset(offset int) *Builder {
	b.offset = &offset
	return b
}

// Tx 在事务中执行操作
func (b *Builder) Tx(fn func(tx *Tx) error, opts ...TxOption) error {
	return b.TxContext(context.Background(), fn, opts...)
}

// TxContext 在事务中执行操作，支持上下文
func (b *Builder) TxContext(ctx context.Context, fn func(tx *Tx) error, opts ...TxOption) error {
	// 检查 executor 是否为 *DB 类型
	if db, ok := b.executor.(*DB); ok {
		// 创建新事务
		return db.TxContext(ctx, fn, opts...)
	}

	// 如果 executor 是 *Tx 类型，直接使用该事务的嵌套事务功能
	if tx, ok := b.executor.(*Tx); ok {
		return tx.NestedTx(fn, opts...)
	}

	return fmt.Errorf("unsupported executor type")
}

// ToSQL generates the final SQL statement and arguments.
func (b *Builder) ToSQL() (string, []interface{}, error) {
	if b.table == "" {
		return "", nil, fmt.Errorf("gsql: table name not specified")
	}
	switch b.operation {
	case opSelect:
		return b.buildSelectSQL()
	case opInsert:
		return b.buildInsertSQL()
	case opUpdate:
		return b.buildUpdateSQL()
	case opDelete:
		return b.buildDeleteSQL()
	default:
		// Default to SELECT if no operation is specified
		return b.buildSelectSQL()
	}
}

// ToRawSQL generates a raw SQL string for debugging (do not use for execution).
func (b *Builder) ToRawSQL() (string, error) {
	query, args, err := b.ToSQL()
	if err != nil {
		return "", err
	}
	// This is a simple replacement for debugging and may not be fully accurate.
	for _, arg := range args {
		// A simple placeholder replacer
		// Note: This doesn't respect placeholder types ($1, $2 vs ?)
		query = strings.Replace(query, "?", fmt.Sprintf("'%v'", arg), 1)
	}
	return query, nil
}

// ExecContext executes INSERT, UPDATE, DELETE operations.
func (b *Builder) ExecContext(ctx context.Context) (sql.Result, error) {
	query, args, err := b.ToSQL()
	if err != nil {
		return nil, err
	}

	return b.executor.ExecContext(ctx, query, args...)
}

// GetContext executes a SELECT query and scans a single row result.
func (b *Builder) GetContext(ctx context.Context, dest interface{}) error {
	b.operation = opSelect // Ensure it's a select operation
	query, args, err := b.ToSQL()
	if err != nil {
		return err
	}

	return b.executor.GetContext(ctx, dest, query, args...)
}

// SelectContext executes a SELECT query and scans multi-row results.
func (b *Builder) SelectContext(ctx context.Context, dest interface{}) error {
	b.operation = opSelect // Ensure it's a select operation
	query, args, err := b.ToSQL()
	if err != nil {
		return err
	}

	return b.executor.SelectContext(ctx, dest, query, args...)
}

// CountContext executes a COUNT query.
func (b *Builder) CountContext(ctx context.Context) (int64, error) {
	var count int64
	// 构建 COUNT 查询
	query, args, err := b.buildCountSQL()
	if err != nil {
		return 0, err
	}

	err = b.executor.GetContext(ctx, &count, query, args...)
	return count, err
}

func (b *Builder) buildSelectSQL() (string, []interface{}, error) {
	var sb strings.Builder

	// SELECT
	sb.WriteString("SELECT ")
	if len(b.columns) == 0 {
		sb.WriteString("*")
	} else {
		sb.WriteString(strings.Join(b.columns, ", "))
	}

	// FROM
	sb.WriteString(" FROM ")
	sb.WriteString(b.table)

	// JOIN
	if len(b.joins) > 0 {
		sb.WriteString(" ")
		sb.WriteString(strings.Join(b.joins, " "))
	}

	// WHERE
	whereClause, whereArgs, err := b.buildWhereClause()
	if err != nil {
		return "", nil, err
	}
	if whereClause != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereClause)
	}

	// GROUP BY
	if len(b.groupBy) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(b.groupBy, ", "))
	}

	// HAVING
	if b.having != "" {
		sb.WriteString(" HAVING ")
		sb.WriteString(b.having)
	}

	// ORDER BY
	if len(b.orderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(strings.Join(b.orderBy, ", "))
	}

	// LIMIT
	if b.limit != nil {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", *b.limit))
	}

	// OFFSET
	if b.offset != nil {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", *b.offset))
	}

	return sb.String(), whereArgs, nil
}

func (b *Builder) buildInsertSQL() (string, []interface{}, error) {
	var sb strings.Builder
	var queryArgs []interface{}

	cols, vals, err := b.extractColumnsAndValues()
	if err != nil {
		return "", nil, err
	}
	if len(cols) == 0 {
		return "", nil, fmt.Errorf("gsql: insert must have values")
	}

	sb.WriteString("INSERT INTO ")
	sb.WriteString(b.table)

	var placeholders []string
	for _, val := range vals {
		placeholders = append(placeholders, b.dialect.Placeholder(len(queryArgs)))
		queryArgs = append(queryArgs, val)
	}

	sb.WriteString(" (")
	sb.WriteString(strings.Join(cols, ", "))
	sb.WriteString(") VALUES (")
	sb.WriteString(strings.Join(placeholders, ", "))
	sb.WriteString(")")

	return sb.String(), queryArgs, nil
}

func (b *Builder) buildUpdateSQL() (string, []interface{}, error) {
	var sb strings.Builder
	var queryArgs []interface{}

	cols, vals, err := b.extractColumnsAndValues()
	if err != nil {
		return "", nil, err
	}
	if len(cols) == 0 {
		return "", nil, fmt.Errorf("gsql: update must have values")
	}

	sb.WriteString("UPDATE ")
	sb.WriteString(b.table)
	sb.WriteString(" SET ")
	var setClauses []string
	for i, col := range cols {
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", col, b.dialect.Placeholder(len(queryArgs))))
		queryArgs = append(queryArgs, vals[i])
	}
	sb.WriteString(strings.Join(setClauses, ", "))

	// WHERE
	whereClause, whereArgs, err := b.buildWhereClause()
	if err != nil {
		return "", nil, err
	}
	if whereClause != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereClause)
		queryArgs = append(queryArgs, whereArgs...)
	} else {
		return "", nil, fmt.Errorf("gsql: unsafe update without where clause")
	}

	return sb.String(), queryArgs, nil
}

func (b *Builder) buildDeleteSQL() (string, []interface{}, error) {
	var sb strings.Builder

	sb.WriteString("DELETE FROM ")
	sb.WriteString(b.table)

	// WHERE
	whereClause, whereArgs, err := b.buildWhereClause()
	if err != nil {
		return "", nil, err
	}
	if whereClause != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereClause)
	} else {
		return "", nil, fmt.Errorf("gsql: unsafe delete without where clause")
	}

	return sb.String(), whereArgs, nil
}

func (b *Builder) buildCountSQL() (string, []interface{}, error) {
	var sb strings.Builder

	sb.WriteString("SELECT COUNT(1) FROM ")
	sb.WriteString(b.table)

	// JOIN
	if len(b.joins) > 0 {
		sb.WriteString(" ")
		sb.WriteString(strings.Join(b.joins, " "))
	}

	// WHERE
	whereClause, whereArgs, err := b.buildWhereClause()
	if err != nil {
		return "", nil, err
	}
	if whereClause != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereClause)
	}

	// GROUP BY
	if len(b.groupBy) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(b.groupBy, ", "))
	}

	// HAVING
	if b.having != "" {
		sb.WriteString(" HAVING ")
		sb.WriteString(b.having)
	}

	return sb.String(), whereArgs, nil
}

func (b *Builder) buildWhereClause() (string, []interface{}, error) {
	if len(b.where) == 0 {
		return "", nil, nil
	}

	var finalArgs []interface{}
	var finalWhere []string
	argOffset := 0

	for _, wherePart := range b.where {
		numPlaceholders := strings.Count(wherePart, "?")
		if numPlaceholders == 0 {
			finalWhere = append(finalWhere, wherePart)
			continue
		}

		partArgs := b.args[argOffset : argOffset+numPlaceholders]
		argOffset += numPlaceholders

		newWherePart := wherePart
		for _, arg := range partArgs {
			if sub, ok := arg.(*Builder); ok {
				subSQL, subArgs, err := sub.ToSQL()
				if err != nil {
					return "", nil, fmt.Errorf("gsql: failed to build subquery: %w", err)
				}
				newWherePart = strings.Replace(newWherePart, "?", "("+subSQL+")", 1)
				finalArgs = append(finalArgs, subArgs...)
				continue
			}

			if arg != nil && reflect.TypeOf(arg).Kind() == reflect.Slice {
				s := reflect.ValueOf(arg)
				if s.Len() == 0 {
					newWherePart = strings.Replace(newWherePart, "?", "(NULL)", 1)
					continue
				}
				placeholders := make([]string, s.Len())
				for j := 0; j < s.Len(); j++ {
					placeholders[j] = b.dialect.Placeholder(len(finalArgs) + j)
					finalArgs = append(finalArgs, s.Index(j).Interface())
				}
				newWherePart = strings.Replace(newWherePart, "?", "("+strings.Join(placeholders, ", ")+")", 1)
			} else {
				newWherePart = strings.Replace(newWherePart, "?", b.dialect.Placeholder(len(finalArgs)), 1)
				finalArgs = append(finalArgs, arg)
			}
		}
		finalWhere = append(finalWhere, newWherePart)
	}

	return strings.Join(finalWhere, " AND "), finalArgs, nil
}

func (b *Builder) extractColumnsAndValues() ([]string, []interface{}, error) {
	if b.values == nil {
		return nil, nil, nil
	}

	// Case 1: map[string]interface{}
	if valMap, ok := b.values.(map[string]interface{}); ok {
		cols := make([]string, 0, len(valMap))
		vals := make([]interface{}, 0, len(valMap))
		for k, v := range valMap {
			cols = append(cols, k)
			vals = append(vals, v)
		}
		return cols, vals, nil
	}

	// Case 2: struct
	v := reflect.ValueOf(b.values)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, nil, fmt.Errorf("gsql: unsupported data type for insert/update: %T", b.values)
	}

	t := v.Type()
	cols := make([]string, 0, t.NumField())
	vals := make([]interface{}, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		// Skip unexported fields
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}
		cols = append(cols, tag)
		vals = append(vals, v.Field(i).Interface())
	}
	return cols, vals, nil
}
