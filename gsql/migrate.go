package gsql

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Migrate struct {
	db              *DB
	manager         *MigrationManager
	autoCreateTable bool
}

// MigrationType 定义迁移类型
type MigrationType string

const (
	// AutoMigrationType 自动迁移类型（基于结构体）
	AutoMigrationType MigrationType = "auto"
	// SQLMigrationType SQL文件迁移类型
	SQLMigrationType MigrationType = "sql"
	// ScriptMigrationType 脚本文件迁移类型
	ScriptMigrationType MigrationType = "script"
)

// MigrationSource 定义迁移源接口
type MigrationSource interface {
	Type() MigrationType
	Load() ([]Migration, error)
}

// Migration 定义单个迁移项
type Migration struct {
	ID          string
	Name        string
	Type        MigrationType
	Version     string
	Description string
	Source      string
	UpFunc      func(*Tx) error // Go函数形式的升级逻辑
	DownFunc    func(*Tx) error // Go函数形式的降级逻辑
	UpSQL       string          // SQL语句形式的升级逻辑
	DownSQL     string          // SQL语句形式的降级逻辑
}

// FileMigrationSource 文件迁移源
type FileMigrationSource struct {
	Path string
}

// Type 返回迁移源类型
func (f *FileMigrationSource) Type() MigrationType {
	return SQLMigrationType
}

// Load 加载文件迁移
func (f *FileMigrationSource) Load() ([]Migration, error) {
	var migrations []Migration

	// 确保路径存在
	info, err := os.Stat(f.Path)
	if err != nil {
		return nil, fmt.Errorf("migration path error: %w", err)
	}

	// 如果是目录，则遍历目录中的文件
	if info.IsDir() {
		files, err := ioutil.ReadDir(f.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration directory: %w", err)
		}

		// 按文件名排序，确保迁移按顺序执行
		sort.Slice(files, func(i, j int) bool {
			return files[i].Name() < files[j].Name()
		})

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			// 只处理.sql文件
			if filepath.Ext(file.Name()) != ".sql" {
				continue
			}

			filePath := filepath.Join(f.Path, file.Name())
			content, err := ioutil.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to read migration file %s: %w", filePath, err)
			}

			// 解析文件内容，分离up和down部分
			upSQL, downSQL := parseSQLMigration(string(content))

			// 生成迁移ID（使用文件名和内容的MD5）
			id := generateMigrationID(file.Name(), string(content))

			migration := Migration{
				ID:          id,
				Name:        file.Name(),
				Type:        SQLMigrationType,
				Version:     strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())),
				Description: file.Name(),
				Source:      filePath,
				UpSQL:       upSQL,
				DownSQL:     downSQL,
			}

			migrations = append(migrations, migration)
		}
	} else {
		// 单个文件
		content, err := ioutil.ReadFile(f.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", f.Path, err)
		}

		upSQL, downSQL := parseSQLMigration(string(content))

		id := generateMigrationID(f.Path, string(content))

		migration := Migration{
			ID:          id,
			Name:        filepath.Base(f.Path),
			Type:        SQLMigrationType,
			Version:     strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path)),
			Description: filepath.Base(f.Path),
			Source:      f.Path,
			UpSQL:       upSQL,
			DownSQL:     downSQL,
		}

		migrations = append(migrations, migration)
	}

	return migrations, nil
}

// parseSQLMigration 解析SQL迁移文件，分离up和down部分
func parseSQLMigration(content string) (upSQL, downSQL string) {
	lines := strings.Split(content, "\n")
	var upLines, downLines []string
	inUpSection := false
	inDownSection := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// 检查是否有特殊标记
		if strings.HasPrefix(trimmedLine, "-- +goose Up") ||
			strings.HasPrefix(trimmedLine, "-- +migrate Up") ||
			strings.HasPrefix(trimmedLine, "/* up */") {
			inUpSection = true
			inDownSection = false
			continue
		} else if strings.HasPrefix(trimmedLine, "-- +goose Down") ||
			strings.HasPrefix(trimmedLine, "-- +migrate Down") ||
			strings.HasPrefix(trimmedLine, "/* down */") {
			inUpSection = false
			inDownSection = true
			continue
		}

		if inUpSection {
			upLines = append(upLines, line)
		} else if inDownSection {
			downLines = append(downLines, line)
		} else if !strings.HasPrefix(trimmedLine, "--") && trimmedLine != "" {
			// 默认情况下，如果没有明确的分隔符，整个文件都是up部分
			upLines = append(upLines, line)
		}
	}

	return strings.Join(upLines, "\n"), strings.Join(downLines, "\n")
}

// generateMigrationID 生成迁移ID
func generateMigrationID(name, content string) string {
	data := name + content
	hash := md5.Sum([]byte(data))
	return fmt.Sprintf("%x", hash)[:16]
}

// AutoMigrationSource 自动迁移源（基于结构体）
type AutoMigrationSource struct {
	Models []interface{}
}

// Type 返回迁移源类型
func (a *AutoMigrationSource) Type() MigrationType {
	return AutoMigrationType
}

// Load 加载自动迁移
func (a *AutoMigrationSource) Load() ([]Migration, error) {
	// 自动迁移不需要预加载，会在执行时处理
	return []Migration{}, nil
}

// MigrationOption 迁移选项
type MigrationOption struct {
	TableName     string
	IgnoreMissing bool
	DryRun        bool
	// 是否自动创建迁移历史表，默认为false
	AutoCreateTable bool
}

// MigrationManager 迁移管理器
type MigrationManager struct {
	db      *DB
	sources []MigrationSource
	options MigrationOption
	history []MigrationHistory
}

// MigrationHistory 迁移历史记录
type MigrationHistory struct {
	ID          string
	Version     string
	Description string
	Type        MigrationType
	AppliedAt   time.Time
	Success     bool
	Error       string
}

// AddSource 添加迁移源
func (m *Migrate) AddSource(source MigrationSource) {
	if m.manager == nil {
		m.manager = &MigrationManager{
			db:      m.db,
			sources: make([]MigrationSource, 0),
		}
	}
	m.manager.sources = append(m.manager.sources, source)
}

// SetOptions 设置迁移选项
func (m *Migrate) SetOptions(options MigrationOption) {
	m.autoCreateTable = options.AutoCreateTable
	if m.manager != nil {
		m.manager.options = options
	}
}

// AutoMigrate 自动迁移模型
// 注意：这个方法需要根据具体的ORM库来实现
func (m *Migrate) AutoMigrate(models ...interface{}) error {
	// 创建迁移历史表（如果配置了自动创建）
	if m.autoCreateTable {
		if err := m.createMigrationTable(); err != nil {
			return fmt.Errorf("failed to create migration table: %w", err)
		}
	}

	// 在事务中执行自动迁移
	return m.db.Tx(func(tx *Tx) error {
		// 执行自动迁移（这里只是示例，实际需要根据使用的ORM实现）
		// 例如，如果使用GORM，可以这样调用：
		// if err := tx.DB.AutoMigrate(models...); err != nil {
		//     return fmt.Errorf("auto migration failed: %w", err)
		// }

		// 这里暂时留空，因为没有具体的ORM实现细节
		// 实际使用时请替换为具体的自动迁移实现

		// 记录成功的自动迁移
		history := MigrationHistory{
			ID:          generateMigrationID("auto", fmt.Sprintf("%v", models)),
			Version:     "auto",
			Description: "Automatic migration",
			Type:        AutoMigrationType,
			AppliedAt:   time.Now(),
			Success:     true,
		}

		// 注释掉 recordMigration 调用，因为我们没有实现这个方法
		if err := m.recordMigration(tx, history); err != nil {
			return fmt.Errorf("failed to record migration: %w", err)
		}

		return nil
	})
}

// createMigrationTable 创建迁移历史表
func (m *Migrate) createMigrationTable() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS migration_history (
		id VARCHAR(255) PRIMARY KEY,
		version VARCHAR(255) NOT NULL,
		description TEXT,
		type VARCHAR(50) NOT NULL,
		applied_at TIMESTAMP NOT NULL,
		success BOOLEAN NOT NULL,
		error TEXT
	)`

	_, err := m.db.Exec(createTableSQL)
	return err
}

// Migrate 执行所有迁移
func (m *Migrate) Migrate() error {
	// 创建迁移历史表（如果配置了自动创建）
	if m.autoCreateTable {
		if err := m.createMigrationTable(); err != nil {
			return fmt.Errorf("failed to create migration table: %w", err)
		}
	}

	if m.manager == nil {
		return fmt.Errorf("migration manager not initialized")
	}

	for _, source := range m.manager.sources {
		migrations, err := source.Load()
		if err != nil {
			return fmt.Errorf("failed to load migrations from %s: %w", source.Type(), err)
		}

		switch source.Type() {
		case AutoMigrationType:
			// 处理自动迁移
			autoSource, ok := source.(*AutoMigrationSource)
			if ok {
				if err := m.AutoMigrate(autoSource.Models...); err != nil {
					return fmt.Errorf("auto migration failed: %w", err)
				}
			}
		case SQLMigrationType, ScriptMigrationType:
			// 处理文件迁移
			for _, migration := range migrations {
				if err := m.executeMigration(migration); err != nil {
					return fmt.Errorf("migration %s failed: %w", migration.ID, err)
				}
			}
		}
	}
	return nil
}

// executeMigration 执行单个迁移
func (m *Migrate) executeMigration(migration Migration) error {
	// 检查是否已应用
	if m.isApplied(migration.ID) {
		return nil
	}

	// 在事务中执行迁移
	return m.db.Tx(func(tx *Tx) error {
		// 执行迁移
		if migration.UpFunc != nil {
			// 执行Go函数形式的迁移
			if err := migration.UpFunc(tx); err != nil {
				return err
			}
		} else if migration.UpSQL != "" {
			// 执行SQL语句形式的迁移
			if _, err := tx.ExecContext(nil, migration.UpSQL); err != nil {
				return err
			}
		} else {
			// 执行SQL或脚本文件形式的迁移
			if err := m.executeFileMigration(tx, migration.Source); err != nil {
				return err
			}
		}

		// 记录迁移历史
		history := MigrationHistory{
			ID:          migration.ID,
			Version:     migration.Version,
			Description: migration.Description,
			Type:        migration.Type,
			AppliedAt:   time.Now(),
			Success:     true,
		}

		// 注释掉 recordMigration 调用，因为我们没有实现这个方法
		if err := m.recordMigration(tx, history); err != nil {
			return err
		}

		return nil
	})
}

// isApplied 检查迁移是否已应用
func (m *Migrate) isApplied(migrationID string) bool {
	// 查询迁移历史表检查是否已应用
	var count int
	row := m.db.QueryRow("SELECT COUNT(*) FROM migration_history WHERE id = ? AND success = ?",
		migrationID, true)
	row.Scan(&count)
	return count > 0
}

// executeFileMigration 执行文件迁移
func (m *Migrate) executeFileMigration(tx *Tx, source string) error {
	// 根据文件扩展名判断执行方式
	switch filepath.Ext(source) {
	case ".sql":
		// 执行SQL文件
		content, err := ioutil.ReadFile(source)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(nil, string(content))
		return err
	case ".js", ".sh", ".py":
		// 执行脚本文件（需要相应的解释器）
		// 这里只是一个示例，实际实现需要根据具体需求调整
		cmd := exec.Command(filepath.Base(source))
		cmd.Dir = filepath.Dir(source)
		return cmd.Run()
	default:
		return fmt.Errorf("unsupported migration file type: %s", filepath.Ext(source))
	}
}

// recordMigration 记录迁移历史
func (m *Migrate) recordMigration(tx *Tx, history MigrationHistory) error {
	// 插入迁移历史记录
	// 使用原生SQL插入记录，避免循环依赖
	sql := `INSERT INTO migration_history (id, version, description, type, applied_at, success, error)
	        VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := tx.Exec(sql, history.ID, history.Version, history.Description,
		string(history.Type), history.AppliedAt, history.Success, history.Error)
	return err
}

// Rollback 回滚指定版本的迁移
func (m *Migrate) Rollback(version string) error {
	// 查找指定版本的迁移历史
	var history MigrationHistory
	row := m.db.QueryRow("SELECT id, version, description, type, applied_at, success, error FROM migration_history WHERE version = ? ORDER BY applied_at DESC LIMIT 1", version)
	err := row.Scan(&history.ID, &history.Version, &history.Description, (*string)(&history.Type), &history.AppliedAt, &history.Success, &history.Error)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("migration version %s not found", version)
		}
		return err
	}

	// 在事务中执行回滚
	return m.db.Tx(func(tx *Tx) error {
		// 执行回滚逻辑
		if history.Type == SQLMigrationType {
			// 对于SQL迁移，查找对应的迁移信息并执行down SQL
			// 注意：这需要我们保存原始的Migration对象或者down SQL
			// 此处简化处理，实际应用中可能需要更复杂的逻辑
		}

		// 更新迁移历史记录为已回滚
		history.Success = false
		history.Error = "Rolled back"

		updateSQL := `UPDATE migration_history SET success = ?, error = ? WHERE id = ?`
		if _, err := tx.ExecContext(nil, updateSQL, history.Success, history.Error, history.ID); err != nil {
			return err
		}

		return nil
	})
}

// Status 获取迁移状态
func (m *Migrate) Status() ([]MigrationHistory, error) {
	var histories []MigrationHistory
	rows, err := m.db.Query("SELECT id, version, description, type, applied_at, success, error FROM migration_history ORDER BY applied_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var history MigrationHistory
		var typeStr string
		err := rows.Scan(&history.ID, &history.Version, &history.Description, &typeStr,
			&history.AppliedAt, &history.Success, &history.Error)
		if err != nil {
			return nil, err
		}
		history.Type = MigrationType(typeStr)
		histories = append(histories, history)
	}

	return histories, rows.Err()
}
