package gsql

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type MigrationSource interface {
	Collect() ([]*Migration, error)
}

type TableName interface {
	TableName() string
}

type StructSource struct {
	structs []interface{}
	dialect Dialect
	parser  StructParser
}

func NewStructSource(dialect Dialect, structs []interface{}, parser ...StructParser) *StructSource {
	var p StructParser = NewDefaultStructParser()
	if len(parser) > 0 && parser[0] != nil {
		p = parser[0]
	}
	return &StructSource{structs: structs, dialect: dialect, parser: p}
}

func (s *StructSource) Collect() ([]*Migration, error) {
	mig, err := s.parser.Parse(s.dialect, s.structs...)
	if err != nil {
		return nil, err
	}
	if mig == nil {
		return []*Migration{}, nil
	}
	return []*Migration{mig}, nil
}

type StructParser interface {
	Parse(dialect Dialect, structs ...interface{}) (*Migration, error)
}

type DefaultStructParser struct{}

func NewDefaultStructParser() *DefaultStructParser {
	return &DefaultStructParser{}
}

func (p *DefaultStructParser) Parse(dialect Dialect, structs ...interface{}) (*Migration, error) {
	if len(structs) == 0 {
		return nil, nil
	}

	var tableSQLs, indexSQLs, downSQLs, structNames []string
	tableNames := make(map[reflect.Type]string)

	for _, str := range structs {
		t := reflect.TypeOf(str)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return nil, fmt.Errorf("gsql: expected a struct or pointer to struct, got %T", str)
		}
		tableName := toSnakeCase(t.Name())
		tableNames[t] = tableName
		if t.Implements(reflect.TypeOf((*TableName)(nil)).Elem()) {
			if name, ok := str.(TableName); ok {
				tableName = name.TableName()
			}
			tableNames[t] = tableName
		}
	}

	for _, str := range structs {
		t := reflect.TypeOf(str)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		tableName := tableNames[t]

		var columns, pks []string
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			dbTag := field.Tag.Get(StructTagName)
			if dbTag == "" || dbTag == "-" {
				continue
			}

			colDef, isPk, fkSQL, idxSQL := p.parseField(field, tableName, dialect)
			columns = append(columns, colDef)
			if isPk {
				pks = append(pks, dbTag)
			}
			if fkSQL != "" {
				indexSQLs = append(indexSQLs, fkSQL)
			}
			if idxSQL != "" {
				indexSQLs = append(indexSQLs, idxSQL)
			}
		}

		pkClause := ""
		if len(pks) > 0 {
			pkClause = fmt.Sprintf(", PRIMARY KEY (%s)", strings.Join(pks, ", "))
		}

		tableSQLs = append(tableSQLs, fmt.Sprintf("CREATE TABLE %s (%s%s);", tableName, strings.Join(columns, ", "), pkClause))
		downSQLs = append(downSQLs, fmt.Sprintf("DROP TABLE IF EXISTS %s;", tableName))
	}

	sort.Sort(sort.Reverse(sort.StringSlice(downSQLs)))
	upSQLs := append(tableSQLs, indexSQLs...)

	return &Migration{
		ID:   fmt.Sprintf("structs_%s", strings.ToLower(strings.Join(structNames, "_"))),
		Name: fmt.Sprintf("Create tables from structs: %s", strings.Join(structNames, ", ")),
		Up: func(tx *Tx) error {
			for _, sql := range upSQLs {
				if _, err := tx.ExecContext(context.Background(), sql); err != nil {
					return fmt.Errorf("failed to execute struct migration SQL: '%s', error: %w", sql, err)
				}
			}
			return nil
		},
		Down: func(tx *Tx) error {
			for _, sql := range downSQLs {
				if _, err := tx.ExecContext(context.Background(), sql); err != nil {
					return err
				}
			}
			return nil
		},
	}, nil
}

func (p *DefaultStructParser) parseField(field reflect.StructField, tableName string, dialect Dialect) (colDef string, isPk bool, fkSQL, idxSQL string) {
	dbTag := field.Tag.Get("db")
	gsqlTag := field.Tag.Get("gsql")
	tagMap := make(map[string]string)
	for _, part := range strings.Split(gsqlTag, ",") {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) == 2 {
			tagMap[kv[0]] = kv[1]
		} else {
			tagMap[kv[0]] = "true"
		}
	}

	colType := dialect.DataTypeOf(field.Type)
	if s, ok := tagMap["size"]; ok {
		if strings.Contains(colType, "VARCHAR") {
			colType = fmt.Sprintf("VARCHAR(%s)", s)
		}
	}

	var constraints []string
	if _, ok := tagMap["not_null"]; ok {
		constraints = append(constraints, "NOT NULL")
	}
	if val, ok := tagMap["default"]; ok {
		constraints = append(constraints, "DEFAULT "+val)
	}
	if _, ok := tagMap["auto_increment"]; ok {
		constraints = append(constraints, dialect.AutoIncrement())
	}
	if _, ok := tagMap["pk"]; ok {
		isPk = true
	}

	if fkDef, ok := tagMap["fk"]; ok {
		parts := strings.Split(fkDef, ".")
		if len(parts) == 2 {
			fkTable, fkCol := parts[0], parts[1]
			fkName := fmt.Sprintf("fk_%s_%s", tableName, dbTag)
			fkSQL = fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s);", tableName, fkName, dbTag, fkTable, fkCol)
		}
	}
	if _, ok := tagMap["index"]; ok {
		idxName := fmt.Sprintf("idx_%s_%s", tableName, dbTag)
		idxSQL = fmt.Sprintf("CREATE INDEX %s ON %s (%s);", idxName, tableName, dbTag)
	}

	colDef = fmt.Sprintf("%s %s %s", dbTag, colType, strings.Join(constraints, " "))
	return strings.TrimSpace(colDef), isPk, fkSQL, idxSQL
}

type SQLCollector interface {
	Collect(dir string) ([]*Migration, error)
}

type CommentCollector struct{}

func (c *CommentCollector) Collect(dir string) ([]*Migration, error) {
	var migrations []*Migration
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".sql") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", path, err)
		}

		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		var upBuilder, downBuilder strings.Builder
		var currentSection *strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			trimmedLine := strings.TrimSpace(line)
			if strings.HasPrefix(trimmedLine, "-- +gsql Up") {
				currentSection = &upBuilder
			} else if strings.HasPrefix(trimmedLine, "-- +gsql Down") {
				currentSection = &downBuilder
			} else if currentSection != nil {
				currentSection.WriteString(line)
				currentSection.WriteString("\n")
			}
		}

		upSQL := strings.TrimSpace(upBuilder.String())
		downSQL := strings.TrimSpace(downBuilder.String())

		// Default behavior: Up is not optional unless a different collector is used.
		if upSQL == "" {
			return nil // Silently skip files without Up directive
		}

		migration := &Migration{
			ID:   d.Name(),
			Name: strings.TrimSuffix(d.Name(), ".sql"),
			Up: func(tx *Tx) error {
				_, err := tx.ExecContext(context.Background(), upSQL)
				return err
			},
		}
		if downSQL != "" {
			migration.Down = func(tx *Tx) error {
				_, err := tx.ExecContext(context.Background(), downSQL)
				return err
			}
		}
		migrations = append(migrations, migration)
		return nil
	})
	return migrations, err
}

type WholeFileCollector struct{}

func (c *WholeFileCollector) Collect(dir string) ([]*Migration, error) {
	var migrations []*Migration
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".sql") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", path, err)
		}
		upSQL := string(content)
		if strings.TrimSpace(upSQL) == "" {
			return nil
		}
		migrations = append(migrations, &Migration{
			ID:   d.Name(),
			Name: strings.TrimSuffix(d.Name(), ".sql"),
			Up: func(tx *Tx) error {
				_, err := tx.ExecContext(context.Background(), upSQL)
				return err
			},
		})
		return nil
	})
	return migrations, err
}

type FilenameCollector struct{}

func (c *FilenameCollector) Collect(dir string) ([]*Migration, error) {
	type pair struct {
		upFile   string
		downFile string
	}
	pairs := make(map[string]*pair)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".sql") {
			return nil
		}

		name := d.Name()
		if base, found := strings.CutSuffix(name, "_up.sql"); found {
			if _, ok := pairs[base]; !ok {
				pairs[base] = &pair{}
			}
			pairs[base].upFile = path
		} else if base, found := strings.CutSuffix(name, "_down.sql"); found {
			if _, ok := pairs[base]; !ok {
				pairs[base] = &pair{}
			}
			pairs[base].downFile = path
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var migrations []*Migration
	for base, p := range pairs {
		if p.upFile == "" {
			continue // Skip down-only files
		}
		upContent, err := os.ReadFile(p.upFile)
		if err != nil {
			return nil, err
		}
		upSQL := string(upContent)
		mig := &Migration{
			ID:   base,
			Name: base,
			Up: func(tx *Tx) error {
				_, err := tx.ExecContext(context.Background(), upSQL)
				return err
			},
		}
		if p.downFile != "" {
			downContent, err := os.ReadFile(p.downFile)
			if err != nil {
				return nil, err
			}
			downSQL := string(downContent)
			mig.Down = func(tx *Tx) error {
				_, err := tx.ExecContext(context.Background(), downSQL)
				return err
			}
		}
		migrations = append(migrations, mig)
	}
	return migrations, nil
}

type SQLDirSource struct {
	dir       string
	collector SQLCollector
}

func NewSQLDirSource(dir string, collector ...SQLCollector) *SQLDirSource {
	var c SQLCollector = &CommentCollector{}
	if len(collector) > 0 && collector[0] != nil {
		c = collector[0]
	}
	return &SQLDirSource{dir: dir, collector: c}
}

func (s *SQLDirSource) Collect() ([]*Migration, error) {
	return s.collector.Collect(s.dir)
}
