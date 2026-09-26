package repository_test

import (
	"context"
	"fmt"
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

func TestSQLiteConversationLogRepositoryListsLatestFiveTurnsBySessionAndRole(t *testing.T) {
	ctx := context.Background()
	sqliteDatabase, err := database.OpenSQLite(ctx, filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer sqliteDatabase.Close()
	repository := repository.NewSQLiteConversationLogRepository(sqliteDatabase)
	baseTime := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	for index := 1; index <= 7; index++ {
		if err := repository.Log(ctx, model.ConversationLog{
			SessionID:  "continued-session",
			Role:       "customer",
			Question:   fmt.Sprintf("customer-question-%d", index),
			Answer:     fmt.Sprintf("customer-answer-%d", index),
			AnswerType: "knowledge",
			CreatedAt:  baseTime.Add(time.Duration(index) * time.Minute),
		}); err != nil {
			t.Fatalf("write customer turn %d: %v", index, err)
		}
	}
	if err := repository.Log(ctx, model.ConversationLog{
		SessionID: "continued-session", Role: "admin", Question: "internal-question",
		Answer: "internal-answer", AnswerType: "knowledge", CreatedAt: baseTime.Add(8 * time.Minute),
	}); err != nil {
		t.Fatalf("write admin turn: %v", err)
	}

	logs, err := repository.ListRecentBySessionAndRole(ctx, "continued-session", "customer", 5)
	if err != nil {
		t.Fatalf("list recent customer turns: %v", err)
	}
	if len(logs) != 5 {
		t.Fatalf("recent turn count = %d, want 5", len(logs))
	}
	for index, entry := range logs {
		expected := fmt.Sprintf("customer-question-%d", index+3)
		if entry.Question != expected || entry.Role != "customer" {
			t.Fatalf("turn %d = %#v, want question %q for customer", index, entry, expected)
		}
	}
}

func TestSQLiteConversationLogRepositoryPersistsOrderAnswer(t *testing.T) {
	ctx := context.Background()
	sqliteDatabase, err := database.OpenSQLite(ctx, filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer sqliteDatabase.Close()
	repository := repository.NewSQLiteConversationLogRepository(sqliteDatabase)
	if err := repository.Log(ctx, model.ConversationLog{
		SessionID: "order-session", Role: "customer", Question: "查订单 ORD2026001",
		Answer: "已查询到订单 ORD2026001 的 Mock 进度信息。", AnswerType: "order",
		CreatedAt: time.Date(2026, time.September, 25, 13, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("persist order answer: %v", err)
	}
	logs, err := repository.ListBySession(ctx, "order-session")
	if err != nil {
		t.Fatalf("list order session: %v", err)
	}
	if len(logs) != 1 || logs[0].AnswerType != "order" || len(logs[0].CitationDocuments) != 0 {
		t.Fatalf("unexpected order log: %#v", logs)
	}
}
