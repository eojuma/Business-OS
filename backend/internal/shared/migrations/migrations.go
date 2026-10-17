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

// noTransactionMarker opts a migration out of the wrapping transaction so it
// can use statements PostgreSQL forbids inside transactions, such as
// CREATE INDEX CONCURRENTLY. Statements then run individually (autocommit),
// so the migration is not atomic; keep such migrations idempotent.
const noTransactionMarker = "-- +migrate NoTransaction"

type migration struct {
	version       int64
	name          string
	up            string
	down          string
	noTransaction bool
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

		if m.noTransaction {
			// Statements execute individually because PostgreSQL treats a
			// multi-statement simple query as one implicit transaction,
			// which forbids CREATE INDEX CONCURRENTLY and friends.
			for _, stmt := range splitStatements(m.up) {
				if err := db.Exec(stmt).Error; err != nil {
					return fmt.Errorf("apply migration %s: %w", m.name, err)
				}
			}
			if err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", m.version).Error; err != nil {
				return err
			}
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
		up := strings.TrimSpace(strings.Replace(sections[0], "-- +migrate Up", "", 1))
		noTransaction := strings.Contains(up, noTransactionMarker)
		up = strings.TrimSpace(strings.ReplaceAll(up, noTransactionMarker, ""))
		result = append(result, migration{
			version:       version,
			name:          entry.Name(),
			up:            up,
			down:          strings.TrimSpace(sections[1]),
			noTransaction: noTransaction,
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

// splitStatements splits a SQL script into individual statements on
// semicolons outside single-quoted strings, line comments, and
// dollar-quoted bodies ($$...$$ or $tag$...$tag$).
func splitStatements(sql string) []string {
	var (
		stmts    []string
		b        strings.Builder
		inString bool
	)
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case inString:
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' { // '' escape
					b.WriteByte(sql[i+1])
					i++
					continue
				}
				inString = false
			}
		case c == '\'':
			inString = true
			b.WriteByte(c)
		case c == '$':
			// Dollar-quoted body: copy verbatim through the matching closer.
			j := i + 1
			for j < len(sql) && isDollarTagChar(sql[j]) {
				j++
			}
			if j < len(sql) && sql[j] == '$' {
				closer := sql[i : j+1]
				if k := strings.Index(sql[j+1:], closer); k >= 0 {
					b.WriteString(sql[i : j+1+k+len(closer)])
					i = j + k + len(closer)
					continue
				}
			}
			b.WriteByte(c)
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				b.WriteByte(sql[i])
				i++
			}
			if i < len(sql) {
				b.WriteByte(sql[i])
			}
		case c == ';':
			if stmt := strings.TrimSpace(b.String()); stmt != "" {
				stmts = append(stmts, stmt)
			}
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if stmt := strings.TrimSpace(b.String()); stmt != "" {
		stmts = append(stmts, stmt)
	}
	return stmts
}

// isDollarTagChar reports whether c can appear in a dollar-quote tag
// (letters, digits, underscores; a tag may not start with a digit).
func isDollarTagChar(c byte) bool {
	return c == '_' ||
		('a' <= c && c <= 'z') ||
		('A' <= c && c <= 'Z') ||
		('0' <= c && c <= '9')
}
