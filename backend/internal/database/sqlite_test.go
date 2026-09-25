package database_test

import (
	"context"
	"path/filepath"
	"testing"

	"project-for-yingbai/backend/internal/database"
)

func TestOpenSQLiteAppliesMigrationsRepeatably(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "runtime", "conversations.db")
	ctx := context.Background()

	first, err := database.OpenSQLite(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	var journalMode string
	if err := first.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL journal mode, got %q", journalMode)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first SQLite connection: %v", err)
	}

	second, err := database.OpenSQLite(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen SQLite database: %v", err)
	}
	defer second.Close()

	var migrationCount int
	if err := second.QueryRowContext(ctx, "SELECT COUNT(*) FROM conversation_schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count SQLite migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected one applied migration after repeated initialization, got %d", migrationCount)
	}
	var tableCount int
	if err := second.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'conversation_logs'",
	).Scan(&tableCount); err != nil {
		t.Fatalf("query conversation log table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected conversation_logs table to exist")
	}
}
