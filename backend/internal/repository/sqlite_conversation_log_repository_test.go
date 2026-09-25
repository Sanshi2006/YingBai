package repository_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"project-for-yingbai/backend/internal/database"
	"project-for-yingbai/backend/internal/model"
	"project-for-yingbai/backend/internal/repository"
)

func TestSQLiteConversationLogRepositoryPersistsCompletedTurns(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "conversations.db")
	sqliteDatabase, err := database.OpenSQLite(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	logger := repository.NewSQLiteConversationLogRepository(sqliteDatabase)
	firstTime := time.Date(2026, time.September, 25, 10, 0, 0, 123, time.UTC)
	secondTime := firstTime.Add(time.Minute)
	if err := logger.Log(ctx, model.ConversationLog{
		SessionID:         "session-1",
		Role:              "customer",
		Question:          "包装破损怎么处理？",
		Answer:            "暂停流转并记录异常。[S1]",
		AnswerType:        "knowledge",
		CitationDocuments: []string{" 样品接收规范.md ", "样品接收规范.md", ""},
		CreatedAt:         firstTime,
	}); err != nil {
		t.Fatalf("write knowledge conversation log: %v", err)
	}
	if err := logger.Log(ctx, model.ConversationLog{
		SessionID:  "session-1",
		Role:       "customer",
		Question:   "火星天气？",
		Answer:     "知识库暂无依据，请转人工",
		AnswerType: "refusal",
		CreatedAt:  secondTime,
	}); err != nil {
		t.Fatalf("write refusal conversation log: %v", err)
	}
	if err := sqliteDatabase.Close(); err != nil {
		t.Fatalf("close SQLite database: %v", err)
	}

	reopenedDatabase, err := database.OpenSQLite(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen SQLite database: %v", err)
	}
	defer reopenedDatabase.Close()
	reopenedRepository := repository.NewSQLiteConversationLogRepository(reopenedDatabase)
	logs, err := reopenedRepository.ListBySession(ctx, "session-1")
	if err != nil {
		t.Fatalf("list conversation logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected two persisted logs, got %d", len(logs))
	}
	if logs[0].Question != "包装破损怎么处理？" || logs[0].AnswerType != "knowledge" ||
		len(logs[0].CitationDocuments) != 1 || logs[0].CitationDocuments[0] != "样品接收规范.md" ||
		!logs[0].CreatedAt.Equal(firstTime) {
		t.Fatalf("unexpected knowledge log: %#v", logs[0])
	}
	if logs[1].Answer != "知识库暂无依据，请转人工" || logs[1].AnswerType != "refusal" ||
		logs[1].CitationDocuments == nil || len(logs[1].CitationDocuments) != 0 ||
		!logs[1].CreatedAt.Equal(secondTime) {
		t.Fatalf("unexpected refusal log: %#v", logs[1])
	}
}
