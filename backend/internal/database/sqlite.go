package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed sqlite_migrations/*.sql
var sqliteMigrationFiles embed.FS

// OpenSQLite opens the local conversation-log database and applies all pending
// versioned migrations. The modernc driver is embedded in the Go binary, so no
// SQLite executable or native library is required on the host.
func OpenSQLite(ctx context.Context, databasePath string) (*sql.DB, error) {
	databasePath = strings.TrimSpace(databasePath)
	if databasePath == "" {
		return nil, fmt.Errorf("SQLite database path is required")
	}
	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve SQLite database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o750); err != nil {
		return nil, fmt.Errorf("create SQLite database directory: %w", err)
	}

	database, err := sql.Open("sqlite", absolutePath)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	// SQLite is a local file. A single shared connection keeps connection-scoped
	// PRAGMAs deterministic and serializes these small audit writes.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect to SQLite: %w", err)
	}
	if err := configureSQLite(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	if err := migrateSQLite(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func configureSQLite(ctx context.Context, database *sql.DB) error {
	var journalMode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return fmt.Errorf("enable SQLite WAL mode: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("enable SQLite WAL mode: unexpected mode %q", journalMode)
	}
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure SQLite: %w", err)
		}
	}
	return nil
}

func migrateSQLite(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS conversation_schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create SQLite migration table: %w", err)
	}

	entries, err := sqliteMigrationFiles.ReadDir("sqlite_migrations")
	if err != nil {
		return fmt.Errorf("read SQLite migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := applySQLiteMigration(ctx, database, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func applySQLiteMigration(ctx context.Context, database *sql.DB, version string) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite migration %s: %w", version, err)
	}
	defer transaction.Rollback()

	var alreadyApplied int
	if err := transaction.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM conversation_schema_migrations WHERE version = ?", version,
	).Scan(&alreadyApplied); err != nil {
		return fmt.Errorf("check SQLite migration %s: %w", version, err)
	}
	if alreadyApplied > 0 {
		return transaction.Commit()
	}

	migration, err := sqliteMigrationFiles.ReadFile("sqlite_migrations/" + version)
	if err != nil {
		return fmt.Errorf("read SQLite migration %s: %w", version, err)
	}
	if _, err := transaction.ExecContext(ctx, string(migration)); err != nil {
		return fmt.Errorf("apply SQLite migration %s: %w", version, err)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO conversation_schema_migrations (version, applied_at) VALUES (?, ?)",
		version, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("record SQLite migration %s: %w", version, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit SQLite migration %s: %w", version, err)
	}
	return nil
}
