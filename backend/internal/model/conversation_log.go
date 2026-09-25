package model

import "time"

// ConversationLog is the durable audit record for one completed chat turn.
type ConversationLog struct {
	ID                int64
	SessionID         string
	Role              string
	Question          string
	Answer            string
	AnswerType        string
	CitationDocuments []string
	CreatedAt         time.Time
}
