package gsql

import (
	"database/sql"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Scan is a powerful replacement for sqlx.StructScan that handles custom tags
// for default values and nil-to-zero-value conversion. It can scan into a
// single struct pointer or a pointer to a slice of structs.
//
// Example Usage:
//
//	var users []User
//	rows, err := db.Queryx("SELECT * FROM users")
//	if err == nil {
//	    err = gsql.Scan(rows, &users)
//	}
func Scan(rows *sqlx.Rows, dest interface{}) error {
	if rows == nil {
		return errors.New("gsql: rows is nil")
	}
	defer rows.Close()
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return errors.New("gsql: destination must be a non-nil pointer")
	}
	slice, isSlice := isSlice(v)

	if isSlice {
		elemType := slice.Type().Elem()
		for rows.Next() {
			newElem := reflect.New(elemType).Elem()
			if err := scanRow(rows, newElem); err != nil {
				return err
			}
			slice.Set(reflect.Append(slice, newElem))
		}
	} else {
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return sql.ErrNoRows
		}
		if err := scanRow(rows, v.Elem()); err != nil {
			return err
		}
	}

	return rows.Err()
}

func scanRow(rows *sqlx.Rows, v reflect.Value) error {
	rowMap := make(map[string]interface{})
	if err := rows.MapScan(rowMap); err != nil {
		return err
	}
	return mapToStruct(rowMap, v)
}

func mapToStruct(rowMap map[string]interface{}, v reflect.Value) error {
	if v.Kind() != reflect.Struct {
		return errors.New("gsql: destination must be a struct")
	}
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		tag := fieldType.Tag.Get("db")

		if tag == "" || tag == "-" || !field.CanSet() {
			continue
		}

		parts := strings.Split(tag, ",")
		dbName := parts[0]
		tagOpts := parts[1:]

		val, ok := rowMap[dbName]

		if !ok || val == nil {
			if contains(tagOpts, "nil") {
				field.Set(reflect.Zero(field.Type()))
				continue
			}
			if err := applyDefaultValue(field, tagOpts); err != nil {
				return err
			}
			continue
		}

		if reflect.TypeOf(val).AssignableTo(field.Type()) {
			field.Set(reflect.ValueOf(val))
		} else {
			switch field.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if v, ok := val.(int64); ok {
					field.SetInt(v)
				}
			case reflect.Float32, reflect.Float64:
				if v, ok := val.(float64); ok {
					field.SetFloat(v)
				}
			}
		}
	}
	return nil
}

func isSlice(v reflect.Value) (reflect.Value, bool) {
	v = v.Elem()
	if v.Kind() == reflect.Slice {
		return v, true
	}
	return v, false
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func applyDefaultValue(field reflect.Value, tagOpts []string) error {
	for _, opt := range tagOpts {
		if strings.HasPrefix(opt, "default:") {
			defaultValueStr := strings.TrimPrefix(opt, "default:")
			switch field.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				val, err := strconv.ParseInt(defaultValueStr, 10, 64)
				if err != nil {
					return err
				}
				field.SetInt(val)
			case reflect.String:
				field.SetString(defaultValueStr)
			case reflect.Bool:
				val, err := strconv.ParseBool(defaultValueStr)
				if err != nil {
					return err
				}
				field.SetBool(val)
			}
			return nil
		}
	}
	return nil
}
