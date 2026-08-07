package gsql

import (
	"reflect"
	"testing"
	"time"
)

func TestDialects(t *testing.T) {
	drivers := []string{"mysql", "postgres", "sqlite3", "unknown"}

	for _, dName := range drivers {
		d := newDialect(dName)

		sql := "SELECT * FROM t WHERE id = ?"
		pSql := d.PlaceholderSQL(sql)
		if dName == "postgres" {
			if pSql != "SELECT * FROM t WHERE id = $1" {
				t.Errorf("%s PlaceholderSQL failed: %s", dName, pSql)
			}
		} else {
			if pSql != sql {
				t.Errorf("%s PlaceholderSQL should be unchanged: %s", dName, pSql)
			}
		}

		ph := d.Placeholder(0)
		if dName == "postgres" {
			if ph != "$1" {
				t.Errorf("%s Placeholder failed", dName)
			}
		} else {
			if ph != "?" {
				t.Errorf("%s Placeholder failed", dName)
			}
		}

		types := []reflect.Type{
			reflect.TypeOf(true),
			reflect.TypeOf(int(1)),
			reflect.TypeOf(int64(1)),
			reflect.TypeOf(float64(1.0)),
			reflect.TypeOf(""),
			reflect.TypeOf(time.Time{}),
		}
		for _, typ := range types {
			dt := d.DataTypeOf(typ)
			if dt == "" {
				t.Errorf("%s DataTypeOf returned empty for %v", dName, typ)
			}
		}

		if d.AutoIncrement() == "" && dName != "postgres" {
			// Postgres 返回空，其它驱动通常不应为空；Postgres returns empty, others usually should not.
			t.Logf("%s AutoIncrement returned empty (may be expected for this driver)", dName)
		}

		if d.PrimaryKeyStr() == "" {
			t.Errorf("%s PrimaryKeyStr failed", dName)
		}
	}
}
