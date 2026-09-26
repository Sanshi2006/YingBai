package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"project-for-yingbai/backend/internal/model"
)

type SQLiteConversationLogRepository struct {
	database *sql.DB
}

func NewSQLiteConversationLogRepository(database *sql.DB) *SQLiteConversationLogRepository {
	return &SQLiteConversationLogRepository{database: database}
}

func (r *SQLiteConversationLogRepository) Log(ctx context.Context, entry model.ConversationLog) error {
	if r == nil || r.database == nil {
		return errors.New("conversation log database is not configured")
	}
	entry.SessionID = strings.TrimSpace(entry.SessionID)
	entry.Role = strings.TrimSpace(entry.Role)
	entry.Question = strings.TrimSpace(entry.Question)
	entry.Answer = strings.TrimSpace(entry.Answer)
	entry.AnswerType = strings.TrimSpace(entry.AnswerType)
	if entry.SessionID == "" || entry.Role == "" || entry.Question == "" || entry.Answer == "" || entry.AnswerType == "" {
		return errors.New("conversation log required fields are missing")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}

	citationDocuments := uniqueNonEmptyStrings(entry.CitationDocuments)
	serializedCitations, err := json.Marshal(citationDocuments)
	if err != nil {
		return fmt.Errorf("serialize citation documents: %w", err)
	}
	_, err = r.database.ExecContext(ctx, `
		INSERT INTO conversation_logs (
			session_id, role, question, answer, answer_type, citation_documents, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, entry.SessionID, entry.Role, entry.Question, entry.Answer, entry.AnswerType,
		string(serializedCitations), entry.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert conversation log: %w", err)
	}
	return nil
}

func (r *SQLiteConversationLogRepository) ListBySession(ctx context.Context, sessionID string) ([]model.ConversationLog, error) {
	if r == nil || r.database == nil {
		return nil, errors.New("conversation log database is not configured")
	}
	rows, err := r.database.QueryContext(ctx, `
		SELECT id, session_id, role, question, answer, answer_type, citation_documents, created_at
		FROM conversation_logs
		WHERE session_id = ?
		ORDER BY created_at ASC, id ASC
	`, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, fmt.Errorf("query conversation logs: %w", err)
	}
	defer rows.Close()
	return scanConversationLogs(rows)
}

func (r *SQLiteConversationLogRepository) ListRecentBySessionAndRole(
	ctx context.Context,
	sessionID, role string,
	limit int,
) ([]model.ConversationLog, error) {
	if r == nil || r.database == nil {
		return nil, errors.New("conversation log database is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	role = strings.TrimSpace(role)
	if sessionID == "" || role == "" || limit <= 0 {
		return nil, errors.New("recent conversation query is invalid")
	}
	rows, err := r.database.QueryContext(ctx, `
		SELECT id, session_id, role, question, answer, answer_type, citation_documents, created_at
		FROM (
			SELECT id, session_id, role, question, answer, answer_type, citation_documents, created_at
			FROM conversation_logs
			WHERE session_id = ? AND role = ?
			ORDER BY created_at DESC, id DESC
			LIMIT ?
		) AS recent_turns
		ORDER BY created_at ASC, id ASC
	`, sessionID, role, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent conversation logs: %w", err)
	}
	defer rows.Close()
	return scanConversationLogs(rows)
}

func scanConversationLogs(rows *sql.Rows) ([]model.ConversationLog, error) {
	logs := make([]model.ConversationLog, 0)
	for rows.Next() {
		var entry model.ConversationLog
		var serializedCitations string
		var createdAt string
		if err := rows.Scan(&entry.ID, &entry.SessionID, &entry.Role, &entry.Question, &entry.Answer,
			&entry.AnswerType, &serializedCitations, &createdAt); err != nil {
			return nil, fmt.Errorf("scan conversation log: %w", err)
		}
		if err := json.Unmarshal([]byte(serializedCitations), &entry.CitationDocuments); err != nil {
			return nil, fmt.Errorf("decode conversation log citations: %w", err)
		}
		parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("decode conversation log timestamp: %w", err)
		}
		entry.CreatedAt = parsedCreatedAt
		logs = append(logs, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation logs: %w", err)
	}
	return logs, nil
}

func uniqueNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
