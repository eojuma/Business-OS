package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

// noTransactionMarker opts a migration out of the wrapping transaction so it
// can use statements PostgreSQL forbids inside transactions, such as
// CREATE INDEX CONCURRENTLY. Statements then run individually (autocommit),
// so the migration is not atomic; keep such migrations idempotent.
const noTransactionMarker = "-- +migrate NoTransaction"

// lockKey serializes migration application across connections and processes
// (parallel test packages, multiple server instances booting at once) with
// pg_advisory_lock. Without it concurrent Up calls can deadlock on
// conflicting table-lock order or race the version-table insert.
const lockKey int64 = 640291337

type migration struct {
	version       int64
	name          string
	up            string
	down          string
	noTransaction bool
}

// withMigrationLock pins a single session, takes the advisory lock on it,
// runs fn, then releases the lock. Session locks are released by PostgreSQL
// if the connection dies, so they cannot leak.
func withMigrationLock(db *gorm.DB, fn func(locked *gorm.DB) error) error {
	ctx := context.Background()
	if db.Statement != nil && db.Statement.Context != nil {
		ctx = db.Statement.Context
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	// Registered first: runs after the unlock below (defer is LIFO).
	defer func() { _ = conn.Close() }()

	// WithContext clones the statement so swapping the connection pool below
	// cannot mutate the caller's session. The advisory lock and every
	// migration then run on this one pinned connection, so the session-scoped
	// lock stays held for the whole migration run.
	locked := db.WithContext(ctx)
	locked.Statement.ConnPool = conn

	// Try-lock in a loop instead of blocking on pg_advisory_lock: a backend
	// blocked inside pg_advisory_lock keeps its snapshot open, and
	// CREATE INDEX CONCURRENTLY waits for such older snapshots, producing an
	// advisory-wait/virtualxid deadlock cycle. Failed try attempts return
	// immediately and the sleep between them holds no snapshot, so the cycle
	// cannot form.
	for {
		var got bool
		if err := locked.Raw("SELECT pg_try_advisory_lock(?)", lockKey).Scan(&got).Error; err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
		if got {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer func() { _ = locked.Exec("SELECT pg_advisory_unlock(?)", lockKey).Error }()

	return fn(locked)
}

func Up(db *gorm.DB) error {
	return withMigrationLock(db, up)
}

func up(db *gorm.DB) error {
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
	return withMigrationLock(db, down)
}

func down(db *gorm.DB) error {
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
