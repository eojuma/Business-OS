package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

type migration struct {
	version int64
	name    string
	up      string
	down    string
}

func Up(db *gorm.DB) error {
	migrations, err := load()
	if err != nil {
		return err
	}
	if err := ensureVersionTable(db); err != nil {
		return err
	}

	for _, m := range migrations {
		var count int64
		if err := db.Table("schema_migrations").Where("version = ?", m.version).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(m.up).Error; err != nil {
				return fmt.Errorf("apply migration %s: %w", m.name, err)
			}
			return tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", m.version).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func Down(db *gorm.DB) error {
	migrations, err := load()
	if err != nil {
		return err
	}
	if err := ensureVersionTable(db); err != nil {
		return err
	}

	for i := len(migrations) - 1; i >= 0; i-- {
		m := migrations[i]
		var count int64
		if err := db.Table("schema_migrations").Where("version = ?", m.version).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			continue
		}

		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(m.down).Error; err != nil {
				return fmt.Errorf("rollback migration %s: %w", m.name, err)
			}
			return tx.Exec("DELETE FROM schema_migrations WHERE version = ?", m.version).Error
		})
	}
	return nil
}

func ensureVersionTable(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error
}

func load() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "sql")
	if err != nil {
		return nil, err
	}

	result := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		parts := strings.SplitN(strings.TrimSuffix(entry.Name(), ".sql"), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid migration version in %q: %w", entry.Name(), err)
		}
		contents, err := migrationFiles.ReadFile("sql/" + entry.Name())
		if err != nil {
			return nil, err
		}
		sections := strings.SplitN(string(contents), "-- +migrate Down", 2)
		if len(sections) != 2 || !strings.Contains(sections[0], "-- +migrate Up") {
			return nil, fmt.Errorf("migration %q must contain Up and Down sections", entry.Name())
		}
		result = append(result, migration{
			version: version,
			name:    entry.Name(),
			up:      strings.TrimSpace(strings.Replace(sections[0], "-- +migrate Up", "", 1)),
			down:    strings.TrimSpace(sections[1]),
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for i := 1; i < len(result); i++ {
		if result[i-1].version == result[i].version {
			return nil, fmt.Errorf("duplicate migration version %d", result[i].version)
		}
	}
	return result, nil
}
