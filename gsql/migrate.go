package gsql

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	RecordTableName = "gsql_migration"
	StructTagName   = "db"
)

var (
	matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap   = regexp.MustCompile("([a-z0-9])([A-Z])")
)

func toSnakeCase(str string) string {
	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

type Migration struct {
	ID   string
	Name string
	Up   func(tx *Tx) error
	// UpWithContext 优先于 Up，被 Migrator.Run 调用以透传外部上下文。
	UpWithContext func(ctx context.Context, tx *Tx) error
	Down          func(tx *Tx) error
	// DownWithContext 为 Down 的上下文版本，保留接口完整性。
	DownWithContext func(ctx context.Context, tx *Tx) error
}

type MigrateOptions struct {
	RecordTableName string
	Recorder        Recorder
	Logger          Logger
}

type MigrateOption func(*MigrateOptions)

func WithRecordTableName(name string) MigrateOption {
	return func(o *MigrateOptions) { o.RecordTableName = name }
}
func WithRecorder(r Recorder) MigrateOption {
	return func(o *MigrateOptions) { o.Recorder = r }
}
func WithLogger(l Logger) MigrateOption {
	return func(o *MigrateOptions) { o.Logger = l }
}

type Migrator struct {
	db *DB
	MigrateOptions
	sources []MigrationSource
}

func (db *DB) Migrate(opts ...MigrateOption) *Migrator {
	options := MigrateOptions{
		RecordTableName: RecordTableName,
		Logger:          db.ensureLogger(),
	}
	for _, opt := range opts {
		opt(&options)
	}
	if options.Logger == nil {
		options.Logger = db.logger
	}
	if options.Recorder == nil {
		options.Recorder = newLogRecorder(db.logger)
	}
	return &Migrator{db: db, MigrateOptions: options}
}

func (m *Migrator) AddSource(source MigrationSource) {
	m.sources = append(m.sources, source)
}

func (m *Migrator) Run(ctx context.Context) (err error) {
	allMigrations, err := m.collectMigrations()
	if err != nil {
		return fmt.Errorf("failed to collect migrations: %w", err)
	}

	sort.Slice(allMigrations, func(i, j int) bool {
		return allMigrations[i].ID < allMigrations[j].ID
	})

	return m.db.TxContext(ctx, func(tx *Tx) error {
		if err := m.Recorder.Init(ctx, tx); err != nil {
			return fmt.Errorf("failed to initialize recorder: %w", err)
		}
		applied, err := m.Recorder.GetAppliedMigrations(ctx, tx)
		if err != nil {
			return err
		}
		pendingCount := 0
		for _, mig := range allMigrations {
			if applied[mig.ID] {
				continue
			}
			pendingCount++
			m.Logger.Infof("Applying migration '%s'...", mig.Name)
			runUp := mig.Up
			if mig.UpWithContext != nil {
				runUp = func(_tx *Tx) error {
					return mig.UpWithContext(ctx, _tx)
				}
			}
			if runUp == nil {
				return fmt.Errorf("migration '%s' has no up function", mig.Name)
			}
			if err := runUp(tx); err != nil {
				return fmt.Errorf("failed to apply migration '%s': %w", mig.Name, err)
			}
			if err := m.Recorder.Record(ctx, tx, mig.ID); err != nil {
				return fmt.Errorf("failed to record migration '%s': %w", mig.Name, err)
			}
		}
		if pendingCount == 0 {
			m.Logger.Infof("Database is up to date.")
		} else {
			m.Logger.Infof("Applied %d new migration(s) successfully.", pendingCount)
		}
		return nil
	})
}

func (m *Migrator) collectMigrations() ([]*Migration, error) {
	var allMigrations []*Migration
	for _, source := range m.sources {
		migrations, err := source.Collect()
		if err != nil {
			return nil, err
		}
		allMigrations = append(allMigrations, migrations...)
	}
	return allMigrations, nil
}

type Recorder interface {
	Init(ctx context.Context, tx *Tx) error
	Record(ctx context.Context, tx *Tx, migrationID string) error
	GetAppliedMigrations(ctx context.Context, tx *Tx) (map[string]bool, error)
}

type DBRecorder struct {
	tableName string
	dialect   Dialect
}

func NewDBRecorder(tableName string, dialect Dialect) *DBRecorder {
	return &DBRecorder{tableName: tableName, dialect: dialect}
}

func (r *DBRecorder) Init(ctx context.Context, tx *Tx) error {
	query := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (id %s PRIMARY KEY, applied_at %s)",
		r.tableName,
		r.dialect.PrimaryKeyStr(),
		r.dialect.DataTypeOf(reflect.TypeOf(time.Time{})),
	)
	_, err := tx.ExecContext(ctx, query)
	return err
}

func (r *DBRecorder) Record(ctx context.Context, tx *Tx, migrationID string) error {
	query := fmt.Sprintf("INSERT INTO %s (id, applied_at) VALUES (?, ?)", r.tableName)
	query = r.dialect.PlaceholderSQL(query)
	_, err := tx.ExecContext(ctx, query, migrationID, time.Now())
	return err
}

func (r *DBRecorder) GetAppliedMigrations(ctx context.Context, tx *Tx) (map[string]bool, error) {
	query := fmt.Sprintf("SELECT id FROM %s", r.tableName)
	var ids []string
	if err := tx.SelectContext(ctx, &ids, query); err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	applied := make(map[string]bool, len(ids))
	for _, id := range ids {
		applied[id] = true
	}
	return applied, nil
}

type LogRecorder struct {
	logger Logger
}

func newLogRecorder(logger Logger) *LogRecorder {
	return &LogRecorder{logger: logger}
}

func (r *LogRecorder) Init(context.Context, *Tx) error {
	r.logger.Infof("Migration recorder initialized")
	return nil
}

func (r *LogRecorder) Record(_ context.Context, _ *Tx, migrationID string) error {
	r.logger.Infof("Recording migration '%s'...", migrationID)
	return nil
}

func (r *LogRecorder) GetAppliedMigrations(context.Context, *Tx) (map[string]bool, error) {
	return make(map[string]bool), nil
}
