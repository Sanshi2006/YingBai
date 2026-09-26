package database_test

import (
	"context"
	"database/sql"
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
	if migrationCount != 2 {
		t.Fatalf("expected two applied migrations after repeated initialization, got %d", migrationCount)
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

func TestOpenSQLiteUpgradesExistingConversationLogsForOrderAnswers(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open legacy SQLite database: %v", err)
	}
	_, err = legacy.ExecContext(ctx, `
		CREATE TABLE conversation_schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
		CREATE TABLE conversation_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('customer', 'service', 'admin')),
			question TEXT NOT NULL,
			answer TEXT NOT NULL,
			answer_type TEXT NOT NULL CHECK (answer_type IN ('knowledge', 'refusal')),
			citation_documents TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL
		);
		INSERT INTO conversation_schema_migrations (version, applied_at)
		VALUES ('001_conversation_logs.sql', '2026-09-25T00:00:00Z');
		INSERT INTO conversation_logs (
			session_id, role, question, answer, answer_type, citation_documents, created_at
		) VALUES (
			'legacy-session', 'customer', '旧问题', '旧回答', 'knowledge', '[]', '2026-09-25T00:00:00Z'
		);
	`)
	if err != nil {
		legacy.Close()
		t.Fatalf("create legacy SQLite schema: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy SQLite database: %v", err)
	}

	upgraded, err := database.OpenSQLite(ctx, databasePath)
	if err != nil {
		t.Fatalf("upgrade SQLite database: %v", err)
	}
	defer upgraded.Close()
	var legacyCount int
	if err := upgraded.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM conversation_logs WHERE session_id = 'legacy-session' AND answer_type = 'knowledge'",
	).Scan(&legacyCount); err != nil {
		t.Fatalf("query preserved legacy row: %v", err)
	}
	if legacyCount != 1 {
		t.Fatalf("legacy rows were not preserved, count = %d", legacyCount)
	}
	if _, err := upgraded.ExecContext(ctx, `
		INSERT INTO conversation_logs (
			session_id, role, question, answer, answer_type, citation_documents, created_at
		) VALUES ('order-session', 'customer', '查订单', 'Mock 结果', 'order', '[]', '2026-09-25T01:00:00Z')
	`); err != nil {
		t.Fatalf("insert order answer after migration: %v", err)
	}
}
