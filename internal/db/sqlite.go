package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
	_ "modernc.org/sqlite"
)

// Store wraps the pure-Go SQLite database, machine-bound sealer, optional rate resolver, and clock.
type Store struct {
	SQL          *sql.DB
	Sealer       *security.Sealer
	Path         string
	NowFn        func() time.Time
	RateResolver pricing.RateResolver
}

// Open opens or initializes the SQLite database in WAL mode with pure Go modernc.org/sqlite.
func Open(path, machineKeyOverride string, nowFn func() time.Time) (*Store, error) {
	if nowFn == nil {
		nowFn = time.Now
	}
	sealer, err := security.NewSealer(path, machineKeyOverride)
	if err != nil {
		return nil, fmt.Errorf("init machine seal: %w", err)
	}

	isMem := path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:")
	if !isMem {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("mkdir db dir: %w", err)
		}
	}

	dsn := path
	if dsn == "" {
		dsn = ":memory:"
	}
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single connection prevents SQLITE_BUSY inside the process and keeps :memory: DBs consistent.
	sqlDB.SetMaxOpenConns(1)

	if !isMem {
		_ = os.Chmod(path, 0o600)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, p := range pragmas {
		if _, err := sqlDB.Exec(p); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	// Idempotent migrations for pre-existing databases.
	if err := ensureTableColumns(sqlDB, "dynamic_pricing", []columnMigration{
		{name: "source", ddl: "TEXT NOT NULL DEFAULT 'agent'"},
	}); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := ensureTableColumns(sqlDB, "accesses", []columnMigration{
		{name: "reusable", ddl: "INTEGER NOT NULL DEFAULT 0"},
		{name: "permanent", ddl: "INTEGER NOT NULL DEFAULT 0"},
		{name: "session_ttl_ms", ddl: "INTEGER NOT NULL DEFAULT 0"},
	}); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	nowMs := strconv.FormatInt(nowFn().UnixMilli(), 10)
	if _, err := sqlDB.Exec(`INSERT OR IGNORE INTO meta(key, value) VALUES ('activation', ?)`, nowMs); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("set activation: %w", err)
	}

	return &Store{
		SQL:    sqlDB,
		Sealer: sealer,
		Path:   dsn,
		NowFn:  nowFn,
	}, nil
}

// OpenReadOnly opens an existing SQLite database in read-only mode without creating files or machine keys.
// Used by short-lived agent hooks so concurrent readers never block or contend with the running daemon.
func OpenReadOnly(path string, nowFn func() time.Time) (*Store, error) {
	if nowFn == nil {
		nowFn = time.Now
	}
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return nil, os.ErrNotExist
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid sqlite path: %s", path)
	}

	dsn := "file:" + path + "?mode=ro&_pragma=busy_timeout(500)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open readonly sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec("PRAGMA busy_timeout = 500;"); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("readonly busy_timeout: %w", err)
	}

	return &Store{
		SQL:   sqlDB,
		Path:  path,
		NowFn: nowFn,
	}, nil
}

// SetRateResolver attaches an optional cached public catalog resolver for uncataloged models.
func (s *Store) SetRateResolver(r pricing.RateResolver) {
	s.RateResolver = r
}

// Close closes the underlying SQLite database.
func (s *Store) Close() error {
	if s.SQL == nil {
		return nil
	}
	return s.SQL.Close()
}

// ActivationTime returns the recorded activation timestamp.
func (s *Store) ActivationTime() time.Time {
	var raw string
	err := s.SQL.QueryRow(`SELECT value FROM meta WHERE key='activation'`).Scan(&raw)
	if err != nil {
		return s.NowFn().UTC()
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return s.NowFn().UTC()
	}
	return time.UnixMilli(ms).UTC()
}

// RecordError persists a non-fatal error record if not already recorded.
func (s *Store) RecordError(kind, identity string) {
	id := security.LegacyHash(kind + identity)
	at := s.NowFn().UTC().Format("2006-01-02T15:04:05.000Z")
	_, _ = s.SQL.Exec(`INSERT OR IGNORE INTO errors(id, kind, at) VALUES (?, ?, ?)`, id, kind, at)
}

type columnMigration struct {
	name string
	ddl  string
}

func ensureTableColumns(sqlDB *sql.DB, table string, cols []columnMigration) error {
	rows, err := sqlDB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan %s column: %w", table, err)
		}
		existing[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate %s columns: %w", table, err)
	}
	_ = rows.Close()

	for _, col := range cols {
		if existing[strings.ToLower(col.name)] {
			continue
		}
		if _, err := sqlDB.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col.name, col.ddl)); err != nil {
			return fmt.Errorf("add column %s.%s: %w", table, col.name, err)
		}
	}
	return nil
}
